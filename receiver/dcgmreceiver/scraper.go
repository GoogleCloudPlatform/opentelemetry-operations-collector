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
	"golang.org/x/sync/errgroup"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/internal/metadata"
)

type dcgmScraper struct {
	config    *Config
	settings  receiver.CreateSettings
	mb        *metadata.MetricsBuilder
	metricsCh <-chan map[uint]deviceMetrics
	cancel    func()
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

	metricsCh := make(chan map[uint]deviceMetrics)
	s.metricsCh = metricsCh

	g.Go(func() error {
		return s.run(scrapeCtx, metricsCh)
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

func (s *dcgmScraper) run(ctx context.Context, metricsCh chan<- map[uint]deviceMetrics) error {
	for {
		client, _ := s.initClient()
		// Ignore the error; it's logged in initClient.
		if client != nil {
			s.runOnce(ctx, client, metricsCh)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return nil
}

func (s *dcgmScraper) runOnce(ctx context.Context, client *dcgmClient, metricsCh chan<- map[uint]deviceMetrics) {
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
		deviceMetrics := client.getDeviceMetrics()
		select {
		case <-ctx.Done():
			return
		case metricsCh <- deviceMetrics:
		case <-time.After(waitTime):
		}
	}
}

func (s *dcgmScraper) scrape(_ context.Context) (pmetric.Metrics, error) {
	deviceMetrics := <-s.metricsCh
	s.settings.Logger.Sugar().Debugf("Metrics collected: %d", len(deviceMetrics))

	now := pcommon.NewTimestampFromTime(time.Now())
	for gpuIndex, gpu := range deviceMetrics {
		s.settings.Logger.Sugar().Debugf("Got %d unique metrics: %v", len(gpu.Metrics), gpu.Metrics)
		rb := s.mb.NewResourceBuilder()
		rb.SetGpuNumber(fmt.Sprintf("%d", gpuIndex))
		rb.SetGpuUUID(gpu.UUID)
		rb.SetGpuModel(gpu.ModelName)
		gpuResource := rb.Emit()

		v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_GR_ENGINE_ACTIVE")
		if !ok {
			v, ok = gpu.Metrics.LastFloat64("DCGM_FI_DEV_GPU_UTIL")
			v /= 100.0 /* normalize */
		}
		if ok {
			s.mb.RecordGpuDcgmUtilizationDataPoint(now, v)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_SM_ACTIVE"); ok {
			s.mb.RecordGpuDcgmSmUtilizationDataPoint(now, v)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_SM_OCCUPANCY"); ok {
			s.mb.RecordGpuDcgmSmOccupancyDataPoint(now, v)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_PIPE_TENSOR_ACTIVE"); ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, v, metadata.AttributeGpuPipeTensor)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_PIPE_FP64_ACTIVE"); ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, v, metadata.AttributeGpuPipeFp64)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_PIPE_FP32_ACTIVE"); ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, v, metadata.AttributeGpuPipeFp32)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_PROF_PIPE_FP16_ACTIVE"); ok {
			s.mb.RecordGpuDcgmPipeUtilizationDataPoint(now, v, metadata.AttributeGpuPipeFp16)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_DEV_ENC_UTIL"); ok {
			s.mb.RecordGpuDcgmCodecEncoderUtilizationDataPoint(now, v/100.0) /* normalize */
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_DEV_DEC_UTIL"); ok {
			s.mb.RecordGpuDcgmCodecDecoderUtilizationDataPoint(now, v/100.0) /* normalize */
		}
		if v, ok := gpu.Metrics.LastInt64("DCGM_FI_DEV_FB_FREE"); ok {
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, 1e6*v, metadata.AttributeGpuMemoryStateFree) /* MBy to By */
		}
		if v, ok := gpu.Metrics.LastInt64("DCGM_FI_DEV_FB_USED"); ok {
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, 1e6*v, metadata.AttributeGpuMemoryStateUsed) /* MBy to By */
		}
		if v, ok := gpu.Metrics.LastInt64("DCGM_FI_DEV_FB_RESERVED"); ok {
			s.mb.RecordGpuDcgmMemoryBytesUsedDataPoint(now, 1e6*v, metadata.AttributeGpuMemoryStateReserved) /* MBy to By */
		}
		v, ok = gpu.Metrics.LastFloat64("DCGM_FI_PROF_DRAM_ACTIVE")
		if !ok { // fallback
			v, ok = gpu.Metrics.LastFloat64("DCGM_FI_DEV_MEM_COPY_UTIL")
			v /= 100.0 /* normalize */
		}
		if ok {
			s.mb.RecordGpuDcgmMemoryBandwidthUtilizationDataPoint(now, v)
		}
		if v, ok := gpu.Metrics.IntegratedRate("DCGM_FI_PROF_PCIE_TX_BYTES"); ok {
			s.mb.RecordGpuDcgmPcieIoDataPoint(now, v, metadata.AttributeNetworkIoDirectionTransmit)
		}
		if v, ok := gpu.Metrics.IntegratedRate("DCGM_FI_PROF_PCIE_RX_BYTES"); ok {
			s.mb.RecordGpuDcgmPcieIoDataPoint(now, v, metadata.AttributeNetworkIoDirectionReceive)
		}
		if v, ok := gpu.Metrics.IntegratedRate("DCGM_FI_PROF_NVLINK_TX_BYTES"); ok {
			s.mb.RecordGpuDcgmNvlinkIoDataPoint(now, v, metadata.AttributeNetworkIoDirectionTransmit)
		}
		if v, ok := gpu.Metrics.IntegratedRate("DCGM_FI_PROF_NVLINK_RX_BYTES"); ok {
			s.mb.RecordGpuDcgmNvlinkIoDataPoint(now, v, metadata.AttributeNetworkIoDirectionReceive)
		}
		i, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION")
		v = float64(i) / 1e3 /* mJ to J */
		if !ok {             // fallback
			i, ok = gpu.Metrics.IntegratedRate("DCGM_FI_DEV_POWER_USAGE")
			v = float64(i)
		}
		if ok {
			s.mb.RecordGpuDcgmEnergyConsumptionDataPoint(now, v)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_DEV_GPU_TEMP"); ok {
			s.mb.RecordGpuDcgmTemperatureDataPoint(now, v)
		}
		if v, ok := gpu.Metrics.LastFloat64("DCGM_FI_DEV_SM_CLOCK"); ok {
			s.mb.RecordGpuDcgmClockFrequencyDataPoint(now, 1e6*v) /* MHz to Hz */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_POWER_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationPower) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_THERMAL_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationThermal) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_SYNC_BOOST_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationSyncBoost) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_BOARD_LIMIT_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationBoardLimit) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_LOW_UTIL_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationLowUtil) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_RELIABILITY_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationReliability) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationAppClock) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION"); ok {
			s.mb.RecordGpuDcgmClockThrottleDurationTimeDataPoint(now, float64(v)/1e6, metadata.AttributeGpuClockViolationBaseClock) /* µs to s */
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_ECC_SBE_VOL_TOTAL"); ok {
			s.mb.RecordGpuDcgmEccErrorsDataPoint(now, v, metadata.AttributeGpuErrorTypeSbe)
		}
		if v, ok := gpu.Metrics.CumulativeTotal("DCGM_FI_DEV_ECC_DBE_VOL_TOTAL"); ok {
			s.mb.RecordGpuDcgmEccErrorsDataPoint(now, v, metadata.AttributeGpuErrorTypeDbe)
		}
		// TODO: XID errors.
		// s.mb.RecordGpuDcgmXidErrorsDataPoint(now, metric.asInt64(), xid)
		s.mb.EmitForResource(metadata.WithResource(gpuResource))
	}

	return s.mb.Emit(), nil
}
