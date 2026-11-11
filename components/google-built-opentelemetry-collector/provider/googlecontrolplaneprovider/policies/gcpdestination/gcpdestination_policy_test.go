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

	// Stage 1 log queue_batch: persistent queue on disk with byte sizing
	logKey := "processors::queue_batch/default_gcp_destination_batch_logs"
	require.True(t, conf.IsSet(logKey))
	logSub, err := conf.Sub(logKey)
	require.NoError(t, err)
	logMap := logSub.ToStringMap()

	assert.Equal(t, googlepolicy.StorageExtensionID.String(), logMap["storage"])
	assert.Equal(t, "bytes", logMap["sizer"])
	assert.Equal(t, int64(33554432), logMap["queue_size"])
	assert.Equal(t, 10, logMap["num_consumers"])
	assert.Equal(t, true, logMap["block_on_overflow"])

	batchMap, ok := logMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "bytes", batchMap["sizer"])
	assert.Equal(t, int64(3670016), batchMap["min_size"])
	assert.Equal(t, int64(4194304), batchMap["max_size"])
	assert.Equal(t, 200*time.Millisecond, batchMap["flush_timeout"])

	// Stage 2 log queue_batch: in-memory item splitter for UTR dual limits (kMaxLogCountPerBatch = 8,192)
	splitLogKey := "processors::queue_batch/default_gcp_destination_split_logs"
	require.True(t, conf.IsSet(splitLogKey))
	splitLogSub, err := conf.Sub(splitLogKey)
	require.NoError(t, err)
	splitLogMap := splitLogSub.ToStringMap()

	assert.Nil(t, splitLogMap["storage"])
	assert.Equal(t, "requests", splitLogMap["sizer"])
	assert.Equal(t, int64(10), splitLogMap["queue_size"])

	splitLogBatchMap, ok := splitLogMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "items", splitLogBatchMap["sizer"])
	assert.Equal(t, int64(0), splitLogBatchMap["min_size"])
	assert.Equal(t, int64(8192), splitLogBatchMap["max_size"])
	assert.Equal(t, 200*time.Millisecond, splitLogBatchMap["flush_timeout"])

	// PreProcessLogIDs should contain both stages in order
	preLogIDs := policy.PreProcessLogIDs()
	require.Len(t, preLogIDs, 2)
	assert.Equal(t, "queue_batch/default_gcp_destination_batch_logs", preLogIDs[0].String())
	assert.Equal(t, "queue_batch/default_gcp_destination_split_logs", preLogIDs[1].String())

	// Stage 1 metric queue_batch: in-memory bytes batcher
	metricKey := "processors::queue_batch/default_gcp_destination_batch_metrics"
	require.True(t, conf.IsSet(metricKey))
	metricSub, err := conf.Sub(metricKey)
	require.NoError(t, err)
	metricMap := metricSub.ToStringMap()
	assert.Nil(t, metricMap["storage"])
	assert.Equal(t, "bytes", metricMap["sizer"])
	assert.Equal(t, int64(33554432), metricMap["queue_size"])

	metricBatchMap, ok := metricMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "bytes", metricBatchMap["sizer"])
	assert.Equal(t, int64(3670016), metricBatchMap["min_size"])
	assert.Equal(t, int64(4194304), metricBatchMap["max_size"])

	// Stage 2 metric queue_batch: in-memory item splitter (kMaxPointsPerRequest = 200)
	splitMetricKey := "processors::queue_batch/default_gcp_destination_split_metrics"
	require.True(t, conf.IsSet(splitMetricKey))
	splitMetricSub, err := conf.Sub(splitMetricKey)
	require.NoError(t, err)
	splitMetricMap := splitMetricSub.ToStringMap()
	assert.Nil(t, splitMetricMap["storage"])
	assert.Equal(t, "requests", splitMetricMap["sizer"])

	splitMetricBatchMap, ok := splitMetricMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "items", splitMetricBatchMap["sizer"])
	assert.Equal(t, int64(0), splitMetricBatchMap["min_size"])
	assert.Equal(t, int64(200), splitMetricBatchMap["max_size"])

	// PreProcessMetricIDs should contain both stages in order
	preMetricIDs := policy.PreProcessMetricIDs()
	require.Len(t, preMetricIDs, 2)
	assert.Equal(t, "queue_batch/default_gcp_destination_batch_metrics", preMetricIDs[0].String())
	assert.Equal(t, "queue_batch/default_gcp_destination_split_metrics", preMetricIDs[1].String())

	// Stage 1 trace queue_batch: in-memory bytes batcher
	traceKey := "processors::queue_batch/default_gcp_destination_batch_traces"
	require.True(t, conf.IsSet(traceKey))
	traceSub, err := conf.Sub(traceKey)
	require.NoError(t, err)
	traceMap := traceSub.ToStringMap()
	assert.Nil(t, traceMap["storage"])
	assert.Equal(t, "bytes", traceMap["sizer"])
	assert.Equal(t, int64(33554432), traceMap["queue_size"])

	traceBatchMap, ok := traceMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "bytes", traceBatchMap["sizer"])
	assert.Equal(t, int64(3670016), traceBatchMap["min_size"])
	assert.Equal(t, int64(4194304), traceBatchMap["max_size"])

	// Stage 2 trace queue_batch: in-memory item splitter (kMaxSpanCountPerBatch = 8,192)
	splitTraceKey := "processors::queue_batch/default_gcp_destination_split_traces"
	require.True(t, conf.IsSet(splitTraceKey))
	splitTraceSub, err := conf.Sub(splitTraceKey)
	require.NoError(t, err)
	splitTraceMap := splitTraceSub.ToStringMap()
	assert.Nil(t, splitTraceMap["storage"])
	assert.Equal(t, "requests", splitTraceMap["sizer"])

	splitTraceBatchMap, ok := splitTraceMap["batch"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "items", splitTraceBatchMap["sizer"])
	assert.Equal(t, int64(0), splitTraceBatchMap["min_size"])
	assert.Equal(t, int64(8192), splitTraceBatchMap["max_size"])

	// PreProcessTraceIDs should contain both stages in order
	preTraceIDs := policy.PreProcessTraceIDs()
	require.Len(t, preTraceIDs, 2)
	assert.Equal(t, "queue_batch/default_gcp_destination_batch_traces", preTraceIDs[0].String())
	assert.Equal(t, "queue_batch/default_gcp_destination_split_traces", preTraceIDs[1].String())
}
