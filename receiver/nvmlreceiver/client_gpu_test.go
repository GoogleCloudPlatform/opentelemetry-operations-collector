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

//go:build gpu
// +build gpu

package nvmlreceiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewNvmlClientWithGpuPresent(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)
	assert.Equal(t, client.disable, false)
	assert.Greater(t, len(client.devices), 0)
}

func TestGpuUtilizationWithGpuPresent(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)

	before := time.Now()
	metrics := client.collectDeviceUtilization()
	after := time.Now()

	assert.GreaterOrEqual(t, len(metrics), 1)
	for _, metric := range metrics {
		assert.Equal(t, metric.name, "nvml.gpu.utilization")
		assert.GreaterOrEqual(t, metric.gpuId, uint(0))
		assert.LessOrEqual(t, metric.gpuId, uint(32))
		assert.GreaterOrEqual(t, metric.asFloat64(), 0.0)
		assert.LessOrEqual(t, metric.asFloat64(), 1.0)
		assert.GreaterOrEqual(t, metric.time, before)
		assert.LessOrEqual(t, metric.time, after)
	}
}

func TestGpuMemoryUsedWithGpuPresent(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)

	var requiredNames = map[string]bool{
		"nvml.gpu.memory.bytes_used": false,
		"nvml.gpu.memory.bytes_free": false,
	}

	before := time.Now()
	metrics := client.collectDeviceMemoryInfo()
	after := time.Now()

	assert.GreaterOrEqual(t, len(metrics), 2)
	for _, metric := range metrics {
		assert.Contains(t, requiredNames, metric.name)
		requiredNames[metric.name] = true
		assert.GreaterOrEqual(t, metric.gpuId, uint(0))
		assert.LessOrEqual(t, metric.gpuId, uint(32))
		assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
		assert.LessOrEqual(t, metric.asInt64(), int64(10995116277760)) // 10 TiB
		assert.GreaterOrEqual(t, metric.time, before)
		assert.LessOrEqual(t, metric.time, after)
	}

	for _, seen := range requiredNames {
		assert.Equal(t, seen, true)
	}
}

// todo: check no fail on bad NVML query
// todo: check max warnings on bad NVML query
// todo: check average utilization is correct
// todo: check model name is meaningful
