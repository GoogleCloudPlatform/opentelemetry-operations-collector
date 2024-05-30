// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build gpu
// +build gpu

package dcgmreceiver

import (
	"errors"
	"fmt"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"go.opentelemetry.io/collector/receiver/scrapererror"
	"go.uber.org/zap"
)

const maxWarningsForFailedDeviceMetricQuery = 5

const dcgmProfilingFieldsStart = dcgm.Short(1000)

var ErrDcgmInitialization = errors.New("error initializing DCGM")

type dcgmClient struct {
	logger                         *zap.SugaredLogger
	handleCleanup                  func()
	enabledFieldIDs                []dcgm.Short
	enabledFieldGroup              dcgm.FieldHandle
	deviceIndices                  []uint
	devicesModelName               []string
	devicesUUID                    []string
	deviceMetricToFailedQueryCount map[string]uint64
}

type dcgmMetric struct {
	timestamp int64
	name      string
	value     [4096]byte
}

// Can't pass argument dcgm.mode because it is unexported
var dcgmInit = func(args ...string) (func(), error) {
	return dcgm.Init(dcgm.Standalone, args...)
}

var dcgmGetLatestValuesForFields = dcgm.GetLatestValuesForFields

func newClient(config *Config, logger *zap.Logger) (*dcgmClient, error) {
	dcgmCleanup, err := initializeDcgm(config, logger)
	if err != nil {
		return nil, errors.Join(ErrDcgmInitialization, err)
	}
	deviceIndices := make([]uint, 0)
	names := make([]string, 0)
	UUIDs := make([]string, 0)
	enabledFieldGroup := dcgm.FieldHandle{}
	requestedFieldIDs := discoverRequestedFieldIDs(config)
	supportedProfilingFieldIDs, err := getSupportedProfilingFields()
	if err != nil {
		// If there is error querying the supported fields at all, let the
		// receiver collect basic metrics: (GPU utilization, used/free memory).
		logger.Sugar().Warnf("Error querying supported profiling fields on '%w'. GPU profiling metrics will not be collected.", err)
	}
	enabledFields, unavailableFields := filterSupportedFields(requestedFieldIDs, supportedProfilingFieldIDs)
	for _, f := range unavailableFields {
		logger.Sugar().Warnf("Field '%s' is not supported. Metric '%s' will not be collected", dcgmIDToName[f], dcgmIDToName[f])
	}
	if len(enabledFields) != 0 {
		deviceIndices, names, UUIDs, err = discoverDevices(logger)
		if err != nil {
			return nil, err
		}
		deviceGroup, err := createDeviceGroup(logger, deviceIndices)
		if err != nil {
			return nil, err
		}
		enabledFieldGroup, err = setWatchesOnEnabledFields(config, logger, deviceGroup, enabledFields)
		if err != nil {
			return nil, fmt.Errorf("Unable to set field watches on %w", err)
		}
	}
	return &dcgmClient{
		logger:                         logger.Sugar(),
		handleCleanup:                  dcgmCleanup,
		enabledFieldIDs:                enabledFields,
		enabledFieldGroup:              enabledFieldGroup,
		deviceIndices:                  deviceIndices,
		devicesModelName:               names,
		devicesUUID:                    UUIDs,
		deviceMetricToFailedQueryCount: make(map[string]uint64),
	}, nil
}

// initializeDcgm tries to initialize a DCGM connection; returns a cleanup func
// only if the connection is initialized successfully without error
func initializeDcgm(config *Config, logger *zap.Logger) (func(), error) {
	isSocket := "0"
	dcgmCleanup, err := dcgmInit(config.TCPAddrConfig.Endpoint, isSocket)
	if err != nil {
		msg := fmt.Sprintf("Unable to connect to DCGM daemon at %s on %v; Is the DCGM daemon running?", config.TCPAddrConfig.Endpoint, err)
		logger.Sugar().Warn(msg)
		if dcgmCleanup != nil {
			dcgmCleanup()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	logger.Sugar().Infof("Connected to DCGM daemon at %s", config.TCPAddrConfig.Endpoint)
	return dcgmCleanup, nil
}

func discoverDevices(logger *zap.Logger) ([]uint, []string, []string, error) {
	supportedDeviceIndices, err := dcgm.GetSupportedDevices()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("Unable to discover supported GPUs on %w", err)
	}
	logger.Sugar().Infof("Discovered %d supported GPU devices", len(supportedDeviceIndices))

	devices := make([]uint, 0, len(supportedDeviceIndices))
	names := make([]string, 0, len(supportedDeviceIndices))
	UUIDs := make([]string, 0, len(supportedDeviceIndices))
	for _, gpuIndex := range supportedDeviceIndices {
		deviceInfo, err := dcgm.GetDeviceInfo(gpuIndex)
		if err != nil {
			logger.Sugar().Warnf("Unable to query device info for NVIDIA device %d on '%w'", gpuIndex, err)
			continue
		}

		devices = append(devices, gpuIndex)
		names = append(names, deviceInfo.Identifiers.Model)
		UUIDs = append(UUIDs, deviceInfo.UUID)
		logger.Sugar().Infof("Discovered NVIDIA device %s with UUID %s", names[gpuIndex], UUIDs[gpuIndex])
	}

	return devices, names, UUIDs, nil
}

func createDeviceGroup(logger *zap.Logger, deviceIndices []uint) (dcgm.GroupHandle, error) {
	deviceGroupName := "google-cloud-ops-agent-group"
	deviceGroup, err := dcgm.CreateGroup(deviceGroupName)
	if err != nil {
		return dcgm.GroupHandle{}, fmt.Errorf("Unable to create DCGM GPU group '%s' on %w", deviceGroupName, err)
	}

	for _, gpuIndex := range deviceIndices {
		err = dcgm.AddToGroup(deviceGroup, gpuIndex)
		if err != nil {
			return dcgm.GroupHandle{}, fmt.Errorf("Unable add NVIDIA device %d to GPU group '%s' on %w", gpuIndex, deviceGroupName, err)
		}
	}

	logger.Sugar().Infof("Created GPU group '%s'", deviceGroupName)
	return deviceGroup, nil
}

func discoverRequestedFieldIDs(config *Config) []dcgm.Short {
	requestedFieldIDs := []dcgm.Short{}
	if config.Metrics.GpuDcgmUtilization.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_GR_ENGINE_ACTIVE"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_GPU_UTIL"]) // fallback
	}
	if config.Metrics.GpuDcgmSmUtilization.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_SM_ACTIVE"])
	}
	if config.Metrics.GpuDcgmSmOccupancy.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_SM_OCCUPANCY"])
	}
	if config.Metrics.GpuDcgmPipeUtilization.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_PIPE_TENSOR_ACTIVE"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_PIPE_FP64_ACTIVE"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_PIPE_FP32_ACTIVE"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_PIPE_FP16_ACTIVE"])
	}
	if config.Metrics.GpuDcgmCodecEncoderUtilization.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_ENC_UTIL"])
	}
	if config.Metrics.GpuDcgmCodecDecoderUtilization.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_DEC_UTIL"])
	}
	if config.Metrics.GpuDcgmMemoryBytesUsed.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_FB_FREE"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_FB_USED"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_FB_RESERVED"])
	}
	if config.Metrics.GpuDcgmMemoryBandwidthUtilization.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_DRAM_ACTIVE"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_MEM_COPY_UTIL"]) // fallback
	}
	if config.Metrics.GpuDcgmPcieTraffic.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_PCIE_TX_BYTES"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_PCIE_RX_BYTES"])
	}
	if config.Metrics.GpuDcgmNvlinkTraffic.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_NVLINK_TX_BYTES"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_PROF_NVLINK_RX_BYTES"])
	}
	if config.Metrics.GpuDcgmEnergyConsumption.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_POWER_USAGE"]) // fallback
	}
	if config.Metrics.GpuDcgmTemperature.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_GPU_TEMP"])
	}
	if config.Metrics.GpuDcgmClockFrequency.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_SM_CLOCK"])
	}
	if config.Metrics.GpuDcgmClockThrottleDurationTime.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_POWER_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_THERMAL_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_SYNC_BOOST_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_BOARD_LIMIT_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_LOW_UTIL_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_RELIABILITY_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION"])
	}
	if config.Metrics.GpuDcgmEccErrors.Enabled {
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_ECC_SBE_VOL_TOTAL"])
		requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI["DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"])
	}
	if config.Metrics.GpuDcgmXidErrors.Enabled {
		//requestedFieldIDs = append(requestedFieldIDs, dcgm.DCGM_FI[""])
	}

	return requestedFieldIDs
}

// getSupportedProfilingFields calls the DCGM query function to find out all
// profiling fields that are supported by the current GPUs
func getSupportedProfilingFields() ([]dcgm.Short, error) {
	supported := []dcgm.Short{}
	// GetSupportedMetricGroups currently does not support passing the actual
	// group handle; here we pass 0 to query supported fields for group 0, which
	// is the default DCGM group that is **supposed** to include all GPUs of the
	// host.
	fieldGroups, err := dcgm.GetSupportedMetricGroups(0)
	if err != nil {
		var dcgmErr *dcgm.DcgmError
		if errors.As(err, &dcgmErr) {
			// When the device does not support profiling metrics, this function
			// will return DCGM_ST_MODULE_NOT_LOADED:
			// "This request is serviced by a module of DCGM that is not
			// currently loaded." Example of this is NVIDIA P4
			if dcgmErr.Code == dcgm.DCGM_ST_MODULE_NOT_LOADED {
				return supported, nil
			}
		}
		return supported, err
	}
	for i := 0; i < len(fieldGroups); i++ {
		for j := 0; j < len(fieldGroups[i].FieldIds); j++ {
			supported = append(supported, dcgm.Short(fieldGroups[i].FieldIds[j]))
		}
	}
	return supported, nil
}

// filterSupportedFields takes the user requested fields and device supported
// profiling fields, and filters to return those that are requested & supported
// to be the enabledFields and requested but not supported as unavailableFields
func filterSupportedFields(requestedFields []dcgm.Short, supportedProfilingFields []dcgm.Short) ([]dcgm.Short, []dcgm.Short) {
	var enabledFields []dcgm.Short
	var unavailableFields []dcgm.Short
	for _, ef := range requestedFields {
		support := false
		if ef < dcgmProfilingFieldsStart {
			// Fields like `DCGM_FI_DEV_*` are not profiling
			// fields, and they are always supported on all devices
			support = true
		}
		for _, sf := range supportedProfilingFields {
			if sf == ef {
				support = true
				break
			}
		}
		if support {
			enabledFields = append(enabledFields, ef)
		} else {
			unavailableFields = append(unavailableFields, ef)
		}
	}
	return enabledFields, unavailableFields
}

func setWatchesOnEnabledFields(config *Config, logger *zap.Logger, deviceGroup dcgm.GroupHandle, enabledFieldIDs []dcgm.Short) (dcgm.FieldHandle, error) {
	var err error

	// Note: Add random suffix to avoid conflict amongnst any parallel collectors
	fieldGroupName := fmt.Sprintf("google-cloud-ops-agent-metrics-%d", randSource.Intn(10000))
	enabledFieldGroup, err := dcgm.FieldGroupCreate(fieldGroupName, enabledFieldIDs)
	if err != nil {
		return dcgm.FieldHandle{}, fmt.Errorf("Unable to create DCGM field group '%s'", fieldGroupName)
	}

	msg := fmt.Sprintf("Created DCGM field group '%s' with field ids: ", fieldGroupName)
	for _, fieldID := range enabledFieldIDs {
		msg += fmt.Sprintf("%d ", fieldID)
	}
	logger.Sugar().Info(msg)

	// Note: DCGM retained samples = Max(maxKeepSamples, maxKeepTime/updateFreq)
	dcgmUpdateFreq := int64(config.CollectionInterval / time.Microsecond)
	dcgmMaxKeepTime := 600.0 /* 10 min */
	dcgmMaxKeepSamples := int32(15)
	err = dcgm.WatchFieldsWithGroupEx(enabledFieldGroup, deviceGroup, dcgmUpdateFreq, dcgmMaxKeepTime, dcgmMaxKeepSamples)
	if err != nil {
		return dcgm.FieldHandle{}, fmt.Errorf("Setting watches for DCGM field group '%s' failed on %w", fieldGroupName, err)
	}
	logger.Sugar().Infof("Setting watches for DCGM field group '%s' succeeded", fieldGroupName)

	return enabledFieldGroup, nil
}

func (client *dcgmClient) cleanup() {
	if client.handleCleanup != nil {
		client.handleCleanup()
	}

	client.logger.Info("Shutdown DCGM")
}

func (client *dcgmClient) getDeviceModelName(gpuIndex uint) string {
	return client.devicesModelName[gpuIndex]
}

func (client *dcgmClient) getDeviceUUID(gpuIndex uint) string {
	return client.devicesUUID[gpuIndex]
}

func (client *dcgmClient) collectDeviceMetrics() (map[uint][]dcgmMetric, error) {
	var err scrapererror.ScrapeErrors
	gpuMetrics := make(map[uint][]dcgmMetric)
	for _, gpuIndex := range client.deviceIndices {
		fieldValues, pollErr := dcgmGetLatestValuesForFields(gpuIndex, client.enabledFieldIDs)
		if pollErr == nil {
			gpuMetrics[gpuIndex] = client.appendMetric(gpuMetrics[gpuIndex], gpuIndex, fieldValues)
			client.logger.Debugf("Successful poll of DCGM daemon for GPU %d", gpuIndex)
		} else {
			msg := fmt.Sprintf("Unable to poll DCGM daemon for GPU %d on %s", gpuIndex, pollErr)
			client.issueWarningForFailedQueryUptoThreshold(gpuIndex, "all-profiling-metrics", msg)
			err.AddPartial(1, fmt.Errorf("%s", msg))
		}
	}

	return gpuMetrics, err.Combine()
}

func (client *dcgmClient) appendMetric(gpuMetrics []dcgmMetric, gpuIndex uint, fieldValues []dcgm.FieldValue_v1) []dcgmMetric {
	for _, fieldValue := range fieldValues {
		dcgmName := dcgmIDToName[dcgm.Short(fieldValue.FieldId)]
		if err := isValidValue(fieldValue); err != nil {
			msg := fmt.Sprintf("Received invalid value (ts %d gpu %d) %s: %v", fieldValue.Ts, gpuIndex, dcgmName, err)
			client.issueWarningForFailedQueryUptoThreshold(gpuIndex, dcgmName, msg)
			continue
		}

		switch fieldValue.FieldType {
		case dcgm.DCGM_FT_DOUBLE:
			client.logger.Debugf("Discovered (ts %d gpu %d) %s = %.3f (f64)", fieldValue.Ts, gpuIndex, dcgmName, fieldValue.Float64())
		case dcgm.DCGM_FT_INT64:
			client.logger.Debugf("Discovered (ts %d gpu %d) %s = %d (i64)", fieldValue.Ts, gpuIndex, dcgmName, fieldValue.Int64())
		}
		gpuMetrics = append(gpuMetrics, dcgmMetric{fieldValue.Ts, dcgmName, fieldValue.Value})
	}

	return gpuMetrics
}

func (client *dcgmClient) issueWarningForFailedQueryUptoThreshold(deviceIdx uint, dcgmName string, reason string) {
	deviceMetric := fmt.Sprintf("device%d.%s", deviceIdx, dcgmName)
	client.deviceMetricToFailedQueryCount[deviceMetric]++

	failedCount := client.deviceMetricToFailedQueryCount[deviceMetric]
	if failedCount <= maxWarningsForFailedDeviceMetricQuery {
		client.logger.Warnf("Unable to query '%s' for Nvidia device %d on '%s'", dcgmName, deviceIdx, reason)
		if failedCount == maxWarningsForFailedDeviceMetricQuery {
			client.logger.Warnf("Surpressing further device query warnings for '%s' for Nvidia device %d", dcgmName, deviceIdx)
		}
	}
}
