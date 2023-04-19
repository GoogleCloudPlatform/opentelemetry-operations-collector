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

//go:build gpu && has_gpu
// +build gpu,has_gpu

package dcgmreceiver

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"
	"gotest.tools/v3/golden"
)

const testdataDir = "testdata"

// ModelSupportedFields can be used to track supported fields for a given GPU
type ModelSupportedFields struct {
	// The model of the GPU device, for example, Tesla P4
	Model string `yaml:"model"`
	// List of supported fields
	SupportedFields []string `yaml:"supported_fields"`
	// List of unsupported fields
	UnsupportedFields []string `yaml:"unsupported_fields"`
}

// TestSupportedFieldsWithGolden test getAllSupportedFields() against the golden
// files for the current GPU model
func TestSupportedFieldsWithGolden(t *testing.T) {
	config := createDefaultConfig().(*Config)
	client, err := newClient(config, zaptest.NewLogger(t))
	require.Nil(t, err)

	assert.NotEmpty(t, client.devicesModelName)
	gpuModel := client.getDeviceModelName(0)
	enabled := discoverEnabledFieldIDs(config)
	fields, err := getAllSupportedFields()
	require.Nil(t, err)
	onFields, offFields := filterSupportedFields(enabled, fields)

	dcgmIDToNameMap := make(map[dcgm.Short]string, len(dcgm.DCGM_FI))
	for fieldName, fieldID := range dcgm.DCGM_FI {
		dcgmIDToNameMap[fieldID] = fieldName
	}
	var onFieldsString []string
	var offFieldsString []string
	for _, f := range onFields {
		onFieldsString = append(onFieldsString, dcgmIDToNameMap[f])
	}
	for _, f := range offFields {
		offFieldsString = append(offFieldsString, dcgmIDToNameMap[f])
	}
	m := ModelSupportedFields{
		Model:             gpuModel,
		SupportedFields:   onFieldsString,
		UnsupportedFields: offFieldsString,
	}
	actual, err := yaml.Marshal(&m)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(dcgmNameToMetricName), len(client.enabledFieldIDs)+len(offFields))
	goldenPath := getModelGoldenFilePath(t, gpuModel)
	golden.Assert(t, string(actual), goldenPath)
}

// LoadExpectedMetrics read the supported metrics of a GPU model from the golden
// file, given a GPU model string
func LoadExpectedMetrics(t *testing.T, model string) []string {
	dcgmNameToMetricNameMap := map[string]string{
		"DCGM_FI_DEV_GPU_UTIL":            "dcgm.gpu.utilization",
		"DCGM_FI_DEV_FB_USED":             "dcgm.gpu.memory.bytes_used",
		"DCGM_FI_DEV_FB_FREE":             "dcgm.gpu.memory.bytes_free",
		"DCGM_FI_PROF_SM_ACTIVE":          "dcgm.gpu.profiling.sm_utilization",
		"DCGM_FI_PROF_SM_OCCUPANCY":       "dcgm.gpu.profiling.sm_occupancy",
		"DCGM_FI_PROF_PIPE_TENSOR_ACTIVE": "dcgm.gpu.profiling.tensor_utilization",
		"DCGM_FI_PROF_DRAM_ACTIVE":        "dcgm.gpu.profiling.dram_utilization",
		"DCGM_FI_PROF_PIPE_FP64_ACTIVE":   "dcgm.gpu.profiling.fp64_utilization",
		"DCGM_FI_PROF_PIPE_FP32_ACTIVE":   "dcgm.gpu.profiling.fp32_utilization",
		"DCGM_FI_PROF_PIPE_FP16_ACTIVE":   "dcgm.gpu.profiling.fp16_utilization",
		"DCGM_FI_PROF_PCIE_TX_BYTES":      "dcgm.gpu.profiling.pcie_sent_bytes",
		"DCGM_FI_PROF_PCIE_RX_BYTES":      "dcgm.gpu.profiling.pcie_received_bytes",
		"DCGM_FI_PROF_NVLINK_TX_BYTES":    "dcgm.gpu.profiling.nvlink_sent_bytes",
		"DCGM_FI_PROF_NVLINK_RX_BYTES":    "dcgm.gpu.profiling.nvlink_received_bytes",
	}
	goldenPath := getModelGoldenFilePath(t, model)
	goldenFile, err := ioutil.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var m ModelSupportedFields
	err = yaml.Unmarshal(goldenFile, &m)
	if err != nil {
		t.Fatal(err)
	}
	var expectedMetrics []string
	for _, supported := range m.SupportedFields {
		expectedMetrics = append(expectedMetrics, dcgmNameToMetricNameMap[supported])
	}
	return expectedMetrics
}

// getModelGoldenFilePath returns golden file path given a GPU model string
func getModelGoldenFilePath(t *testing.T, model string) string {
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return path.Join(testDir, testdataDir, fmt.Sprintf("%s.yaml", strings.ReplaceAll(model, " ", "_")))
}

func TestNewDcgmClientWithGpuPresent(t *testing.T) {
	client, err := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.Nil(t, err)

	assert.NotNil(t, client)
	assert.NotNil(t, client.handleCleanup)
	assert.Greater(t, len(client.deviceIndices), 0)
	for gpuIndex := range client.deviceIndices {
		assert.Greater(t, len(client.devicesModelName[gpuIndex]), 0)
		assert.Greater(t, len(client.devicesUUID[gpuIndex]), 0)
	}
}

func TestCollectGpuProfilingMetrics(t *testing.T) {
	client, err := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.Nil(t, err)
	expectedMetrics := LoadExpectedMetrics(t, client.devicesModelName[0])
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
		case "dcgm.gpu.profiling.sm_occupancy":
			fallthrough
		case "dcgm.gpu.profiling.sm_utilization":
			assert.GreaterOrEqual(t, metric.asFloat64(), float64(0.0))
			assert.LessOrEqual(t, metric.asFloat64(), float64(1.0))
		case "dcgm.gpu.utilization":
			assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
			assert.LessOrEqual(t, metric.asInt64(), int64(100))
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

		seenMetric[fmt.Sprintf("gpu{%d}.metric{%s}", metric.gpuIndex, metric.name)] = true
	}

	for _, gpuIndex := range client.deviceIndices {
		for _, metric := range expectedMetrics {
			assert.Equal(t, seenMetric[fmt.Sprintf("gpu{%d}.metric{%s}", gpuIndex, metric)], true)
		}
	}
}
