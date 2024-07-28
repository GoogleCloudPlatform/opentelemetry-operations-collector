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
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/internal/metadata"
)

type dcgmScraper struct {
	config            *Config
	settings          receiver.CreateSettings
	client            *dcgmClient
	stopClientPolling chan bool
	mb                *metadata.MetricsBuilder
	// Resource set.
	devices map[uint]bool
	// Value trackers.
	aggregates map[string]*defaultMap[uint, typedMetricTracker]
	mu         sync.Mutex
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
	clientSettings := &dcgmClientSettings{
		endpoint:         s.config.TCPAddrConfig.Endpoint,
		pollingInterval:  s.config.CollectionInterval,
		fields:           discoverRequestedFields(s.config),
		retryBlankValues: true,
		maxRetries:       5,
	}
	client, err := newClient(clientSettings, s.settings.Logger)
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

func newRateIntegrator[V int64 | float64]() *rateIntegrator[V] {
	ri := new(rateIntegrator[V])
	ri.Reset()
	return ri
}

func newCumulativeTracker[V int64 | float64]() *cumulativeTracker[V] {
	ct := new(cumulativeTracker[V])
	ct.Reset()
	return ct
}

func newGaugeTracker[V int64 | float64]() *gaugeTracker[V] {
	gt := new(gaugeTracker[V])
	gt.Reset()
	return gt
}

func makeTypedMetricTracker[V int64 | float64, T metricTracker[V]](f func() T) func() typedMetricTracker {
	var tmt typedMetricTracker
	var m interface{} = f()
	switch v := m.(type) {
	case metricTracker[int64]:
		tmt.i64 = v
	case metricTracker[float64]:
		tmt.f64 = v
	}
	return func() typedMetricTracker { return tmt }
}

func newTypedMetricTrackerMap[V int64 | float64, T metricTracker[V]](f func() T) *defaultMap[uint, typedMetricTracker] {
	return newDefaultMap[uint](makeTypedMetricTracker(f))
}

func (s *dcgmScraper) start(_ context.Context, _ component.Host) error {
	startTime := pcommon.NewTimestampFromTime(time.Now())
	mbConfig := metadata.DefaultMetricsBuilderConfig()
	mbConfig.Metrics = s.config.Metrics
	s.mb = metadata.NewMetricsBuilder(
		mbConfig, s.settings, metadata.WithStartTime(startTime))
	s.aggregates = map[string]*defaultMap[uint, typedMetricTracker]{
		"DCGM_FI_PROF_GR_ENGINE_ACTIVE":           newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_DEV_GPU_UTIL":                    newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_PROF_SM_ACTIVE":                  newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_PROF_SM_OCCUPANCY":               newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_PROF_PIPE_TENSOR_ACTIVE":         newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_PROF_PIPE_FP64_ACTIVE":           newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_PROF_PIPE_FP32_ACTIVE":           newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_PROF_PIPE_FP16_ACTIVE":           newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_DEV_ENC_UTIL":                    newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_DEV_DEC_UTIL":                    newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_DEV_FB_FREE":                     newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_DEV_FB_USED":                     newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_DEV_FB_RESERVED":                 newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_PROF_DRAM_ACTIVE":                newTypedMetricTrackerMap(newGaugeTracker[float64]),
		"DCGM_FI_DEV_MEM_COPY_UTIL":               newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_PROF_PCIE_TX_BYTES":              newTypedMetricTrackerMap(newRateIntegrator[int64]),
		"DCGM_FI_PROF_PCIE_RX_BYTES":              newTypedMetricTrackerMap(newRateIntegrator[int64]),
		"DCGM_FI_PROF_NVLINK_TX_BYTES":            newTypedMetricTrackerMap(newRateIntegrator[int64]),
		"DCGM_FI_PROF_NVLINK_RX_BYTES":            newTypedMetricTrackerMap(newRateIntegrator[int64]),
		"DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION":    newTypedMetricTrackerMap(newCumulativeTracker[float64]),
		"DCGM_FI_DEV_POWER_USAGE":                 newTypedMetricTrackerMap(newRateIntegrator[float64]),
		"DCGM_FI_DEV_GPU_TEMP":                    newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_DEV_SM_CLOCK":                    newTypedMetricTrackerMap(newGaugeTracker[int64]),
		"DCGM_FI_DEV_POWER_VIOLATION":             newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_THERMAL_VIOLATION":           newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_SYNC_BOOST_VIOLATION":        newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_BOARD_LIMIT_VIOLATION":       newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_LOW_UTIL_VIOLATION":          newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_RELIABILITY_VIOLATION":       newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION":  newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION": newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_ECC_SBE_VOL_TOTAL":           newTypedMetricTrackerMap(newCumulativeTracker[int64]),
		"DCGM_FI_DEV_ECC_DBE_VOL_TOTAL":           newTypedMetricTrackerMap(newCumulativeTracker[int64]),
	}

	err := s.initClient()
	if err != nil {
		return err
	}
	s.stopClientPolling = make(chan bool)
	go s.clientPoller(10 * time.Second) // TODO

	return nil
}

func (s *dcgmScraper) stop(_ context.Context) error {
L:
	for i := 1; i < 2; i++ {
		select {
		// Doing this in a blocking fashion causes hangs.
		case s.stopClientPolling <- true:
			break L
		default:
			s.settings.Logger.Sugar().Debug("Stop signal ignored; trying again")
		}
	}
	if s.client != nil {
		s.client.cleanup()
	}
	return nil
}

func (s *dcgmScraper) collectClientMetrics() error {
	err := s.initClient()
	if err != nil || s.client == nil {
		return err
	}

	s.settings.Logger.Sugar().Debug("Client created, collecting metrics")

	deviceMetrics, err := s.client.collectDeviceMetrics()
	if err != nil {
		s.settings.Logger.Sugar().Warnf("Metrics not collected; err=%v", err)
		return err
	}
	s.settings.Logger.Sugar().Debugf("Metrics collected: %d", len(deviceMetrics))

	s.mu.Lock()
	defer s.mu.Unlock()
	for gpuIndex, gpuMetrics := range deviceMetrics {
		s.settings.Logger.Sugar().Debugf("Got %d metrics for %d: %v", len(gpuMetrics), gpuIndex, gpuMetrics)
		s.devices[gpuIndex] = true
		for _, metric := range gpuMetrics {
			tmt := s.aggregates[metric.name].Get(gpuIndex)
			switch v := metric.value.(type) {
			case int64:
				tmt.i64.Update(metric.timestamp, v)
			case float64:
				tmt.f64.Update(metric.timestamp, v)
			}
		}
	}
	return nil
}

func (s *dcgmScraper) clientPoller(interval time.Duration) {
	_ = s.collectClientMetrics()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = s.collectClientMetrics()
		case <-s.stopClientPolling:
			s.settings.Logger.Sugar().Info("Stopping client poller")
			return
		}
	}
}

func discoverRequestedFields(config *Config) []string {
	requestedFields := []string{}
	if config.Metrics.GpuDcgmUtilization.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_GR_ENGINE_ACTIVE")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_GPU_UTIL") // fallback
	}
	if config.Metrics.GpuDcgmSmUtilization.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_SM_ACTIVE")
	}
	if config.Metrics.GpuDcgmSmOccupancy.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_SM_OCCUPANCY")
	}
	if config.Metrics.GpuDcgmPipeUtilization.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_PIPE_TENSOR_ACTIVE")
		requestedFields = append(requestedFields, "DCGM_FI_PROF_PIPE_FP64_ACTIVE")
		requestedFields = append(requestedFields, "DCGM_FI_PROF_PIPE_FP32_ACTIVE")
		requestedFields = append(requestedFields, "DCGM_FI_PROF_PIPE_FP16_ACTIVE")
	}
	if config.Metrics.GpuDcgmCodecEncoderUtilization.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_ENC_UTIL")
	}
	if config.Metrics.GpuDcgmCodecDecoderUtilization.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_DEC_UTIL")
	}
	if config.Metrics.GpuDcgmMemoryBytesUsed.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_FB_FREE")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_FB_USED")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_FB_RESERVED")
	}
	if config.Metrics.GpuDcgmMemoryBandwidthUtilization.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_DRAM_ACTIVE")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_MEM_COPY_UTIL") // fallback
	}
	if config.Metrics.GpuDcgmPcieIo.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_PCIE_TX_BYTES")
		requestedFields = append(requestedFields, "DCGM_FI_PROF_PCIE_RX_BYTES")
	}
	if config.Metrics.GpuDcgmNvlinkIo.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_PROF_NVLINK_TX_BYTES")
		requestedFields = append(requestedFields, "DCGM_FI_PROF_NVLINK_RX_BYTES")
	}
	if config.Metrics.GpuDcgmEnergyConsumption.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_POWER_USAGE") // fallback
	}
	if config.Metrics.GpuDcgmTemperature.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_GPU_TEMP")
	}
	if config.Metrics.GpuDcgmClockFrequency.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_SM_CLOCK")
	}
	if config.Metrics.GpuDcgmClockThrottleDurationTime.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_POWER_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_THERMAL_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_SYNC_BOOST_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_BOARD_LIMIT_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_LOW_UTIL_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_RELIABILITY_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION")
	}
	if config.Metrics.GpuDcgmEccErrors.Enabled {
		requestedFields = append(requestedFields, "DCGM_FI_DEV_ECC_SBE_VOL_TOTAL")
		requestedFields = append(requestedFields, "DCGM_FI_DEV_ECC_DBE_VOL_TOTAL")
	}
	if config.Metrics.GpuDcgmXidErrors.Enabled {
		// requestedFields = append(requestedFields, "")
		func() {}() // no-op
	}

	return requestedFields
}

type point struct {
	timestamp int64
	value     interface{}
}

func (s *dcgmScraper) snapshotClientMetrics() map[uint]map[string]point {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics := make(map[uint]map[string]point)
	for gpuIndex := range s.devices {
		perDevice := make(map[string]point)
		metrics[gpuIndex] = perDevice
		// We have to iterate over all metrics for each device. This is not ideal.
		for name, points := range s.aggregates {
			if tmt, ok := points.TryGet(gpuIndex); ok {
				switch {
				case tmt.i64 != nil:
					ts, v := tmt.i64.Value()
					perDevice[name] = point{timestamp: ts, value: v}
				case tmt.f64 != nil:
					ts, v := tmt.f64.Value()
					perDevice[name] = point{timestamp: ts, value: v}
				}
			}
		}
	}
	return metrics
}

func (s *dcgmScraper) scrape(_ context.Context) (pmetric.Metrics, error) {
	clientMetrics := s.snapshotClientMetrics()
	for gpuIndex, metrics := range clientMetrics {
		rb := s.mb.NewResourceBuilder()
		rb.SetGpuNumber(fmt.Sprintf("%d", gpuIndex))
		rb.SetGpuUUID(s.client.getDeviceUUID(gpuIndex))
		rb.SetGpuModel(s.client.getDeviceModelName(gpuIndex))
		gpuResource := rb.Emit()
		if p, ok := metrics["DCGM_FI_PROF_GR_ENGINE_ACTIVE"]; ok {
			utilization := p.value.(float64)
			s.mb.RecordGpuDcgmUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), utilization)
		} else if p, ok := metrics["DCGM_FI_DEV_GPU_UTIL"]; ok { // fallback
			utilization := float64(p.value.(int64)) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), utilization)
		}
		if p, ok := metrics["DCGM_FI_PROF_SM_ACTIVE"]; ok {
			smActive := p.value.(float64)
			s.mb.RecordGpuDcgmSmUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), smActive)
		}
		if p, ok := metrics["DCGM_FI_PROF_SM_OCCUPANCY"]; ok {
			smOccupancy := p.value.(float64)
			s.mb.RecordGpuDcgmSmOccupancyDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), smOccupancy)
		}
		if p, ok := metrics["DCGM_FI_PROF_PIPE_TENSOR_ACTIVE"]; ok {
			pipeUtil := p.value.(float64)
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), pipeUtil, metadata.AttributeGpuPipeTensor)
		}
		if p, ok := metrics["DCGM_FI_PROF_PIPE_FP64_ACTIVE"]; ok {
			pipeUtil := p.value.(float64)
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), pipeUtil, metadata.AttributeGpuPipeFp64)
		}
		if p, ok := metrics["DCGM_FI_PROF_PIPE_FP32_ACTIVE"]; ok {
			pipeUtil := p.value.(float64)
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), pipeUtil, metadata.AttributeGpuPipeFp32)
		}
		if p, ok := metrics["DCGM_FI_PROF_PIPE_FP16_ACTIVE"]; ok {
			pipeUtil := p.value.(float64)
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), pipeUtil, metadata.AttributeGpuPipeFp16)
		}
		if p, ok := metrics["DCGM_FI_DEV_ENC_UTIL"]; ok {
			encUtil := float64(p.value.(int64)) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmCodecEncoderUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), encUtil)
		}
		if p, ok := metrics["DCGM_FI_DEV_DEC_UTIL"]; ok {
			decUtil := float64(p.value.(int64)) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmCodecDecoderUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), decUtil)
		}
		if p, ok := metrics["DCGM_FI_DEV_FB_FREE"]; ok {
			bytesFree := 1e6 * p.value.(int64) /* MBy to By */
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), bytesFree, metadata.AttributeGpuMemoryStateFree)
		}
		if p, ok := metrics["DCGM_FI_DEV_FB_USED"]; ok {
			bytesUsed := 1e6 * p.value.(int64) /* MBy to By */
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), bytesUsed, metadata.AttributeGpuMemoryStateUsed)
		}
		if p, ok := metrics["DCGM_FI_DEV_FB_RESERVED"]; ok {
			bytesReserved := 1e6 * p.value.(int64) /* MBy to By */
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), bytesReserved, metadata.AttributeGpuMemoryStateReserved)
		}
		if p, ok := metrics["DCGM_FI_PROF_DRAM_ACTIVE"]; ok {
			memCopyUtil := p.value.(float64)
			s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), memCopyUtil)
		} else if p, ok := metrics["DCGM_FI_DEV_MEM_COPY_UTIL"]; ok { // fallback
			memCopyUtil := float64(p.value.(int64)) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), memCopyUtil)
		}
		if p, ok := metrics["DCGM_FI_PROF_PCIE_TX_BYTES"]; ok {
			pcieTx := p.value.(int64)
			s.mb.RecordGpuDcgmPcieIoDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), pcieTx, metadata.AttributeNetworkIoDirectionTransmit)
		}
		if p, ok := metrics["DCGM_FI_PROF_PCIE_RX_BYTES"]; ok {
			pcieRx := p.value.(int64)
			s.mb.RecordGpuDcgmPcieIoDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), pcieRx, metadata.AttributeNetworkIoDirectionReceive)
		}
		if p, ok := metrics["DCGM_FI_PROF_NVLINK_TX_BYTES"]; ok {
			nvlinkTx := p.value.(int64)
			s.mb.RecordGpuDcgmNvlinkIoDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), nvlinkTx, metadata.AttributeNetworkIoDirectionTransmit)
		}
		if p, ok := metrics["DCGM_FI_PROF_NVLINK_RX_BYTES"]; ok {
			nvlinkRx := p.value.(int64)
			s.mb.RecordGpuDcgmNvlinkIoDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), nvlinkRx, metadata.AttributeNetworkIoDirectionReceive)
		}
		if p, ok := metrics["DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION"]; ok {
			energyUsed := float64(p.value.(int64)) / 1e3 /* mJ to J */
			s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), energyUsed)
		} else if p, ok := metrics["DCGM_FI_DEV_POWER_USAGE"]; ok { // fallback
			energyUsed := p.value.(float64)
			s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), energyUsed)
		}
		if p, ok := metrics["DCGM_FI_DEV_GPU_TEMP"]; ok {
			temperature := float64(p.value.(int64))
			s.mb.RecordGpuDcgmTemperatureDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), temperature)
		}
		if p, ok := metrics["DCGM_FI_DEV_SM_CLOCK"]; ok {
			clockFreq := 1e6 * float64(p.value.(int64)) /* MHz to Hz */
			s.mb.RecordGpuDcgmClockFrequencyDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), clockFreq)
		}
		if p, ok := metrics["DCGM_FI_DEV_POWER_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationPower)
		}
		if p, ok := metrics["DCGM_FI_DEV_THERMAL_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationThermal)
		}
		if p, ok := metrics["DCGM_FI_DEV_SYNC_BOOST_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationSyncBoost)
		}
		if p, ok := metrics["DCGM_FI_DEV_BOARD_LIMIT_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationBoardLimit)
		}
		if p, ok := metrics["DCGM_FI_DEV_LOW_UTIL_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationLowUtil)
		}
		if p, ok := metrics["DCGM_FI_DEV_RELIABILITY_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationReliability)
		}
		if p, ok := metrics["DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationAppClock)
		}
		if p, ok := metrics["DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION"]; ok {
			violationTime := float64(p.value.(int64)) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), violationTime, metadata.AttributeGpuClockViolationBaseClock)
		}
		if p, ok := metrics["DCGM_FI_DEV_ECC_SBE_VOL_TOTAL"]; ok {
			sbeErrors := p.value.(int64)
			s.mb.RecordGpuDcgmEccErrorsDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), sbeErrors, metadata.AttributeGpuErrorTypeSbe)
		}
		if p, ok := metrics["DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"]; ok {
			dbeErrors := p.value.(int64)
			s.mb.RecordGpuDcgmEccErrorsDataPoint(pcommon.NewTimestampFromTime(time.UnixMicro(p.timestamp)), dbeErrors, metadata.AttributeGpuErrorTypeDbe)
		}
		// TODO: XID errors.
		// s.mb.RecordGpuDcgmXidErrorsDataPoint(now, p.value.(int64), xid)
		s.mb.EmitForResource(metadata.WithResource(gpuResource))
	}

	return s.mb.Emit(), nil
}
