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
	"fmt"
	"testing"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap/zaptest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/testprofilepause"
)

func TestScrapeWithGpuPresent(t *testing.T) {
	settings := componenttest.NewNopReceiverCreateSettings()
	settings.Logger = zaptest.NewLogger(t)

	scraper, err := newDcgmScraper(createDefaultConfig().(*Config), settings)
	require.NotNil(t, scraper)
	require.NoError(t, err)

	err = scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())
	validateScraperResult(t, metrics)
}

func TestScrapeOnPollingError(t *testing.T) {
	realDcgmGetLatestValuesForFields := dcgmGetLatestValuesForFields
	defer func() { dcgmGetLatestValuesForFields = realDcgmGetLatestValuesForFields }()
	dcgmGetLatestValuesForFields = func(gpu uint, fields []dcgm.Short) ([]dcgm.FieldValue_v1, error) {
		return nil, fmt.Errorf("DCGM polling error")
	}

	settings := componenttest.NewNopReceiverCreateSettings()
	settings.Logger = zaptest.NewLogger(t)

	scraper, err := newDcgmScraper(createDefaultConfig().(*Config), settings)
	require.NotNil(t, scraper)
	require.NoError(t, err)

	err = scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())

	assert.Error(t, err)
	assert.Equal(t, metrics.MetricCount(), 0)
}

func TestScrapeOnProfilingPaused(t *testing.T) {
	config := createDefaultConfig().(*Config)
	config.CollectionInterval = 10 * time.Millisecond

	settings := componenttest.NewNopReceiverCreateSettings()
	settings.Logger = zaptest.NewLogger(t)

	scraper, err := newDcgmScraper(config, settings)
	require.NotNil(t, scraper)
	require.NoError(t, err)

	defer func() { testprofilepause.ResumeProfilingMetrics() }()
	testprofilepause.PauseProfilingMetrics()
	time.Sleep(20 * time.Millisecond)

	err = scraper.start(context.Background(), componenttest.NewNopHost())
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

func validateScraperResult(t *testing.T, metrics pmetric.Metrics) {
	expectedMetrics := map[string]int{
		"dcgm.gpu.utilization":                   1,
		"dcgm.gpu.memory.bytes_used":             2,
		"dcgm.gpu.profiling.sm_utilization":      1,
		"dcgm.gpu.profiling.sm_occupancy":        1,
		"dcgm.gpu.profiling.pipe_utilization":    4,
		"dcgm.gpu.profiling.dram_utilization":    1,
		"dcgm.gpu.profiling.pcie_traffic_rate":   2,
		"dcgm.gpu.profiling.nvlink_traffic_rate": 2,
	}

	metricWasSeen := make(map[string]bool)
	expectedDataPointCount := 0
	for metric, expectedMetricDataPoints := range expectedMetrics {
		metricWasSeen[metric] = false
		expectedDataPointCount += expectedMetricDataPoints
	}

	assert.Equal(t, metrics.MetricCount(), len(expectedMetrics))
	assert.Equal(t, metrics.DataPointCount(), expectedDataPointCount)

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

		assert.Equal(t, expectedMetrics[m.Name()], dps.Len())

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
