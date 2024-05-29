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
			case "DCGM_FI_DEV_GPU_UTIL":
				gpuUtil := float64(metric.asInt64()) / 100.0 /* normalize */
				s.mb.RecordDcgmGpuUtilizationDataPoint(now, gpuUtil)
			case "DCGM_FI_DEV_FB_USED":
				bytesUsed := 1e6 * metric.asInt64() /* MB to B */
				s.mb.RecordDcgmGpuMemoryBytesUsedDataPoint(now, bytesUsed, metadata.AttributeMemoryStateUsed)
			case "DCGM_FI_DEV_FB_FREE":
				bytesFree := 1e6 * metric.asInt64() /* MB to B */
				s.mb.RecordDcgmGpuMemoryBytesUsedDataPoint(now, bytesFree, metadata.AttributeMemoryStateFree)
			case "DCGM_FI_PROF_SM_ACTIVE":
				s.mb.RecordDcgmGpuProfilingSmUtilizationDataPoint(now, metric.asFloat64())
			case "DCGM_FI_PROF_SM_OCCUPANCY":
				s.mb.RecordDcgmGpuProfilingSmOccupancyDataPoint(now, metric.asFloat64())
			case "DCGM_FI_PROF_PIPE_TENSOR_ACTIVE":
				s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributePipeTensor)
			case "DCGM_FI_PROF_PIPE_FP64_ACTIVE":
				s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributePipeFp64)
			case "DCGM_FI_PROF_PIPE_FP32_ACTIVE":
				s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributePipeFp32)
			case "DCGM_FI_PROF_PIPE_FP16_ACTIVE":
				s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), metadata.AttributePipeFp16)
			case "DCGM_FI_PROF_DRAM_ACTIVE":
				s.mb.RecordDcgmGpuProfilingDramUtilizationDataPoint(now, metric.asFloat64())
			case "DCGM_FI_PROF_PCIE_TX_BYTES":
				/* DCGM already returns these as bytes/sec despite the name */
				s.mb.RecordDcgmGpuProfilingPcieTrafficRateDataPoint(now, metric.asInt64(), metadata.AttributeDirectionTx)
			case "DCGM_FI_PROF_PCIE_RX_BYTES":
				s.mb.RecordDcgmGpuProfilingPcieTrafficRateDataPoint(now, metric.asInt64(), metadata.AttributeDirectionRx)
			case "DCGM_FI_PROF_NVLINK_TX_BYTES":
				s.mb.RecordDcgmGpuProfilingNvlinkTrafficRateDataPoint(now, metric.asInt64(), metadata.AttributeDirectionTx)
			case "DCGM_FI_PROF_NVLINK_RX_BYTES":
				s.mb.RecordDcgmGpuProfilingNvlinkTrafficRateDataPoint(now, metric.asInt64(), metadata.AttributeDirectionRx)
			}
		}
		s.mb.EmitForResource(metadata.WithResource(gpuResource))
	}

	return s.mb.Emit(), err
}
