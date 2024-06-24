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
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/internal/metadata"
)

type dcgmScraper struct {
	config   *Config
	settings receiver.CreateSettings
	client   *dcgmClient
	mb       *metadata.MetricsBuilder
}

func newDcgmScraper(config *Config, settings receiver.CreateSettings) *dcgmScraper {
	return &dcgmScraper{config: config, settings: settings}
}

// initClient will try to create a new dcgmClient if currently has no client;
// it will try to initialize the communication with the DCGM service; if
// success, create a client; only return errors if DCGM service is available but
// failed to create client.
func (s *dcgmScraper) initClient() error {
	if s.client != nil {
		return nil
	}
	client, err := newClient(s.config, s.settings.Logger)
	if err != nil {
		s.settings.Logger.Sugar().Warn(err)
		if errors.Is(err, ErrDcgmInitialization) {
			// If cannot connect to DCGM, return no error and retry at next
			// collection time
			return nil
		}
		return err
	}
	s.client = client
	return nil
}

func (s *dcgmScraper) start(_ context.Context, _ component.Host) error {
	startTime := pcommon.NewTimestampFromTime(time.Now())
	mbConfig := metadata.DefaultMetricsBuilderConfig()
	mbConfig.Metrics = s.config.Metrics
	s.mb = metadata.NewMetricsBuilder(
		mbConfig, s.settings, metadata.WithStartTime(startTime))

	return nil
}

func (s *dcgmScraper) stop(_ context.Context) error {
	if s.client != nil {
		s.client.cleanup()
	}
	return nil
}

func (s *dcgmScraper) scrape(_ context.Context) (pmetric.Metrics, error) {
	err := s.initClient()
	if err != nil || s.client == nil {
		return s.mb.Emit(), err
	}

	deviceMetrics, err := s.client.collectDeviceMetrics()

	now := pcommon.NewTimestampFromTime(time.Now())
	for gpuIndex, metrics := range deviceMetrics {
		rb := s.mb.NewResourceBuilder()
		rb.SetGpuNumber(fmt.Sprintf("%d", gpuIndex))
		rb.SetGpuUUID(s.client.getDeviceUUID(gpuIndex))
		rb.SetGpuModel(s.client.getDeviceModelName(gpuIndex))
		gpuResource := rb.Emit()
		for _, metric := range metrics {
			switch metric.name {
			case "DCGM_FI_PROF_GR_ENGINE_ACTIVE":
				s.mb.RecordGpuDcgmUtilizationDataPoint(now, metric.asFloat64())
			// TODO: fallback
			//case "DCGM_FI_DEV_GPU_UTIL":
			//	gpuUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			//	s.mb.RecordGpuDcgmUtilizationDataPoint(now, gpuUtil)
			case "DCGM_FI_PROF_SM_ACTIVE":
				s.mb.RecordGpuDcgmSmUtilizationDataPoint(now, metric.asFloat64())
			case "DCGM_FI_PROF_SM_OCCUPANCY":
				s.mb.RecordGpuDcgmSmOccupancyDataPoint(now, metric.asFloat64())
			case "DCGM_FI_PROF_PIPE_TENSOR_ACTIVE":
				s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeTensor)
			case "DCGM_FI_PROF_PIPE_FP64_ACTIVE":
				s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeFp64)
			case "DCGM_FI_PROF_PIPE_FP32_ACTIVE":
				s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeFp32)
			case "DCGM_FI_PROF_PIPE_FP16_ACTIVE":
				s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeFp16)
			case "DCGM_FI_DEV_ENC_UTIL":
				encUtil := float64(metric.asInt64()) / 100.0 /* normalize */
				s.mb.RecordGpuDcgmCodecEncoderUtilizationDataPoint(now, encUtil)
			case "DCGM_FI_DEV_DEC_UTIL":
				decUtil := float64(metric.asInt64()) / 100.0 /* normalize */
				s.mb.RecordGpuDcgmCodecDecoderUtilizationDataPoint(now, decUtil)
			case "DCGM_FI_DEV_FB_FREE":
				bytesFree := 1e6 * metric.asInt64() /* MBy to By */
				s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, bytesFree, metadata.AttributeGpuMemoryStateFree)
			case "DCGM_FI_DEV_FB_USED":
				bytesUsed := 1e6 * metric.asInt64() /* MBy to By */
				s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, bytesUsed, metadata.AttributeGpuMemoryStateUsed)
			case "DCGM_FI_DEV_FB_RESERVED":
				bytesFree := 1e6 * metric.asInt64() /* MBy to By */
				s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, bytesFree, metadata.AttributeGpuMemoryStateReserved)
			case "DCGM_FI_PROF_DRAM_ACTIVE":
				s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(now, metric.asFloat64())
			// TODO: fallback
			//case "DCGM_FI_DEV_MEM_COPY_UTIL":
			//	memCopyUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			//	s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(now, memCopyUtil)
			case "DCGM_FI_PROF_PCIE_TX_BYTES":
				pcieTx := int64(float64(metric.asInt64()) * (s.config.CollectionInterval.Seconds())) /* rate to delta */
				s.mb.RecordGpuDcgmPcieIoDataPoint(now, pcieTx, metadata.AttributeNetworkIoDirectionTransmit)
			case "DCGM_FI_PROF_PCIE_RX_BYTES":
				pcieRx := int64(float64(metric.asInt64()) * (s.config.CollectionInterval.Seconds())) /* rate to delta */
				s.mb.RecordGpuDcgmPcieIoDataPoint(now, pcieRx, metadata.AttributeNetworkIoDirectionReceive)
			case "DCGM_FI_PROF_NVLINK_TX_BYTES":
				nvlinkTx := int64(float64(metric.asInt64()) * (s.config.CollectionInterval.Seconds())) /* rate to delta */
				s.mb.RecordGpuDcgmNvlinkIoDataPoint(now, nvlinkTx, metadata.AttributeNetworkIoDirectionTransmit)
			case "DCGM_FI_PROF_NVLINK_RX_BYTES":
				nvlinkRx := int64(float64(metric.asInt64()) * (s.config.CollectionInterval.Seconds())) /* rate to delta */
				s.mb.RecordGpuDcgmNvlinkIoDataPoint(now, nvlinkRx, metadata.AttributeNetworkIoDirectionReceive)
			case "DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION":
				s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(now, metric.asFloat64())
			// TODO: fallback
			//case "DCGM_FI_DEV_POWER_USAGE":
			//	powerUsage := metric.asFloat64() * (s.config.CollectionInterval.Seconds()) /* rate to delta */ // TODO: cumulative
			//	s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(now, powerUsage)
			case "DCGM_FI_DEV_GPU_TEMP":
				s.mb.RecordGpuDcgmTemperatureDataPoint(now, metric.asFloat64())
			case "DCGM_FI_DEV_SM_CLOCK":
				clockFreq := 1e6 * metric.asFloat64() /* MHz to Hz */
				s.mb.RecordGpuDcgmClockFrequencyDataPoint(now, clockFreq)
			case "DCGM_FI_DEV_POWER_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationPower)
			case "DCGM_FI_DEV_THERMAL_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationThermal)
			case "DCGM_FI_DEV_SYNC_BOOST_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationSyncBoost)
			case "DCGM_FI_DEV_BOARD_LIMIT_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationBoardLimit)
			case "DCGM_FI_DEV_LOW_UTIL_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationLowUtil)
			case "DCGM_FI_DEV_RELIABILITY_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationReliability)
			case "DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationAppClock)
			case "DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION":
				violationTime := float64(metric.asInt64()) / 1e6 /* us to s */
				s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationBaseClock)
			case "DCGM_FI_DEV_ECC_SBE_VOL_TOTAL":
				s.mb.RecordGpuDcgmEccErrorsDataPoint(now, metric.asInt64(), metadata.AttributeGpuErrorTypeSbe)
			case "DCGM_FI_DEV_ECC_DBE_VOL_TOTAL":
				s.mb.RecordGpuDcgmEccErrorsDataPoint(now, metric.asInt64(), metadata.AttributeGpuErrorTypeDbe)
			}
		}
		// TODO: XID errors.
		//s.mb.RecordGpuDcgmXidErrorsDataPoint(now, metric.asInt64(), xid)
		s.mb.EmitForResource(metadata.WithResource(gpuResource))
	}

	return s.mb.Emit(), err
}
