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
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"golang.org/x/sync/errgroup"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/internal/metadata"
)

type dcgmScraper struct {
	config   *Config
	settings receiver.CreateSettings
	mb       *metadata.MetricsBuilder
	cancel   func()
}

func newDcgmScraper(config *Config, settings receiver.CreateSettings) *dcgmScraper {
	return &dcgmScraper{config: config, settings: settings}
}

// initClient will try to initialize the communication with the DCGM service; if
// success, create a client; only return errors if DCGM service is available but
// failed to create client.
func (s *dcgmScraper) initClient() (*dcgmClient, error) {
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
			return nil, nil
		}
		return nil, err
	}
	return client, nil
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

func (s *dcgmScraper) start(ctx context.Context, _ component.Host) error {
	startTime := pcommon.NewTimestampFromTime(time.Now())
	mbConfig := metadata.DefaultMetricsBuilderConfig()
	mbConfig.Metrics = s.config.Metrics
	s.mb = metadata.NewMetricsBuilder(
		mbConfig, s.settings, metadata.WithStartTime(startTime))

	scrapeCtx, scrapeCancel := context.WithCancel(context.WithoutCancel(ctx))
	g, scrapeCtx := errgroup.WithContext(scrapeCtx)

	s.cancel = func() {
		scrapeCancel()
		g.Wait()
	}

	g.Go(func() error {
		return s.run(scrapeCtx)
	})

	return nil
}

func (s *dcgmScraper) stop(_ context.Context) error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
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

func (s *dcgmScraper) run(ctx context.Context) error {
	for {
		client, _ := s.initClient()
		// Ignore the error; it's logged in initClient.
		if client != nil {
			s.runOnce(ctx, client)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return nil
}

func (s *dcgmScraper) runOnce(ctx context.Context, client *dcgmClient) {
	for {
		waitTime, err := client.collect()
		// Ignore the error; it's logged in collect()
		if err != nil {
			waitTime = 10 * time.Second
		}
		// Try to poll at least twice per collection interval
		waitTime = max(
			100*time.Millisecond,
			min(
				s.config.CollectionInterval,
				waitTime,
			)/2,
		)
		select {
		case <-ctx.Done():
			return
		// FIXME: Allow collecting metrics here
		case <-time.After(waitTime):
		}
	}
}

func (s *dcgmScraper) scrape(_ context.Context) (pmetric.Metrics, error) {
	err := s.initClient()
	if err != nil || s.client == nil {
		return s.mb.Emit(), err
	}

	s.settings.Logger.Sugar().Debug("Client created, collecting metrics")
	deviceMetrics, err := s.client.collectDeviceMetrics()
	if err != nil {
		s.settings.Logger.Sugar().Warnf("Metrics not collected; err=%v", err)
		return s.mb.Emit(), err
	}
	s.settings.Logger.Sugar().Debugf("Metrics collected: %d", len(deviceMetrics))

	now := pcommon.NewTimestampFromTime(time.Now())
	for gpuIndex, gpuMetrics := range deviceMetrics {
		metricsByName := make(map[string][]dcgmMetric)
		for _, metric := range gpuMetrics {
			metricsByName[metric.name] = append(metricsByName[metric.name], metric)
		}
		s.settings.Logger.Sugar().Debugf("Got %d unique metrics: %v", len(metricsByName), metricsByName)
		metrics := make(map[string]dcgmMetric)
		for name, points := range metricsByName {
			slices.SortStableFunc(points, func(a, b dcgmMetric) int {
				return cmp.Compare(a.timestamp, b.timestamp)
			})
			metrics[name] = points[len(points)-1]
		}
		rb := s.mb.NewResourceBuilder()
		rb.SetGpuNumber(fmt.Sprintf("%d", gpuIndex))
		rb.SetGpuUUID(s.client.getDeviceUUID(gpuIndex))
		rb.SetGpuModel(s.client.getDeviceModelName(gpuIndex))
		gpuResource := rb.Emit()
		if metric, ok := metrics["DCGM_FI_PROF_GR_ENGINE_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmUtilizationDataPoint(now, metric.asFloat64())
		} else if metric, ok := metrics["DCGM_FI_DEV_GPU_UTIL"]; ok { // fallback
			gpuUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmUtilizationDataPoint(now, gpuUtil)
		}
		if metric, ok := metrics["DCGM_FI_PROF_SM_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmSmUtilizationDataPoint(now, metric.asFloat64())
		}
		if metric, ok := metrics["DCGM_FI_PROF_SM_OCCUPANCY"]; ok {
			s.mb.RecordGpuDcgmSmOccupancyDataPoint(now, metric.asFloat64())
		}
		if metric, ok := metrics["DCGM_FI_PROF_PIPE_TENSOR_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeTensor)
		}
		if metric, ok := metrics["DCGM_FI_PROF_PIPE_FP64_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeFp64)
		}
		if metric, ok := metrics["DCGM_FI_PROF_PIPE_FP32_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeFp32)
		}
		if metric, ok := metrics["DCGM_FI_PROF_PIPE_FP16_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributeGpuPipeFp16)
		}
		if metric, ok := metrics["DCGM_FI_DEV_ENC_UTIL"]; ok {
			encUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmCodecEncoderUtilizationDataPoint(now, encUtil)
		}
		if metric, ok := metrics["DCGM_FI_DEV_DEC_UTIL"]; ok {
			decUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmCodecDecoderUtilizationDataPoint(now, decUtil)
		}
		if metric, ok := metrics["DCGM_FI_DEV_FB_FREE"]; ok {
			bytesFree := 1e6 * metric.asInt64() /* MBy to By */
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, bytesFree, metadata.AttributeGpuMemoryStateFree)
		}
		if metric, ok := metrics["DCGM_FI_DEV_FB_USED"]; ok {
			bytesUsed := 1e6 * metric.asInt64() /* MBy to By */
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, bytesUsed, metadata.AttributeGpuMemoryStateUsed)
		}
		if metric, ok := metrics["DCGM_FI_DEV_FB_RESERVED"]; ok {
			bytesReserved := 1e6 * metric.asInt64() /* MBy to By */
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, bytesReserved, metadata.AttributeGpuMemoryStateReserved)
		}
		if metric, ok := metrics["DCGM_FI_PROF_DRAM_ACTIVE"]; ok {
			s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(now, metric.asFloat64())
		} else if metric, ok := metrics["DCGM_FI_DEV_MEM_COPY_UTIL"]; ok { // fallback
			memCopyUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(now, memCopyUtil)
		}
		if metric, ok := metrics["DCGM_FI_PROF_PCIE_TX_BYTES"]; ok {
			s.aggregates.pcieTotal.tx.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, pcieTx := s.aggregates.pcieTotal.tx.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmPcieIoDataPoint(now, pcieTx, metadata.AttributeNetworkIoDirectionTransmit)
		}
		if metric, ok := metrics["DCGM_FI_PROF_PCIE_RX_BYTES"]; ok {
			s.aggregates.pcieTotal.rx.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, pcieRx := s.aggregates.pcieTotal.rx.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmPcieIoDataPoint(now, pcieRx, metadata.AttributeNetworkIoDirectionReceive)
		}
		if metric, ok := metrics["DCGM_FI_PROF_NVLINK_TX_BYTES"]; ok {
			s.aggregates.nvlinkTotal.tx.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, nvlinkTx := s.aggregates.nvlinkTotal.tx.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmNvlinkIoDataPoint(now, nvlinkTx, metadata.AttributeNetworkIoDirectionTransmit)
		}
		if metric, ok := metrics["DCGM_FI_PROF_NVLINK_RX_BYTES"]; ok {
			s.aggregates.nvlinkTotal.rx.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, nvlinkRx := s.aggregates.nvlinkTotal.rx.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmNvlinkIoDataPoint(now, nvlinkRx, metadata.AttributeNetworkIoDirectionReceive)
		}
		if metric, ok := metrics["DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION"]; ok {
			s.aggregates.energyConsumption.total.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.energyConsumption.total.Get(gpuIndex).Value()
			energyUsed := float64(value) / 1e3 /* mJ to J */
			s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(now, energyUsed)
		} else if metric, ok := metrics["DCGM_FI_DEV_POWER_USAGE"]; ok { // fallback
			s.aggregates.energyConsumption.fallback.Get(gpuIndex).Update(metric.timestamp, metric.asFloat64())
			_, energyUsed := s.aggregates.energyConsumption.fallback.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(now, energyUsed)
		}
		if metric, ok := metrics["DCGM_FI_DEV_GPU_TEMP"]; ok {
			s.mb.RecordGpuDcgmTemperatureDataPoint(now, float64(metric.asInt64()))
		}
		if metric, ok := metrics["DCGM_FI_DEV_SM_CLOCK"]; ok {
			clockFreq := 1e6 * float64(metric.asInt64()) /* MHz to Hz */
			s.mb.RecordGpuDcgmClockFrequencyDataPoint(now, clockFreq)
		}
		if metric, ok := metrics["DCGM_FI_DEV_POWER_VIOLATION"]; ok {
			s.aggregates.throttleDuration.powerViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.powerViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationPower)
		}
		if metric, ok := metrics["DCGM_FI_DEV_THERMAL_VIOLATION"]; ok {
			s.aggregates.throttleDuration.thermalViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.thermalViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationThermal)
		}
		if metric, ok := metrics["DCGM_FI_DEV_SYNC_BOOST_VIOLATION"]; ok {
			s.aggregates.throttleDuration.syncBoostViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.syncBoostViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationSyncBoost)
		}
		if metric, ok := metrics["DCGM_FI_DEV_BOARD_LIMIT_VIOLATION"]; ok {
			s.aggregates.throttleDuration.boardLimitViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.boardLimitViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationBoardLimit)
		}
		if metric, ok := metrics["DCGM_FI_DEV_LOW_UTIL_VIOLATION"]; ok {
			s.aggregates.throttleDuration.lowUtilViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.lowUtilViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationLowUtil)
		}
		if metric, ok := metrics["DCGM_FI_DEV_RELIABILITY_VIOLATION"]; ok {
			s.aggregates.throttleDuration.reliabilityViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.reliabilityViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationReliability)
		}
		if metric, ok := metrics["DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION"]; ok {
			s.aggregates.throttleDuration.totalAppClocksViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.totalAppClocksViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationAppClock)
		}
		if metric, ok := metrics["DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION"]; ok {
			s.aggregates.throttleDuration.totalBaseClocksViolation.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, value := s.aggregates.throttleDuration.totalBaseClocksViolation.Get(gpuIndex).Value()
			violationTime := float64(value) / 1e6 /* us to s */
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, violationTime, metadata.AttributeGpuClockViolationBaseClock)
		}
		if metric, ok := metrics["DCGM_FI_DEV_ECC_SBE_VOL_TOTAL"]; ok {
			s.aggregates.eccTotal.sbe.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, sbeErrors := s.aggregates.eccTotal.sbe.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmEccErrorsDataPoint(now, sbeErrors, metadata.AttributeGpuErrorTypeSbe)
		}
		if metric, ok := metrics["DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"]; ok {
			s.aggregates.eccTotal.dbe.Get(gpuIndex).Update(metric.timestamp, metric.asInt64())
			_, dbeErrors := s.aggregates.eccTotal.dbe.Get(gpuIndex).Value()
			s.mb.RecordGpuDcgmEccErrorsDataPoint(now, dbeErrors, metadata.AttributeGpuErrorTypeDbe)
		}
		// TODO: XID errors.
		// s.mb.RecordGpuDcgmXidErrorsDataPoint(now, metric.asInt64(), xid)
		s.mb.EmitForResource(metadata.WithResource(gpuResource))
	}

	return s.mb.Emit(), err
}
