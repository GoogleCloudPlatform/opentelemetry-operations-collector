// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gcpdestination

import (
	"context"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/processor/queuebatchprocessor"
)

func TestQueueBatchSizers(t *testing.T) {
	cfg := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
	cfg.Sizer = exporterhelper.RequestSizerTypeBytes
	batch := cfg.Batch.GetOrInsertDefault()
	batch.Sizer = exporterhelper.RequestSizerTypeItems

	got, err := queueBatchSizers(cfg)
	if err != nil {
		t.Fatalf("queueBatchSizers() err = %v", err)
	}
	if got["sizer"] != "bytes" {
		t.Errorf("queue sizer = %v, want bytes", got["sizer"])
	}
	batchSizers, ok := got["batch"].(map[string]any)
	if !ok || batchSizers["sizer"] != "items" {
		t.Errorf("batch sizers = %v, want sizer items", got["batch"])
	}
}

func TestGCPDestinationPolicy_Evaluate_QueueBatchConfig(t *testing.T) {
	policy := &GCPDestinationPolicy{
		Name:      "default_gcp_destination",
		ProjectID: "test-project",
	}

	conf, err := policy.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	// Log queue_batch: persistent queue on disk matching Ops Agent otlpExporterForLogs
	logKey := "processors::queue_batch/default_gcp_destination_batch_logs"
	require.True(t, conf.IsSet(logKey))
	logSub, err := conf.Sub(logKey)
	require.NoError(t, err)
	logMap := logSub.ToStringMap()

	assert.Equal(t, googlepolicy.StorageExtensionID.String(), logMap["storage"])
	assert.Equal(t, "bytes", logMap["sizer"])
	assert.Equal(t, int64(20000000), logMap["queue_size"])
	assert.Equal(t, 10, logMap["num_consumers"])
	assert.Equal(t, true, logMap["block_on_overflow"])

	batchMap, ok := logMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "bytes", batchMap["sizer"])
	assert.Equal(t, int64(1000000), batchMap["min_size"])
	assert.Equal(t, int64(5000000), batchMap["max_size"])
	assert.Equal(t, 200*time.Millisecond, batchMap["flush_timeout"])

	// Metric queue_batch: in-memory only (no storage)
	metricKey := "processors::queue_batch/default_gcp_destination_batch_metrics"
	require.True(t, conf.IsSet(metricKey))
	metricSub, err := conf.Sub(metricKey)
	require.NoError(t, err)
	metricMap := metricSub.ToStringMap()
	assert.Nil(t, metricMap["storage"])
	assert.Equal(t, "requests", metricMap["sizer"])

	// Trace queue_batch: in-memory only (no storage)
	traceKey := "processors::queue_batch/default_gcp_destination_batch_traces"
	require.True(t, conf.IsSet(traceKey))
	traceSub, err := conf.Sub(traceKey)
	require.NoError(t, err)
	traceMap := traceSub.ToStringMap()
	assert.Nil(t, traceMap["storage"])
	assert.Equal(t, "requests", traceMap["sizer"])
}
