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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"
	"gotest.tools/v3/golden"
)

const testdataDir = "testdata"

// modelSupportedFields can be used to track supported fields for a given GPU
type modelSupportedFields struct {
	// The model of the GPU device, for example, Tesla P4
	Model string `yaml:"model"`
	// List of supported fields
	SupportedFields []string `yaml:"supported_fields"`
	// List of unsupported fields
	UnsupportedFields []string `yaml:"unsupported_fields"`
}

// TestSupportedFieldsWithGolden test getSupportedProfilingFields() against the
// golden files for the current GPU model
func TestSupportedFieldsWithGolden(t *testing.T) {
	config := createDefaultConfig().(*Config)
	client, err := newClient(config, zaptest.NewLogger(t))
	require.Nil(t, err, "cannot initialize DCGM. Install and run DCGM before running tests.")

	assert.NotEmpty(t, client.devicesModelName)
	gpuModel := client.getDeviceModelName(0)
	allFields := discoverRequestedFieldIDs(config)
	supportedProfilingFields, err := getSupportedProfilingFields()
	require.Nil(t, err)
	enabledFields, unavailableFields := filterSupportedFields(allFields, supportedProfilingFields)

	var enabledFieldsString []string
	var unavailableFieldsString []string
	for _, f := range enabledFields {
		enabledFieldsString = append(enabledFieldsString, dcgmIDToName[f])
	}
	for _, f := range unavailableFields {
		unavailableFieldsString = append(unavailableFieldsString, dcgmIDToName[f])
	}
	m := modelSupportedFields{
		Model:             gpuModel,
		SupportedFields:   enabledFieldsString,
		UnsupportedFields: unavailableFieldsString,
	}
	actual, err := yaml.Marshal(&m)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, len(allFields), len(client.enabledFieldIDs)+len(unavailableFieldsString))
	goldenPath := getModelGoldenFilePath(t, gpuModel)
	golden.Assert(t, string(actual), goldenPath)
	client.cleanup()
}

// LoadExpectedMetrics read the supported metrics of a GPU model from the golden
// file, given a GPU model string
func LoadExpectedMetrics(t *testing.T, model string) []string {
	t.Helper()
	goldenPath := getModelGoldenFilePath(t, model)
	goldenFile, err := ioutil.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var m modelSupportedFields
	err = yaml.Unmarshal(goldenFile, &m)
	if err != nil {
		t.Fatal(err)
	}
	var expectedMetrics []string
	for _, supported := range m.SupportedFields {
		expectedMetrics = append(expectedMetrics, supported)
	}
	return expectedMetrics
}

// getModelGoldenFilePath returns golden file path given a GPU model string
func getModelGoldenFilePath(t *testing.T, model string) string {
	t.Helper()
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return path.Join(testDir, testdataDir, fmt.Sprintf("%s.yaml", strings.ReplaceAll(model, " ", "_")))
}

func TestNewDcgmClientWithGpuPresent(t *testing.T) {
	client, err := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.Nil(t, err, "cannot initialize DCGM. Install and run DCGM before running tests.")

	assert.NotNil(t, client)
	assert.NotNil(t, client.handleCleanup)
	assert.Greater(t, len(client.deviceIndices), 0)
	for gpuIndex := range client.deviceIndices {
		assert.Greater(t, len(client.devicesModelName[gpuIndex]), 0)
		assert.Greater(t, len(client.devicesUUID[gpuIndex]), 0)
	}
	client.cleanup()
}

func TestCollectGpuProfilingMetrics(t *testing.T) {
	client, err := newClient(createDefaultConfig().(*Config), zaptest.NewLogger(t))
	require.Nil(t, err, "cannot initialize DCGM. Install and run DCGM before running tests.")
	expectedMetrics := LoadExpectedMetrics(t, client.devicesModelName[0])
	var maxCollectionInterval = 60 * time.Second
	before := time.Now().UnixMicro() - maxCollectionInterval.Microseconds()
	deviceMetrics, err := client.collectDeviceMetrics()
	after := time.Now().UnixMicro()
	assert.Nil(t, err)

	seenMetric := make(map[string]bool)
	assert.GreaterOrEqual(t, len(deviceMetrics), 0)
	assert.LessOrEqual(t, len(deviceMetrics), 32)
	for gpuIndex, metrics := range deviceMetrics {
		for _, metric := range metrics {
			switch metric.name {
			case "DCGM_FI_PROF_PIPE_TENSOR_ACTIVE":
				fallthrough
			case "DCGM_FI_PROF_DRAM_ACTIVE":
				fallthrough
			case "DCGM_FI_PROF_PIPE_FP64_ACTIVE":
				fallthrough
			case "DCGM_FI_PROF_PIPE_FP32_ACTIVE":
				fallthrough
			case "DCGM_FI_PROF_PIPE_FP16_ACTIVE":
				fallthrough
			case "DCGM_FI_PROF_SM_OCCUPANCY":
				fallthrough
			case "DCGM_FI_PROF_SM_ACTIVE":
				assert.GreaterOrEqual(t, metric.asFloat64(), float64(0.0))
				assert.LessOrEqual(t, metric.asFloat64(), float64(1.0))
			case "DCGM_FI_DEV_GPU_UTIL":
				assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
				assert.LessOrEqual(t, metric.asInt64(), int64(100))
			case "DCGM_FI_DEV_FB_FREE":
				fallthrough
			case "DCGM_FI_DEV_FB_USED":
				// arbitrary max of 10 TiB
				assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
				assert.LessOrEqual(t, metric.asInt64(), int64(10485760))
			case "DCGM_FI_PROF_PCIE_TX_BYTES":
				fallthrough
			case "DCGM_FI_PROF_PCIE_RX_BYTES":
				fallthrough
			case "DCGM_FI_PROF_NVLINK_TX_BYTES":
				fallthrough
			case "DCGM_FI_PROF_NVLINK_RX_BYTES":
				// arbitrary max of 10 TiB/sec
				assert.GreaterOrEqual(t, metric.asInt64(), int64(0))
				assert.LessOrEqual(t, metric.asInt64(), int64(10995116277760))
			default:
				t.Errorf("Unexpected metric '%s'", metric.name)
			}

			assert.GreaterOrEqual(t, metric.timestamp, before)
			assert.LessOrEqual(t, metric.timestamp, after)

			seenMetric[fmt.Sprintf("gpu{%d}.metric{%s}", gpuIndex, metric.name)] = true
		}
	}

	for _, gpuIndex := range client.deviceIndices {
		for _, metric := range expectedMetrics {
			assert.True(t, seenMetric[fmt.Sprintf("gpu{%d}.metric{%s}", gpuIndex, metric)], fmt.Sprintf("%s on gpu %d", metric, gpuIndex))
		}
	}
	client.cleanup()
}
