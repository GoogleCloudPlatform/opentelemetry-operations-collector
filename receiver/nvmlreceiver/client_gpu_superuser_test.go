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

//go:build gpu && superuser
// +build gpu,superuser

package nvmlreceiver

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver/testcudakernel"
)

func TestNewNvmlClientWithGpuSupportsAccountingMode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client, _ := newClient(createDefaultConfig().(*Config), logger)
	require.NotNil(t, client)
	assert.Equal(t, client.disable, false)
	assert.Greater(t, len(client.devices), 0)
	assert.Equal(t, client.collectProcessInfo, true)
}

func TestCollectGpuProcessesAccounting(t *testing.T) {
	client, _ := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.NotNil(t, client)
	assert.Equal(t, client.disable, false)
	assert.Greater(t, len(client.devices), 0)
	assert.Equal(t, client.collectProcessInfo, true)

	testcudakernel.SubmitCudaTestKernel()

	before := time.Now()
	metrics := client.collectProcessMetrics()
	after := time.Now()

	seenSelfPid := false
	for _, metric := range metrics {
		assert.GreaterOrEqual(t, metric.time, before)
		assert.LessOrEqual(t, metric.time, after)
		assert.GreaterOrEqual(t, metric.gpuIndex, uint(0))
		assert.LessOrEqual(t, metric.gpuIndex, uint(32))
		assert.GreaterOrEqual(t, metric.processPid, int(0))
		assert.LessOrEqual(t, metric.processPid, int(32768))
		assert.GreaterOrEqual(t, metric.lifetimeGpuUtilization, uint64(0))
		assert.LessOrEqual(t, metric.lifetimeGpuUtilization, uint64(100))
		assert.GreaterOrEqual(t, metric.lifetimeGpuMaxMemory, uint64(0))
		assert.LessOrEqual(t, metric.lifetimeGpuMaxMemory, uint64(1073741824))

		seenSelfPid = seenSelfPid || metric.processPid == os.Getpid()
	}

	assert.Equal(t, seenSelfPid, true)
}
