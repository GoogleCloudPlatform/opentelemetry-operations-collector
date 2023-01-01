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

// Note: DCGM daemon needs to be running for all GPU tests

//go:build gpu && !windows
// +build gpu,!windows

package dcgmreceiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewDcgmClientWithGpuPresent(t *testing.T) {
	client, err := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.Nil(t, err)

	assert.NotNil(t, client)
	assert.NotNil(t, client.handleCleanup)
	assert.Greater(t, len(client.deviceIndices), 0)
	for gpuIndex, _ := range client.deviceIndices {
		assert.Greater(t, len(client.devicesModelName[gpuIndex]), 0)
		assert.Greater(t, len(client.devicesUUID[gpuIndex]), 0)
	}
	assert.Equal(t, len(client.enabledfieldIDs), len(dcgmNameToMetricName))
}

func TestCollectGpuProfilingMetrics(t *testing.T) {
	client, err := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.Nil(t, err)

	expectedMetrics := []string{
		"dcgm.gpu.utilization",
		"dcgm.gpu.memory.bytes_used",
		"dcgm.gpu.memory.bytes_free",
		"dcgm.gpu.profiling.sm_utilization",
		"dcgm.gpu.profiling.sm_occupancy",
		"dcgm.gpu.profiling.tensor_utilization",
		"dcgm.gpu.profiling.dram_utilization",
		"dcgm.gpu.profiling.fp64_utilization",
		"dcgm.gpu.profiling.fp32_utilization",
		"dcgm.gpu.profiling.fp16_utilization",
		"dcgm.gpu.profiling.pcie_sent_bytes",
		"dcgm.gpu.profiling.pcie_received_bytes",
		"dcgm.gpu.profiling.nvlink_sent_bytes",
		"dcgm.gpu.profiling.nvlink_received_bytes",
	}

	var maxCollectionInterval = 60 * time.Second
	before := time.Now().UnixMicro() - maxCollectionInterval.Microseconds()
	metrics, err := client.collectDeviceMetrics()
	after := time.Now().UnixMicro()
	assert.Nil(t, err)

	seenMetric := make(map[string]bool)
	for _, metric := range metrics {
		assert.GreaterOrEqual(t, metric.gpuIndex, uint(0))
		assert.LessOrEqual(t, metric.gpuIndex, uint(32))

		switch metric.name {
		case "dcgm.gpu.profiling.sm_utilization":
			fallthrough
		case "dcgm.gpu.profiling.sm_occupancy":
			fallthrough
		case "dcgm.gpu.profiling.tensor_utilization":
			fallthrough
		case "dcgm.gpu.profiling.dram_utilization":
			fallthrough
		case "dcgm.gpu.profiling.fp64_utilization":
			fallthrough
		case "dcgm.gpu.profiling.fp32_utilization":
			fallthrough
		case "dcgm.gpu.profiling.fp16_utilization":
			fallthrough
		case "dcgm.gpu.utilization":
			assert.GreaterOrEqual(t, metric.asFloat64(), float64(0.0))
			assert.LessOrEqual(t, metric.asFloat64(), float64(1.0))
		case "dcgm.gpu.memory.bytes_free":
			fallthrough
		case "dcgm.gpu.memory.bytes_used":
			// arbitrary max of 10 TiB
			assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
			assert.LessOrEqual(t, metric.asInt64(), int64(10485760))
		case "dcgm.gpu.profiling.pcie_sent_bytes":
			fallthrough
		case "dcgm.gpu.profiling.pcie_received_bytes":
			fallthrough
		case "dcgm.gpu.profiling.nvlink_sent_bytes":
			fallthrough
		case "dcgm.gpu.profiling.nvlink_received_bytes":
			// arbitrary max of 10 TiB/sec
			assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
			assert.LessOrEqual(t, metric.asInt64(), int64(10995116277760))
		default:
			t.Errorf("Unexpected metric '%s'", metric.name)
		}

		assert.GreaterOrEqual(t, metric.timestamp, before)
		assert.LessOrEqual(t, metric.timestamp, after)

		seenMetric[metric.name] = true
	}

	for _, metric := range expectedMetrics {
		assert.Equal(t, seenMetric[metric], true)
	}
}
