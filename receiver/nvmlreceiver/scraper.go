// Copyright 2022 Google LLC
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

package nvmlreceiver

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver/internal/metadata"
)

type nvmlScraper struct {
	config   *Config
	settings component.ReceiverCreateSettings
	client   *nvmlClient
	mb       *metadata.MetricsBuilder
}

func newNvmlScraper(config *Config, settings component.ReceiverCreateSettings) (*nvmlScraper, error) {
	return &nvmlScraper{config: config, settings: settings}, nil
}

func (s *nvmlScraper) start(_ context.Context, host component.Host) error {
	var err error
	s.client, err = newClient(s.config, s.settings.Logger)
	if err != nil {
		return err
	}

	starttime := pcommon.NewTimestampFromTime(time.Now())
	s.mb = metadata.NewMetricsBuilder(
		s.config.Metrics, s.settings.BuildInfo, metadata.WithStartTime(starttime))

	return nil
}

func (s *nvmlScraper) stop(_ context.Context) error {
	s.client.cleanup()
	return nil
}

func (s *nvmlScraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	deviceMetrics, err := s.client.collectDeviceMetrics()

	for _, metric := range deviceMetrics {
		timestamp := pcommon.NewTimestampFromTime(metric.time)
		model := s.client.getDeviceModelName(metric.gpuID)
		gpuID := fmt.Sprintf("%d", metric.gpuID)
		switch metric.name {
		case "nvml.gpu.utilization":
			s.mb.RecordNvmlGpuUtilizationDataPoint(timestamp, metric.asFloat64(), model, gpuID)
		case "nvml.gpu.memory.bytes_used":
			s.mb.RecordNvmlGpuMemoryBytesUsedDataPoint(timestamp, metric.asInt64(), model, gpuID, metadata.AttributeMemoryStateUsed)
		case "nvml.gpu.memory.bytes_free":
			s.mb.RecordNvmlGpuMemoryBytesUsedDataPoint(timestamp, metric.asInt64(), model, gpuID, metadata.AttributeMemoryStateFree)
		}
	}

	return s.mb.Emit(), err
}
