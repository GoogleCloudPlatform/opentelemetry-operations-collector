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

//go:build with_gpu
// +build with_gpu

package nvmlreceiver

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewNvmlClientWithGpuPresent(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)
	require.Equal(t, client.disable, false)
	require.Greater(t, len(client.devices), 0)
}

func TestGpuUtilizationWithGpuPresent(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)

	metrics := client.collectDeviceUtilization()
	require.GreaterOrEqual(t, len(metrics), 1)
	for _, metric := range metrics {
		require.Equal(t, metric.name, "nvml.gpu.utilization")
		require.GreaterOrEqual(t, metric.asFloat64(), 0.0)
		require.LessOrEqual(t, metric.asFloat64(), 1.0)
	}
}

func TestGpuMemoryUsedWithGpuPresent(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)

	metrics := client.collectDeviceMemoryInfo()
	require.GreaterOrEqual(t, len(metrics), 2)
	for _, metric := range metrics {
		nameMatch :=
			metric.name == "nvml.gpu.memory.bytes_used" ||
				metric.name == "nvml.gpu.memory.bytes_free"
		require.Equal(t, nameMatch, true)
		require.GreaterOrEqual(t, metric.asInt64(), int64(0))
		require.LessOrEqual(t, metric.asInt64(), int64(10995116277760))
	}
}
