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

//go:build !windows
// +build !windows

package dcgmreceiver

import (
	"context"
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

func (s *dcgmScraper) start(_ context.Context, host component.Host) error {
	var err error
	s.client, err = newClient(s.config, s.settings.Logger)
	if err != nil {
		return err
	}

	startTime := pcommon.NewTimestampFromTime(time.Now())
	s.mb = metadata.NewMetricsBuilder(
		s.config.Metrics, s.settings.BuildInfo, metadata.WithStartTime(startTime))

	return nil
}

func (s *dcgmScraper) stop(_ context.Context) error {
	if s.client != nil {
		s.client.cleanup()
	}
	return nil
}

func (s *dcgmScraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	deviceMetrics, err := s.client.collectDeviceMetrics()

	now := pcommon.NewTimestampFromTime(time.Now())
	for _, metric := range deviceMetrics {
		model := s.client.getDeviceModelName(metric.gpuIndex)
		UUID := s.client.getDeviceUUID(metric.gpuIndex)
		gpuIndex := fmt.Sprintf("%d", metric.gpuIndex)
		switch metric.name {
		case "dcgm.gpu.utilization":
			gpuUtil := float64(metric.asInt64()) / 100.0 /* normalize */
			s.mb.RecordDcgmGpuUtilizationDataPoint(now, gpuUtil, model, gpuIndex, UUID)
		case "dcgm.gpu.memory.bytes_used":
			bytesUsed := 1e6 * metric.asInt64() /* MB to B */
			s.mb.RecordDcgmGpuMemoryBytesUsedDataPoint(now, bytesUsed, model, gpuIndex, UUID, metadata.AttributeMemoryStateUsed)
		case "dcgm.gpu.memory.bytes_free":
			bytesFree := 1e6 * metric.asInt64() /* MB to B */
			s.mb.RecordDcgmGpuMemoryBytesUsedDataPoint(now, bytesFree, model, gpuIndex, UUID, metadata.AttributeMemoryStateFree)
		case "dcgm.gpu.profiling.sm_utilization":
			s.mb.RecordDcgmGpuProfilingSmUtilizationDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID)
		case "dcgm.gpu.profiling.sm_occupancy":
			s.mb.RecordDcgmGpuProfilingSmOccupancyDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID)
		case "dcgm.gpu.profiling.tensor_utilization":
			s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID, metadata.AttributePipeTensor)
		case "dcgm.gpu.profiling.fp64_utilization":
			s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID, metadata.AttributePipeFp64)
		case "dcgm.gpu.profiling.fp32_utilization":
			s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID, metadata.AttributePipeFp32)
		case "dcgm.gpu.profiling.fp16_utilization":
			s.mb.RecordDcgmGpuProfilingPipeUtilizationDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID, metadata.AttributePipeFp16)
		case "dcgm.gpu.profiling.dram_utilization":
			s.mb.RecordDcgmGpuProfilingDramUtilizationDataPoint(now, metric.asFloat64(), model, gpuIndex, UUID)
		case "dcgm.gpu.profiling.pcie_sent_bytes":
			/* DCGM already returns these as bytes/sec despite the name */
			s.mb.RecordDcgmGpuProfilingPcieTrafficRateDataPoint(now, metric.asInt64(), model, gpuIndex, UUID, metadata.AttributeDirectionTx)
		case "dcgm.gpu.profiling.pcie_received_bytes":
			s.mb.RecordDcgmGpuProfilingPcieTrafficRateDataPoint(now, metric.asInt64(), model, gpuIndex, UUID, metadata.AttributeDirectionRx)
		case "dcgm.gpu.profiling.nvlink_sent_bytes":
			s.mb.RecordDcgmGpuProfilingNvlinkTrafficRateDataPoint(now, metric.asInt64(), model, gpuIndex, UUID, metadata.AttributeDirectionTx)
		case "dcgm.gpu.profiling.nvlink_received_bytes":
			s.mb.RecordDcgmGpuProfilingNvlinkTrafficRateDataPoint(now, metric.asInt64(), model, gpuIndex, UUID, metadata.AttributeDirectionRx)
		}
	}

	return s.mb.Emit(), err
}
