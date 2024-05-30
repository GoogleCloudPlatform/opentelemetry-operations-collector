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
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/scraperhelper"
	"go.uber.org/zap/zaptest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/internal/metadata"
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
	expectedMetrics := loadExpectedScraperMetrics(t, scraper.client.getDeviceModelName(0))
	validateScraperResult(t, metrics, expectedMetrics)
}

func TestScrapeWithDelayedDcgmService(t *testing.T) {
	realDcgmInit := dcgmInit
	defer func() { dcgmInit = realDcgmInit }()
	dcgmInit = func(args ...string) (func(), error) {
		return nil, fmt.Errorf("No DCGM client library *OR* No DCGM connection")
	}

	var settings receiver.CreateSettings
	settings.Logger = zaptest.NewLogger(t)

	scraper := newDcgmScraper(createDefaultConfig().(*Config), settings)
	require.NotNil(t, scraper)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())
	assert.NoError(t, err) // If failed to init DCGM, should have no error
	assert.Equal(t, 0, metrics.MetricCount())

	// Scrape again with DCGM not available
	metrics, err = scraper.scrape(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, metrics.MetricCount())

	// Simulate DCGM becomes available
	dcgmInit = realDcgmInit
	metrics, err = scraper.scrape(context.Background())
	assert.NoError(t, err)
	expectedMetrics := loadExpectedScraperMetrics(t, scraper.client.getDeviceModelName(0))
	validateScraperResult(t, metrics, expectedMetrics)
}

func TestScrapeWithEmptyMetricsConfig(t *testing.T) {
	var settings receiver.CreateSettings
	settings.Logger = zaptest.NewLogger(t)
	emptyConfig := &Config{
		ControllerConfig: scraperhelper.ControllerConfig{
			CollectionInterval: defaultCollectionInterval,
		},
		TCPAddrConfig: confignet.TCPAddrConfig{
			Endpoint: defaultEndpoint,
		},
		Metrics: metadata.MetricsConfig{
			GpuDcgmClockFrequency: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmClockThrottleDurationTime: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmCodecDecoderUtilization: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmCodecEncoderUtilization: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmEccErrors: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmEnergyConsumption: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmMemoryBandwidthUtilization: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmMemoryBytesUsed: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmNvlinkTraffic: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmPcieTraffic: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmPipeUtilization: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmSmOccupancy: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmSmUtilization: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmTemperature: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmUtilization: metadata.MetricConfig{
				Enabled: false,
			},
			GpuDcgmXidErrors: metadata.MetricConfig{
				Enabled: false,
			},
		},
	}

	scraper := newDcgmScraper(emptyConfig, settings)
	require.NotNil(t, scraper)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	metrics, err := scraper.scrape(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 0, metrics.MetricCount())
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
	assert.Equal(t, 0, metrics.MetricCount())
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

	expectedMetrics := []string{
		//TODO "gpu.dcgm.utilization",
		"gpu.dcgm.codec.decoder.utilization",
		"gpu.dcgm.codec.encoder.utilization",
		"gpu.dcgm.memory.bytes_used",
		//TODO "gpu.dcgm.memory.bandwidth_utilization",
		//TODO "gpu.dcgm.energy_consumption",
		"gpu.dcgm.temperature",
		"gpu.dcgm.clock.frequency",
		"gpu.dcgm.clock.throttle_duration.time",
		"gpu.dcgm.ecc_errors",
	}

	ilms := metrics.ResourceMetrics().At(0).ScopeMetrics()
	require.Equal(t, 1, ilms.Len())

	ms := ilms.At(0).Metrics()
	require.LessOrEqual(t, len(expectedMetrics), ms.Len())

	metricWasSeen := make(map[string]bool)
	for i := 0; i < ms.Len(); i++ {
		metricWasSeen[ms.At(i).Name()] = true
	}

	for _, metric := range expectedMetrics {
		assert.True(t, metricWasSeen[metric], metric)
	}
}

// loadExpectedScraperMetrics calls LoadExpectedMetrics to read the supported
// metrics from the golden file given a GPU model, and then convert the name
// from how they are defined in the dcgm client to scraper naming
func loadExpectedScraperMetrics(t *testing.T, model string) map[string]int {
	t.Helper()
	expectedMetrics := make(map[string]int)
	receiverMetricNameToScraperMetricName := map[string]string{
		"DCGM_FI_PROF_GR_ENGINE_ACTIVE": "gpu.dcgm.utilization",
		//"DCGM_FI_DEV_GPU_UTIL":          "gpu.dcgm.utilization",
		"DCGM_FI_PROF_SM_ACTIVE":          "gpu.dcgm.sm.utilization",
		"DCGM_FI_PROF_SM_OCCUPANCY":       "gpu.dcgm.sm.occupancy",
		"DCGM_FI_PROF_PIPE_TENSOR_ACTIVE": "gpu.dcgm.pipe.utilization",
		"DCGM_FI_PROF_PIPE_FP64_ACTIVE":   "gpu.dcgm.pipe.utilization",
		"DCGM_FI_PROF_PIPE_FP32_ACTIVE":   "gpu.dcgm.pipe.utilization",
		"DCGM_FI_PROF_PIPE_FP16_ACTIVE":   "gpu.dcgm.pipe.utilization",
		"DCGM_FI_DEV_ENC_UTIL":            "gpu.dcgm.codec.encoder.utilization",
		"DCGM_FI_DEV_DEC_UTIL":            "gpu.dcgm.codec.decoder.utilization",
		"DCGM_FI_DEV_FB_FREE":             "gpu.dcgm.memory.bytes_used",
		"DCGM_FI_DEV_FB_USED":             "gpu.dcgm.memory.bytes_used",
		"DCGM_FI_DEV_FB_RESERVED":         "gpu.dcgm.memory.bytes_used",
		"DCGM_FI_PROF_DRAM_ACTIVE":        "gpu.dcgm.memory.bandwidth_utilization",
		//"DCGM_FI_DEV_MEM_COPY_UTIL":               "gpu.dcgm.memory.bandwidth_utilization",
		"DCGM_FI_PROF_PCIE_TX_BYTES": "gpu.dcgm.pcie.traffic",
		"DCGM_FI_PROF_PCIE_RX_BYTES": "gpu.dcgm.pcie.traffic",
		"DCGM_FI_PROF_NVLINK_TX_BYTES":         "gpu.dcgm.nvlink.traffic",
		"DCGM_FI_PROF_NVLINK_RX_BYTES":         "gpu.dcgm.nvlink.traffic",
		"DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION": "gpu.dcgm.energy_consumption",
		//"DCGM_FI_DEV_POWER_USAGE":                 "gpu.dcgm.energy_consumption",
		"DCGM_FI_DEV_GPU_TEMP":                    "gpu.dcgm.temperature",
		"DCGM_FI_DEV_SM_CLOCK":                    "gpu.dcgm.clock.frequency",
		"DCGM_FI_DEV_POWER_VIOLATION":             "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_THERMAL_VIOLATION":           "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_SYNC_BOOST_VIOLATION":        "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_BOARD_LIMIT_VIOLATION":       "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_LOW_UTIL_VIOLATION":          "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_RELIABILITY_VIOLATION":       "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_TOTAL_APP_CLOCKS_VIOLATION":  "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_TOTAL_BASE_CLOCKS_VIOLATION": "gpu.dcgm.clock.throttle_duration.time",
		"DCGM_FI_DEV_ECC_SBE_VOL_TOTAL":           "gpu.dcgm.ecc_errors",
		"DCGM_FI_DEV_ECC_DBE_VOL_TOTAL":           "gpu.dcgm.ecc_errors",
	}
	expectedReceiverMetrics := LoadExpectedMetrics(t, model)
	for _, em := range expectedReceiverMetrics {
		scraperMetric := receiverMetricNameToScraperMetricName[em]
		if scraperMetric != "" {
			expectedMetrics[scraperMetric] += 1
		}
		// TODO: fallbacks.
	}
	return expectedMetrics
}

func validateScraperResult(t *testing.T, metrics pmetric.Metrics, expectedMetrics map[string]int) {
	t.Helper()
	metricWasSeen := make(map[string]bool)
	expectedDataPointCount := 0
	for metric, expectedMetricDataPoints := range expectedMetrics {
		metricWasSeen[metric] = false
		expectedDataPointCount += expectedMetricDataPoints
	}

	assert.LessOrEqual(t, len(expectedMetrics), metrics.MetricCount())
	assert.LessOrEqual(t, expectedDataPointCount, metrics.DataPointCount())

	r := metrics.ResourceMetrics().At(0).Resource()
	assert.Contains(t, r.Attributes().AsRaw(), "gpu.number")
	assert.Contains(t, r.Attributes().AsRaw(), "gpu.uuid")
	assert.Contains(t, r.Attributes().AsRaw(), "gpu.model")

	ilms := metrics.ResourceMetrics().At(0).ScopeMetrics()
	require.Equal(t, 1, ilms.Len())

	ms := ilms.At(0).Metrics()
	for i := 0; i < ms.Len(); i++ {
		m := ms.At(i)
		var dps pmetric.NumberDataPointSlice

		switch m.Name() {
		case "gpu.dcgm.utilization":
			fallthrough
		case "gpu.dcgm.sm.utilization":
			fallthrough
		case "gpu.dcgm.sm.occupancy":
			fallthrough
		case "gpu.dcgm.pipe.utilization":
			fallthrough
		case "gpu.dcgm.codec.encoder.utilization":
			fallthrough
		case "gpu.dcgm.codec.decoder.utilization":
			fallthrough
		case "gpu.dcgm.memory.bytes_used":
			fallthrough
		case "gpu.dcgm.memory.bandwidth_utilization":
			fallthrough
		case "gpu.dcgm.temperature":
			fallthrough
		case "gpu.dcgm.clock.frequency":
			dps = m.Gauge().DataPoints()
		case "gpu.dcgm.energy_consumption":
			fallthrough
		case "gpu.dcgm.clock.throttle_duration.time":
			fallthrough
		case "gpu.dcgm.pcie.traffic":
			fallthrough
		case "gpu.dcgm.nvlink.traffic":
			fallthrough
		case "gpu.dcgm.ecc_errors":
			fallthrough
		case "gpu.dcgm.xid_errors":
			dps = m.Sum().DataPoints()
		default:
			t.Errorf("Unexpected metric %s", m.Name())
		}
		assert.LessOrEqual(t, expectedMetrics[m.Name()], dps.Len())

		switch m.Name() {
		case "gpu.dcgm.utilization":
		case "gpu.dcgm.sm.utilization":
		case "gpu.dcgm.sm.occupancy":
		case "gpu.dcgm.pipe.utilization":
			for j := 0; j < dps.Len(); j++ {
				assert.Contains(t, dps.At(j).Attributes().AsRaw(), "pipe")
			}
		case "gpu.dcgm.codec.encoder.utilization":
		case "gpu.dcgm.codec.decoder.utilization":
		case "gpu.dcgm.memory.bytes_used":
			for j := 0; j < dps.Len(); j++ {
				assert.Contains(t, dps.At(j).Attributes().AsRaw(), "memory_state")
			}
		case "gpu.dcgm.memory.bandwidth_utilization":
		case "gpu.dcgm.pcie.traffic":
			fallthrough
		case "gpu.dcgm.nvlink.traffic":
			for j := 0; j < dps.Len(); j++ {
				assert.Contains(t, dps.At(j).Attributes().AsRaw(), "direction")
			}
		case "gpu.dcgm.energy_consumption":
		case "gpu.dcgm.temperature":
		case "gpu.dcgm.clock.frequency":
		case "gpu.dcgm.clock.throttle_duration.time":
			for j := 0; j < dps.Len(); j++ {
				assert.Contains(t, dps.At(j).Attributes().AsRaw(), "violation")
			}
		case "gpu.dcgm.ecc_errors":
			for j := 0; j < dps.Len(); j++ {
				assert.Contains(t, dps.At(j).Attributes().AsRaw(), "error_type")
			}
		// TODO
		//case "gpu.dcgm.xid_errors":
		//	for j := 0; j < dps.Len(); j++ {
		//		assert.Contains(t, dps.At(j).Attributes().AsRaw(), "xid")
		//	}
		default:
			t.Errorf("Unexpected metric %s", m.Name())
		}

		metricWasSeen[m.Name()] = true
	}

	for metric := range expectedMetrics {
		assert.True(t, metricWasSeen[metric], metric)
	}
}
