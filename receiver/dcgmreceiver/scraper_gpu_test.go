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

//go:build gpu && has_gpu
// +build gpu,has_gpu

package dcgmreceiver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap/zaptest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/testprofilepause"
)

func TestScrapeWithGpuPresent(t *testing.T) {
	var settings receiver.CreateSettings
	settings.Logger = zaptest.NewLogger(t)

	scraper := newDcgmScraper(createDefaultConfig().(*Config), settings)
	require.NotNil(t, scraper)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())
	expectedGroups := loadExpectedMetricGroupsFromGoldenFile(t, scraper.client.getDeviceModelName(0))
	validateScraperResult(t, metrics, expectedGroups)
}

func TestScrapeOnPollingError(t *testing.T) {
	realDcgmGetLatestValuesForFields := dcgmGetLatestValuesForFields
	defer func() { dcgmGetLatestValuesForFields = realDcgmGetLatestValuesForFields }()
	dcgmGetLatestValuesForFields = func(gpu uint, fields []dcgm.Short) ([]dcgm.FieldValue_v1, error) {
		return nil, fmt.Errorf("DCGM polling error")
	}

	var settings receiver.CreateSettings
	settings.Logger = zaptest.NewLogger(t)

	scraper := newDcgmScraper(createDefaultConfig().(*Config), settings)
	require.NotNil(t, scraper)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())

	assert.Error(t, err)
	assert.Equal(t, metrics.MetricCount(), 0)
}

func TestScrapeOnProfilingPaused(t *testing.T) {
	config := createDefaultConfig().(*Config)
	config.CollectionInterval = 10 * time.Millisecond

	var settings receiver.CreateSettings
	settings.Logger = zaptest.NewLogger(t)

	scraper := newDcgmScraper(config, settings)
	require.NotNil(t, scraper)

	defer func() { testprofilepause.ResumeProfilingMetrics() }()
	testprofilepause.PauseProfilingMetrics()
	time.Sleep(20 * time.Millisecond)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())

	assert.NoError(t, err)
	require.Equal(t, metrics.MetricCount(), 2)

	expectedMetrics := []string{
		"dcgm.gpu.utilization",
		"dcgm.gpu.memory.bytes_used",
	}

	ms := metrics.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	metricWasSeen := make(map[string]bool)
	for i := 0; i < ms.Len(); i++ {
		metricWasSeen[ms.At(i).Name()] = true
	}

	for _, metric := range expectedMetrics {
		assert.Equal(t, metricWasSeen[metric], true)
	}
}

func loadExpectedMetricGroupsFromGoldenFile(t *testing.T, model string) map[string]int {
	expectedMetricGroups := map[string]int{
		"dcgm.gpu.utilization":                   0,
		"dcgm.gpu.memory.bytes_used":             0,
		"dcgm.gpu.profiling.sm_utilization":      0,
		"dcgm.gpu.profiling.sm_occupancy":        0,
		"dcgm.gpu.profiling.pipe_utilization":    0,
		"dcgm.gpu.profiling.dram_utilization":    0,
		"dcgm.gpu.profiling.pcie_traffic_rate":   0,
		"dcgm.gpu.profiling.nvlink_traffic_rate": 0,
	}
	nameToGroupMap := map[string]string{
		"dcgm.gpu.utilization":                     "dcgm.gpu.utilization",
		"dcgm.gpu.memory.bytes_used":               "dcgm.gpu.memory.bytes_used",
		"dcgm.gpu.memory.bytes_free":               "dcgm.gpu.memory.bytes_used",
		"dcgm.gpu.profiling.sm_utilization":        "dcgm.gpu.profiling.sm_utilization",
		"dcgm.gpu.profiling.sm_occupancy":          "dcgm.gpu.profiling.sm_occupancy",
		"dcgm.gpu.profiling.tensor_utilization":    "dcgm.gpu.profiling.pipe_utilization",
		"dcgm.gpu.profiling.dram_utilization":      "dcgm.gpu.profiling.dram_utilization",
		"dcgm.gpu.profiling.fp64_utilization":      "dcgm.gpu.profiling.pipe_utilization",
		"dcgm.gpu.profiling.fp32_utilization":      "dcgm.gpu.profiling.pipe_utilization",
		"dcgm.gpu.profiling.fp16_utilization":      "dcgm.gpu.profiling.pipe_utilization",
		"dcgm.gpu.profiling.pcie_sent_bytes":       "dcgm.gpu.profiling.pcie_traffic_rate",
		"dcgm.gpu.profiling.pcie_received_bytes":   "dcgm.gpu.profiling.pcie_traffic_rate",
		"dcgm.gpu.profiling.nvlink_sent_bytes":     "dcgm.gpu.profiling.nvlink_traffic_rate",
		"dcgm.gpu.profiling.nvlink_received_bytes": "dcgm.gpu.profiling.nvlink_traffic_rate",
	}
	expectedMetrics := LoadExpectedMetrics(t, model)
	for _, em := range expectedMetrics {
		expectedMetricGroups[nameToGroupMap[em]] += 1
	}
	return expectedMetricGroups
}

func validateScraperResult(t *testing.T, metrics pmetric.Metrics, expectedMetrics map[string]int) {

	metricWasSeen := make(map[string]bool)
	expectedDataPointCount := 0
	for metric, expectedMetricDataPoints := range expectedMetrics {
		metricWasSeen[metric] = false
		expectedDataPointCount += expectedMetricDataPoints
	}

	assert.LessOrEqual(t, len(expectedMetrics), metrics.MetricCount())
	assert.LessOrEqual(t, expectedDataPointCount, metrics.DataPointCount())

	ilms := metrics.ResourceMetrics().At(0).ScopeMetrics()
	require.Equal(t, 1, ilms.Len())

	ms := ilms.At(0).Metrics()
	for i := 0; i < ms.Len(); i++ {
		m := ms.At(i)
		dps := m.Gauge().DataPoints()
		for j := 0; j < dps.Len(); j++ {
			assert.Regexp(t, ".*gpu_number:.*", dps.At(j).Attributes().AsRaw())
			assert.Regexp(t, ".*model:.*", dps.At(j).Attributes().AsRaw())
			assert.Regexp(t, ".*uuid:.*", dps.At(j).Attributes().AsRaw())
		}

		assert.LessOrEqual(t, expectedMetrics[m.Name()], dps.Len())

		switch m.Name() {
		case "dcgm.gpu.utilization":
		case "dcgm.gpu.memory.bytes_used":
			for j := 0; j < dps.Len(); j++ {
				assert.Regexp(t, ".*memory_state:.*", dps.At(j).Attributes().AsRaw())
			}
		case "dcgm.gpu.profiling.sm_utilization":
		case "dcgm.gpu.profiling.sm_occupancy":
		case "dcgm.gpu.profiling.dram_utilization":
		case "dcgm.gpu.profiling.pipe_utilization":
			for j := 0; j < dps.Len(); j++ {
				assert.Regexp(t, ".*pipe:.*", dps.At(j).Attributes().AsRaw())
			}
		case "dcgm.gpu.profiling.pcie_traffic_rate":
			fallthrough
		case "dcgm.gpu.profiling.nvlink_traffic_rate":
			for j := 0; j < dps.Len(); j++ {
				assert.Regexp(t, ".*direction:.*", dps.At(j).Attributes().AsRaw())
			}
		default:
			t.Errorf("Unexpected metric %s", m.Name())
		}

		metricWasSeen[m.Name()] = true
	}

	for metric := range expectedMetrics {
		assert.Equal(t, metricWasSeen[metric], true)
	}
}
