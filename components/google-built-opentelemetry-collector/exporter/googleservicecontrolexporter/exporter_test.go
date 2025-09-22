// Copyright 2025 Google LLC
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

package googleservicecontrolexporter

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/genproto/googleapis/api/distribution"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter/internal/metadata"
)

const (
	testExporterStartTime = "2017-09-03T11:16:10Z"
	testConsumerID        = "projects/test-customer-id"
	testServiceID         = "test-service-id"
	testServiceConfigID   = "test-service-config-id"
	testLocation          = "us-central-1"

	testProjectIdKey       = "test-project-id-key"
	testServiceKey         = "test-service-key"
	testServiceConfigIdKey = "test-service-config-key"

	gcpLocation = "cloud.googleapis.com/location"
)

var (
	testLogTime                = time.Date(2020, 2, 11, 20, 26, 13, 789, time.UTC)
	testLogTimestamp           = pcommon.NewTimestampFromTime(testLogTime)
	expectedResourceAttributes = map[string]string{}
	testExporterStartTimeTs    = MustConvertTime(testExporterStartTime)
)

func MustConvertTime(t string) *timestamppb.Timestamp {
	s, err := time.Parse(time.RFC3339, t)
	if err != nil {
		panic(err)
	}
	return timestamppb.New(s)
}

func unexportedOptsForScRequest() cmp.Option {
	return cmpopts.IgnoreUnexported(scpb.Operation{},
		scpb.MetricValueSet{},
		scpb.MetricValue{},
		timestamppb.Timestamp{},
		scpb.Distribution_ExplicitBuckets{},
		distribution.Distribution_Exemplar{},
		scpb.Distribution{},
		scpb.LogEntry{},
		scpb.LogEntrySourceLocation{},
		scpb.HttpRequest{},
		structpb.Value{},
		structpb.Struct{})
}

func noError(_ context.Context) error {
	return nil
}

func fakeError(_ context.Context) error {
	return fmt.Errorf("Fake error")
}

func createExporterThroughOTel(t *testing.T, timeout time.Duration, retryEnabled bool) exporter.Metrics {
	t.Helper()
	conf := createDefaultConfig().(*Config)
	conf.ServiceName = testServiceID
	conf.ConsumerProject = testConsumerID
	conf.TimeoutConfig = exporterhelper.TimeoutConfig{Timeout: timeout}
	conf.BackOffConfig = configretry.BackOffConfig{
		Enabled:         retryEnabled,
		InitialInterval: 0 * time.Second,
		MaxElapsedTime:  3 * time.Second,
	}
	// Queueing adds another layer of complexity to the tests.
	// For example: `ConsumeMetrics` function becomes asynchronous
	// (it just adds tasks to the queue, and then worker goroutines pull from that queue at some point in the future).
	// Thus, we disable queueing, and only test it using integration tests.
	conf.QueueConfig = exporterhelper.QueueBatchConfig{
		Enabled: false,
	}

	settings := exportertest.NewNopSettings(metadata.Type)
	e, err := createMetricsExporter(context.Background(), settings, conf)
	if err != nil {
		t.Fatalf("Could not create exporter: %v", err)
	}
	return e
}

func createOperation(mvs []*scpb.MetricValueSet) *scpb.Operation {
	return &scpb.Operation{
		ConsumerId:      testConsumerID,
		OperationName:   "OpenTelemetry Reported Metrics",
		Labels:          map[string]string{},
		MetricValueSets: mvs,
	}
}

func createOperationWithConsumer(consumerID string, mvs []*scpb.MetricValueSet) *scpb.Operation {
	op := createOperation(mvs)
	op.ConsumerId = consumerID
	return op
}

// In most of the tests we create a list consisting of one Operation.
func createSingleOp(mvs []*scpb.MetricValueSet) []*scpb.Operation {
	return []*scpb.Operation{createOperation(mvs)}
}

func createExporterWithSleepingScServer(t *testing.T, timeout time.Duration, retryEnabled bool, errOnSleep error) (exporter.Metrics, *fakeClient) {
	t.Helper()
	aLotOfTime := 1 * time.Minute
	sleeper := func(ctx context.Context) error {
		select {
		case <-ctx.Done(): // context cancelled, return
			return errOnSleep
		case <-time.After(aLotOfTime): // just sleeping while ctx is not done
		}
		return nil
	}

	defaultClientProvider := clientProvider
	scClient := newFakeClient(sleeper)
	clientProvider = func(_ string, _ bool, _ bool, _ *zap.Logger, _ ...grpc.DialOption) (ServiceControlClient, error) {
		return scClient, nil
	}
	defer func() {
		clientProvider = defaultClientProvider
	}()

	return createExporterThroughOTel(t, timeout, retryEnabled), scClient
}

type metricData struct {
	Resource pcommon.Resource
	Metrics  []pmetric.Metric
}

func emptyResource() pcommon.Resource {
	r := pcommon.NewResource()
	return r
}

func sampleResource() pcommon.Resource {
	r := pcommon.NewResource()
	r.Attributes().PutStr(testServiceConfigIdKey, testServiceConfigID)
	r.Attributes().PutStr(testServiceKey, testServiceID)
	r.Attributes().PutStr(testProjectIdKey, testConsumerID)

	return r
}

func metricDataToPmetric(data metricData) pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	rms := metrics.ResourceMetrics()
	rms.EnsureCapacity(1)
	rm := rms.AppendEmpty()
	data.Resource.CopyTo(rm.Resource())

	rm.ScopeMetrics().EnsureCapacity(1)
	sm := rm.ScopeMetrics().AppendEmpty()
	met := sm.Metrics()
	met.EnsureCapacity(len(data.Metrics))
	for i, m := range data.Metrics {
		met.AppendEmpty()
		m.CopyTo(met.At(i))
	}

	return metrics
}

func sampleMetricData(t *testing.T) metricData {
	t.Helper()
	start, err := time.Parse(time.RFC3339, "2019-09-03T11:16:10Z")
	if err != nil {
		t.Fatalf("Cannot set the start time: %v", err)
	}

	m1 := pmetric.NewMetric()
	m1.SetName("testservice.com/utilization")
	m1.SetEmptyGauge()
	m1.Gauge().DataPoints().EnsureCapacity(2)

	p1 := m1.Gauge().DataPoints().AppendEmpty()
	p1.SetTimestamp(pcommon.NewTimestampFromTime(start))
	p1.SetDoubleValue(0.33)
	attrs := p1.Attributes()
	attrs.PutStr("label1", "label1-value1")

	p2 := m1.Gauge().DataPoints().AppendEmpty()
	p2.SetTimestamp(pcommon.NewTimestampFromTime(start))
	p2.SetDoubleValue(0.5)
	attrs = p2.Attributes()
	attrs.PutStr("label1", "label1-value2")

	// Create similar metrics with different names for TestCreateOperations.
	m2 := pmetric.NewMetric()
	m3 := pmetric.NewMetric()
	m1.CopyTo(m2)
	m2.SetName("testservice.com/usage")
	m1.CopyTo(m3)
	m3.SetName("testservice.com/ratio")

	return metricData{
		Metrics:  []pmetric.Metric{m1, m2, m3},
		Resource: emptyResource(),
	}
}

// operationLess is a less function we pass to the cmp.Diff to ensure we compare
// the content and not the order of the operations.
func operationLess(x, y *scpb.Operation) bool {
	if x.Labels[gcpLocation] < y.Labels[gcpLocation] {
		return true
	}
	return len(x.Labels) < len(y.Labels)
}

func metricValueLess(x, y *scpb.MetricValue) bool {
	return x.GetDoubleValue() < y.GetDoubleValue()
}

func TestAddAndBuild(t *testing.T) {
	int64Cumulative := pmetric.NewMetric()
	int64Cumulative.SetName("testservice.com/request_count")
	int64Cumulative.SetEmptySum()
	int64Cumulative.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

	int64Delta := pmetric.NewMetric()
	int64Delta.SetName("testservice.com/request_count_delta")
	int64Delta.SetEmptySum()
	int64Delta.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityDelta)

	doubleCumulative := pmetric.NewMetric()
	doubleCumulative.SetName("testservice.com/float_sum")
	doubleCumulative.SetEmptySum()
	doubleCumulative.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

	int64Gauge := pmetric.NewMetric()
	int64Gauge.SetName("testservice.com/latency")
	int64Gauge.SetEmptyGauge()
	int64Gauge.SetUnit("ms")

	doubleGauge := pmetric.NewMetric()
	doubleGauge.SetName("testservice.com/utilization")
	doubleGauge.SetEmptyGauge()
	doubleGauge.SetUnit("ms")

	distributionCumulative := pmetric.NewMetric()
	distributionCumulative.SetName("testservice.com/latency_distribution")
	distributionCumulative.SetEmptyHistogram()
	distributionCumulative.Histogram().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	distributionCumulative.SetUnit("ms")

	s, err := time.Parse(time.RFC3339, "2019-09-03T11:16:10Z")
	if err != nil {
		t.Fatalf("Cannot set the start time: %v", err)
	}

	start := pcommon.NewTimestampFromTime(s)
	later := pcommon.NewTimestampFromTime(s.Add(time.Second))

	startTs := timestamppb.New(start.AsTime())
	laterTs := timestamppb.New(later.AsTime())
	laterMinusMsTs := timestamppb.New(later.AsTime().Add(-1 * time.Microsecond))

	tests := []struct {
		name    string
		metrics metricData
		want    []*scpb.Operation
	}{
		{
			name: "int64_cumulative",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					int64Cumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(2)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetIntValue(10)
					p1.Attributes().PutStr("label1", "label1-value1")
					p1.Attributes().PutStr("label2", "label2-value1")

					p2 := m.Sum().DataPoints().AppendEmpty()
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetIntValue(13)
					p2.Attributes().PutStr("label1", "label1-value2")
					p2.Attributes().PutStr("label2", "label2-value1")
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/request_count",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
								"label2": "label2-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{10},
						},
						{
							Labels: map[string]string{
								"label1": "label1-value2",
								"label2": "label2-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{13},
						},
					},
				},
			}),
		},
		{
			name: "int64_delta",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					int64Delta.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(2)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetIntValue(10)
					p1.Attributes().PutStr("label1", "label1-value1")
					p1.Attributes().PutStr("label2", "label2-value1")

					p2 := m.Sum().DataPoints().AppendEmpty()
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetIntValue(13)
					p2.Attributes().PutStr("label1", "label1-value2")
					p2.Attributes().PutStr("label2", "label2-value1")
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/request_count_delta",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
								"label2": "label2-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{10},
						},
						{
							Labels: map[string]string{
								"label1": "label1-value2",
								"label2": "label2-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{13},
						},
					},
				},
			}),
		},
		{
			name: "int64_delta_start_time_eq_end_time",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					int64Delta.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(1)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetTimestamp(later)
					p1.SetStartTimestamp(later)
					p1.SetIntValue(10)
					p1.Attributes().PutStr("label1", "label1-value1")
					p1.Attributes().PutStr("label2", "label2-value1")

					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/request_count_delta",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
								"label2": "label2-value1",
							},
							StartTime: laterMinusMsTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{10},
						},
					},
				},
			}),
		},
		{
			name: "int64_delta_no_start_time",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					int64Delta.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(1)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetTimestamp(later)
					p1.SetIntValue(10)
					p1.Attributes().PutStr("label1", "label1-value1")
					p1.Attributes().PutStr("label2", "label2-value1")

					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/request_count_delta",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
								"label2": "label2-value1",
							},
							StartTime: laterMinusMsTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{10},
						},
					},
				},
			}),
		},
		{
			name: "double_cumulative",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleCumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(2)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetDoubleValue(1.2)

					p2 := m.Sum().DataPoints().AppendEmpty()
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetDoubleValue(1.3)
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/float_sum",
					MetricValues: []*scpb.MetricValue{
						{
							Labels:    map[string]string{},
							StartTime: startTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_DoubleValue{1.2},
						},
						{
							Labels:    map[string]string{},
							StartTime: startTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_DoubleValue{1.3},
						},
					},
				},
			}),
		},
		{
			name: "cumulative_start_time_eq_end_time",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleCumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(1)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetTimestamp(later)
					p1.SetStartTimestamp(later)
					p1.SetDoubleValue(1.2)

					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/float_sum",
					MetricValues: []*scpb.MetricValue{
						{
							Labels:    map[string]string{},
							EndTime:   laterTs,
							StartTime: testExporterStartTimeTs,
							Value:     &scpb.MetricValue_DoubleValue{1.2},
						},
					},
				},
			}),
		},
		{
			name: "cumulative_no_start_time",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleCumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(1)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetTimestamp(later)
					p1.SetDoubleValue(1.2)

					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/float_sum",
					MetricValues: []*scpb.MetricValue{
						{
							Labels:    map[string]string{},
							EndTime:   laterTs,
							StartTime: testExporterStartTimeTs,
							Value:     &scpb.MetricValue_DoubleValue{1.2},
						},
					},
				},
			}),
		},
		{
			name: "int_gauge",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					int64Gauge.CopyTo(m)
					m.Gauge().DataPoints().EnsureCapacity(2)

					p1 := m.Gauge().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetIntValue(20)
					p1.Attributes().PutStr("label1", "label1-value1")

					p2 := m.Gauge().DataPoints().AppendEmpty()
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetIntValue(30)
					p2.Attributes().PutStr("label1", "label1-value2")
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/latency",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: laterTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{20},
						},
						{
							Labels: map[string]string{
								"label1": "label1-value2",
							},
							StartTime: laterTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_Int64Value{30},
						},
					},
				},
			}),
		},
		{
			name: "double_gauge",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleGauge.CopyTo(m)
					m.Gauge().DataPoints().EnsureCapacity(2)

					p1 := m.Gauge().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetDoubleValue(0.33)
					p1.Attributes().PutStr("label1", "label1-value1")

					p2 := m.Gauge().DataPoints().AppendEmpty()
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetDoubleValue(0.5)
					p2.Attributes().PutStr("label1", "label1-value2")
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/utilization",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: laterTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_DoubleValue{0.33},
						},
						{
							Labels: map[string]string{
								"label1": "label1-value2",
							},
							StartTime: laterTs,
							EndTime:   laterTs,
							Value:     &scpb.MetricValue_DoubleValue{0.5},
						},
					},
				},
			}),
		},
		{
			name: "distribution_cumulative_start_time_eq_end_time",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					distributionCumulative.CopyTo(m)
					m.Histogram().DataPoints().EnsureCapacity(1)

					p := m.Histogram().DataPoints().AppendEmpty()
					p.SetTimestamp(later)
					p.SetStartTimestamp(later)
					p.Attributes().PutStr("label1", "label1-value1")
					p.SetCount(2)
					p.ExplicitBounds().FromRaw([]float64{5.0})
					p.BucketCounts().FromRaw([]uint64{1, 1, ^uint64(0) - 30})
					ex := p.Exemplars()
					ex.EnsureCapacity(2)

					e1 := ex.AppendEmpty()
					e1.SetTimestamp(later)
					e1.SetDoubleValue(1.0)

					e2 := ex.AppendEmpty()
					e2.SetTimestamp(later)
					e2.SetDoubleValue(9.0)
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/latency_distribution",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: testExporterStartTimeTs,
							EndTime:   laterTs,
							Value: &scpb.MetricValue_DistributionValue{
								&scpb.Distribution{
									BucketCounts: []int64{1, 1, int64(^uint64(0) >> 1)},
									Count:        2,
									Exemplars: []*distribution.Distribution_Exemplar{
										{
											Timestamp: laterTs,
											Value:     1.0,
										},
										{
											Timestamp: laterTs,
											Value:     9.0,
										},
									},
									Mean:                  5,
									SumOfSquaredDeviation: 32,
									BucketOption: &scpb.Distribution_ExplicitBuckets_{
										&scpb.Distribution_ExplicitBuckets{
											Bounds: []float64{5.0},
										},
									},
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "distribution_cumulative_no_start_time",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					distributionCumulative.CopyTo(m)
					m.Histogram().DataPoints().EnsureCapacity(1)

					p := m.Histogram().DataPoints().AppendEmpty()
					p.SetTimestamp(later)
					p.Attributes().PutStr("label1", "label1-value1")
					p.SetCount(2)
					p.ExplicitBounds().FromRaw([]float64{5.0})
					p.BucketCounts().FromRaw([]uint64{1, 1})
					ex := p.Exemplars()
					ex.EnsureCapacity(2)

					e1 := ex.AppendEmpty()
					e1.SetTimestamp(later)
					e1.SetDoubleValue(1.0)

					e2 := ex.AppendEmpty()
					e2.SetTimestamp(later)
					e2.SetDoubleValue(9.0)
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/latency_distribution",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: testExporterStartTimeTs,
							EndTime:   laterTs,
							Value: &scpb.MetricValue_DistributionValue{
								&scpb.Distribution{
									BucketCounts: []int64{1, 1},
									Count:        2,
									Exemplars: []*distribution.Distribution_Exemplar{
										{
											Timestamp: laterTs,
											Value:     1.0,
										},
										{
											Timestamp: laterTs,
											Value:     9.0,
										},
									},
									Mean:                  5,
									SumOfSquaredDeviation: 32,
									BucketOption: &scpb.Distribution_ExplicitBuckets_{
										&scpb.Distribution_ExplicitBuckets{
											Bounds: []float64{5.0},
										},
									},
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "distribution_cumulative_zero_count",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					distributionCumulative.CopyTo(m)
					m.Histogram().DataPoints().EnsureCapacity(1)

					p := m.Histogram().DataPoints().AppendEmpty()
					p.SetTimestamp(later)
					p.SetStartTimestamp(start)
					p.Attributes().PutStr("label1", "label1-value1")
					p.SetCount(0)
					p.SetSum(11)
					p.ExplicitBounds().FromRaw([]float64{5.0})
					p.BucketCounts().FromRaw([]uint64{1, 1})
					ex := p.Exemplars()
					ex.EnsureCapacity(2)

					e1 := ex.AppendEmpty()
					e1.SetTimestamp(later)
					e1.SetDoubleValue(1.0)

					e2 := ex.AppendEmpty()
					e2.SetTimestamp(later)
					e2.SetDoubleValue(9.0)
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/latency_distribution",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value: &scpb.MetricValue_DistributionValue{
								&scpb.Distribution{
									BucketCounts: []int64{1, 1},
									Count:        0,
									Exemplars: []*distribution.Distribution_Exemplar{
										{
											Timestamp: laterTs,
											Value:     1.0,
										},
										{
											Timestamp: laterTs,
											Value:     9.0,
										},
									},
									BucketOption: &scpb.Distribution_ExplicitBuckets_{
										&scpb.Distribution_ExplicitBuckets{
											Bounds: []float64{5.0},
										},
									},
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "distribution_cumulative_no_exemplars",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					distributionCumulative.CopyTo(m)
					m.Histogram().DataPoints().EnsureCapacity(1)

					p := m.Histogram().DataPoints().AppendEmpty()
					p.SetTimestamp(later)
					p.SetStartTimestamp(start)
					p.Attributes().PutStr("label1", "label1-value1")
					p.SetCount(2)
					p.SetSum(11)
					p.ExplicitBounds().FromRaw([]float64{5.0})
					p.BucketCounts().FromRaw([]uint64{1, 1})
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/latency_distribution",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value: &scpb.MetricValue_DistributionValue{
								&scpb.Distribution{
									BucketCounts: []int64{1, 1},
									Count:        2,
									Mean:         5.5,
									BucketOption: &scpb.Distribution_ExplicitBuckets_{
										&scpb.Distribution_ExplicitBuckets{
											Bounds: []float64{5.0},
										},
									},
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "distribution_cumulative",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					distributionCumulative.CopyTo(m)
					m.Histogram().DataPoints().EnsureCapacity(1)

					p := m.Histogram().DataPoints().AppendEmpty()
					p.SetTimestamp(later)
					p.SetStartTimestamp(start)
					p.Attributes().PutStr("label1", "label1-value1")
					p.SetCount(2)
					p.ExplicitBounds().FromRaw([]float64{5.0})
					p.BucketCounts().FromRaw([]uint64{1, 1})
					ex := p.Exemplars()
					ex.EnsureCapacity(2)

					e1 := ex.AppendEmpty()
					e1.SetTimestamp(later)
					e1.SetDoubleValue(1.0)

					e2 := ex.AppendEmpty()
					e2.SetTimestamp(later)
					e2.SetDoubleValue(9.0)
					return []pmetric.Metric{m}
				}(),
				Resource: emptyResource(),
			},
			want: createSingleOp([]*scpb.MetricValueSet{
				{
					MetricName: "testservice.com/latency_distribution",
					MetricValues: []*scpb.MetricValue{
						{
							Labels: map[string]string{
								"label1": "label1-value1",
							},
							StartTime: startTs,
							EndTime:   laterTs,
							Value: &scpb.MetricValue_DistributionValue{
								&scpb.Distribution{
									BucketCounts: []int64{1, 1},
									Count:        2,
									Exemplars: []*distribution.Distribution_Exemplar{
										{
											Timestamp: laterTs,
											Value:     1.0,
										},
										{
											Timestamp: laterTs,
											Value:     9.0,
										},
									},
									Mean:                  5,
									SumOfSquaredDeviation: 32,
									BucketOption: &scpb.Distribution_ExplicitBuckets_{
										&scpb.Distribution_ExplicitBuckets{
											Bounds: []float64{5.0},
										},
									},
								},
							},
						},
					},
				},
			}),
		},
		{
			name: "labels_from_resource",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleCumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(2)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetDoubleValue(1.2)

					return []pmetric.Metric{m}
				}(),
				Resource: func() pcommon.Resource {
					r := pcommon.NewResource()
					r.Attributes().PutStr(gcpLocation, testLocation)
					return r
				}(),
			},
			want: []*scpb.Operation{
				{
					ConsumerId:    testConsumerID,
					Labels:        map[string]string{gcpLocation: testLocation},
					OperationName: "OpenTelemetry Reported Metrics",
					MetricValueSets: []*scpb.MetricValueSet{
						{
							MetricName: "testservice.com/float_sum",
							MetricValues: []*scpb.MetricValue{
								{
									Labels:    map[string]string{},
									StartTime: startTs,
									EndTime:   laterTs,
									Value:     &scpb.MetricValue_DoubleValue{1.2},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "dynamic_consumer_id",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleCumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(1)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetDoubleValue(1.2)
					p1.Attributes().PutStr("label1", "label1-value1")

					m2 := pmetric.NewMetric()
					m.CopyTo(m2)
					p2 := m.Sum().DataPoints().At(0)
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetDoubleValue(1.3)
					p2.Attributes().PutStr("label1", "label1-value2")

					return []pmetric.Metric{m2, m}
				}(),
				Resource: func() pcommon.Resource {
					r := pcommon.NewResource()
					r.Attributes().PutStr(dynamicConsumerAttribute, "consumer-project-dynamic")
					return r
				}(),
			},
			want: []*scpb.Operation{
				createOperationWithConsumer("consumer-project-dynamic", []*scpb.MetricValueSet{
					{
						MetricName: "testservice.com/float_sum",
						MetricValues: []*scpb.MetricValue{
							{
								Labels:    map[string]string{"label1": "label1-value1"},
								StartTime: startTs,
								EndTime:   laterTs,
								Value:     &scpb.MetricValue_DoubleValue{1.2},
							},
						},
					},
				}),
				createOperationWithConsumer("consumer-project-dynamic", []*scpb.MetricValueSet{
					{
						MetricName: "testservice.com/float_sum",
						MetricValues: []*scpb.MetricValue{
							{
								Labels:    map[string]string{"label1": "label1-value2"},
								StartTime: startTs,
								EndTime:   laterTs,
								Value:     &scpb.MetricValue_DoubleValue{1.3},
							},
						},
					},
				}),
			},
		},
		{
			name: "same_metric_creates_two_operations",
			metrics: metricData{
				Metrics: func() []pmetric.Metric {
					m := pmetric.NewMetric()
					doubleCumulative.CopyTo(m)
					m.Sum().DataPoints().EnsureCapacity(1)

					p1 := m.Sum().DataPoints().AppendEmpty()
					p1.SetStartTimestamp(start)
					p1.SetTimestamp(later)
					p1.SetDoubleValue(1.2)
					p1.Attributes().PutStr("label1", "label1-value1")

					m2 := pmetric.NewMetric()
					m.CopyTo(m2)
					p2 := m.Sum().DataPoints().At(0)
					p2.SetStartTimestamp(start)
					p2.SetTimestamp(later)
					p2.SetDoubleValue(1.3)
					p2.Attributes().PutStr("label1", "label1-value2")

					return []pmetric.Metric{m2, m}
				}(),
				Resource: emptyResource(),
			},
			want: []*scpb.Operation{
				createOperation([]*scpb.MetricValueSet{
					{
						MetricName: "testservice.com/float_sum",
						MetricValues: []*scpb.MetricValue{
							{
								Labels:    map[string]string{"label1": "label1-value1"},
								StartTime: startTs,
								EndTime:   laterTs,
								Value:     &scpb.MetricValue_DoubleValue{1.2},
							},
						},
					},
				}),
				createOperation([]*scpb.MetricValueSet{
					{
						MetricName: "testservice.com/float_sum",
						MetricValues: []*scpb.MetricValue{
							{
								Labels:    map[string]string{"label1": "label1-value2"},
								StartTime: startTs,
								EndTime:   laterTs,
								Value:     &scpb.MetricValue_DoubleValue{1.3},
							},
						},
					},
				}),
			},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := newFakeClient(noError)
			cfg := Config{
				ServiceName:        testServiceID,
				ConsumerProject:    testConsumerID,
				ServiceConfigID:    testServiceConfigID,
				EnableDebugHeaders: true,
			}
			e := NewMetricsExporter(cfg, zap.NewNop(), c, componenttest.NewNopTelemetrySettings())
			e.exporterStartTime, _ = time.Parse(time.RFC3339, testExporterStartTime)

			err := e.ConsumeMetrics(context.Background(), metricDataToPmetric(tc.metrics))

			require.NoError(t, err)
			if len(c.requests) != 1 {
				t.Errorf("Unexpected number of requests to service control API, got %d, want 1", len(c.requests))
			}

			request := c.requests[0]
			if diff := cmp.Diff(request.ServiceConfigId, testServiceConfigID); diff != "" {
				t.Errorf("ServiceConfigId differs, -got +want: %s", diff)
			}
			if diff := cmp.Diff(request.Operations, tc.want, cleanOperation, cmpopts.SortSlices(operationLess), cmpopts.SortSlices(metricValueLess), unexportedOptsForScRequest()); diff != "" {
				t.Errorf("Operations differ, -got +want: %s", diff)
			}
			for _, op := range request.Operations {
				if op.OperationId == "" {
					t.Errorf("Operation required field was not set, field: OperationID, operation: %v", op)
				}
				if !op.StartTime.IsValid() {
					t.Errorf("Operation required field was not set, field: StartTime, operation: %v", op)
				}
				if !op.EndTime.IsValid() {
					t.Errorf("Operation required field was not set, field: EndTime, operation: %v", op)
				}
			}
		})
	}
}

func TestErrorPropagation(t *testing.T) {
	metrics := sampleMetricData(t)
	c := newFakeClient(fakeError)
	cfg := Config{
		ServiceName:        testServiceID,
		ConsumerProject:    testConsumerID,
		ServiceConfigID:    testServiceConfigID,
		EnableDebugHeaders: true,
	}
	e := NewMetricsExporter(cfg, zap.NewNop(), c, componenttest.NewNopTelemetrySettings())

	err := e.ConsumeMetrics(context.Background(), metricDataToPmetric(metrics))
	if err == nil {
		t.Errorf("Expected to have an error, but ConsumeMetrics returned nil")
	}
}

func TestCreateOperations(t *testing.T) {
	// Logic of the test: send several metrics to our Service Control exporter,
	// and expect it to create several Operation-s.
	m := sampleMetricData(t)

	createRms := func(metrics []pmetric.Metric) pmetric.ResourceMetricsSlice {
		rms := pmetric.NewResourceMetricsSlice()
		rm := rms.AppendEmpty()
		m.Resource.CopyTo(rm.Resource())

		scopeMetrics := rm.ScopeMetrics()
		sm := scopeMetrics.AppendEmpty()

		for _, metric := range metrics {
			m := sm.Metrics().AppendEmpty()
			metric.CopyTo(m)
		}

		return rms
	}

	tests := []struct {
		name        string
		metricsFunc func() []pmetric.Metric
		wantOpsFunc func(*MetricsExporter, []pmetric.Metric, time.Time) []*scpb.Operation
	}{
		{
			// If X and Y are metric names, then we have the following Metrics: [X, Y, X, Y].
			// Values are not important in the scope of this test.
			// We expect to have 4 operations: [X], [Y], [X], [Y].
			name: "two_segments",
			metricsFunc: func() []pmetric.Metric {
				met := m.Metrics[0:2]
				met = append(met, met...)
				return met
			},
			wantOpsFunc: func(e *MetricsExporter, met []pmetric.Metric, now time.Time) []*scpb.Operation {
				return []*scpb.Operation{
					e.createOperation(expectedResourceAttributes, met[0:1][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[1:2][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[2:3][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[3:4][0], now, testConsumerID),
				}
			},
		},
		{
			// [X, Y, Z] -> [X], [Y], [Z]
			name: "all_different",
			metricsFunc: func() []pmetric.Metric {
				return m.Metrics
			},
			wantOpsFunc: func(e *MetricsExporter, met []pmetric.Metric, now time.Time) []*scpb.Operation {
				return []*scpb.Operation{
					e.createOperation(expectedResourceAttributes, met[0:1][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[1:2][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[2:3][0], now, testConsumerID),
				}
			},
		},
		{
			// [X, X, X] -> [X], [X], [X]
			name: "all_identical",
			metricsFunc: func() []pmetric.Metric {
				met := m.Metrics[0:1]
				met = append(met, m.Metrics[0])
				met = append(met, m.Metrics[0])
				return met
			},
			wantOpsFunc: func(e *MetricsExporter, met []pmetric.Metric, now time.Time) []*scpb.Operation {
				return []*scpb.Operation{
					e.createOperation(expectedResourceAttributes, met[0:1][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[1:2][0], now, testConsumerID),
					e.createOperation(expectedResourceAttributes, met[2:3][0], now, testConsumerID),
				}
			},
		},
		{
			// [] -> []
			name: "empty",
			metricsFunc: func() []pmetric.Metric {
				return []pmetric.Metric{}
			},
			wantOpsFunc: func(e *MetricsExporter, met []pmetric.Metric, now time.Time) []*scpb.Operation {
				return []*scpb.Operation{}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				ServiceName:        testServiceID,
				ConsumerProject:    testConsumerID,
				ServiceConfigID:    testServiceConfigID,
				EnableDebugHeaders: true,
			}
			e := NewMetricsExporter(cfg, zap.NewNop(), newFakeClient(noError), componenttest.NewNopTelemetrySettings())
			metrics := tc.metricsFunc()

			ops := e.createReportRequest(createRms(metrics)).Operations
			now := time.Now()
			if len(ops) > 0 {
				// `createOperations` calls time.Now() inside. We can't predict
				// what the value will be, so we just read it from the output.
				now = ops[0].EndTime.AsTime()
			}

			wantOps := tc.wantOpsFunc(e, metrics, now)
			if diff := cmp.Diff(ops, wantOps, cleanOperation, cmpopts.SortSlices(operationLess), cmpopts.SortSlices(metricValueLess), unexportedOptsForScRequest()); diff != "" {
				t.Errorf("Operations differ, -got +want: %s", diff)
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	// Logic of this test:
	// - make server respond very slowly
	// - send a request to server
	// - if the timeout works, we'll get a response sooner than "very slowly"
	metrics := sampleMetricData(t)
	timeout := 3 * time.Second
	e, _ := createExporterWithSleepingScServer(t, timeout, false, nil)

	before := time.Now()
	e.ConsumeMetrics(context.Background(), metricDataToPmetric(metrics))
	diff := time.Since(before)
	// `+1` is because diff can be smth like "defaultTimeout+0.004".
	// `-1` is to check that server slept.
	if got := diff.Seconds(); got < timeout.Seconds()-1 || got > timeout.Seconds()+1 {
		t.Errorf("Expected the request to complete in about %f seconds, got %v", timeout.Seconds(), got)
	}
}

func TestRetries(t *testing.T) {
	// Logic of this test:
	// - make fake server reply very slowly
	// - this will result in DeadlineExceeded in our exporter's ConsumeMetrics
	// - hence, the exporterhelper wrapper should retry the call
	// We check our fake server and except to see more than one request resulting from 1 call to wrapped ConsumeMetrics.
	tests := []struct {
		name                   string
		minNumRequests         int
		wantExactlyMinRequests bool
		errorOnSleep           error
	}{
		{
			name:           "retriable ctx DeadlineExceeded",
			minNumRequests: 2,
			errorOnSleep:   context.DeadlineExceeded,
		},
		{
			name:           "retriable unavailable",
			minNumRequests: 2,
			errorOnSleep:   status.Error(codes.Unavailable, "service unavailable"),
		},
		{
			name:           "retriable deadline exceeded",
			minNumRequests: 2,
			errorOnSleep:   status.Error(codes.DeadlineExceeded, "deadline exceeded"),
		},
		{
			name:                   "nonretriable invalid argument",
			minNumRequests:         1,
			wantExactlyMinRequests: true,
			errorOnSleep:           status.Error(codes.InvalidArgument, "service name is missing"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metrics := sampleMetricData(t)
			e, scClient := createExporterWithSleepingScServer(t, 1*time.Second, true, tc.errorOnSleep)

			e.ConsumeMetrics(context.Background(), metricDataToPmetric(metrics))

			got := len(scClient.requests)
			if tc.wantExactlyMinRequests && got != tc.minNumRequests {
				t.Errorf("Wrong number of Service Control requests: got %d, want exactly %d", got, tc.minNumRequests)
			}
			if !tc.wantExactlyMinRequests && got < tc.minNumRequests {
				t.Errorf("Wrong number of Service Control requests: got %d, want >= %d", got, tc.minNumRequests)
			}
		})
	}

}

func TestExporterStartTime(t *testing.T) {
	c := newFakeClient(noError)
	now := time.Now()
	cfg := Config{
		ServiceName:        testServiceID,
		ConsumerProject:    testConsumerID,
		ServiceConfigID:    testServiceConfigID,
		EnableDebugHeaders: true,
	}
	e := NewMetricsExporter(cfg, zap.NewNop(), c, componenttest.NewNopTelemetrySettings())

	if e.exporterStartTime.Before(now) {
		t.Errorf("Wrong exporter start time: got %v, want >= %v", e.exporterStartTime, now)
	}
	if future := now.Add(1 * time.Minute); e.exporterStartTime.After(future) {
		t.Errorf("Wrong exporter start time: got %v, want <= %v", e.exporterStartTime, future)
	}
}

func TestParseConsumerID(t *testing.T) {
	c := newFakeClient(noError)

	tests := []struct {
		consumerID string
		want       string
	}{
		{consumerID: "project:1234", want: "project:1234"},
		{consumerID: "project_number:1234", want: "project_number:1234"},
		{consumerID: "projects/1234", want: "projects/1234"},
		{consumerID: "folders/1234", want: "folders/1234"},
		{consumerID: "organizations/1234", want: "organizations/1234"},
		{consumerID: "api_key:1234", want: "api_key:1234"},
		{consumerID: "projectid", want: "projects/projectid"},
	}
	for _, tc := range tests {
		cfg := Config{
			ServiceName:        testServiceID,
			ConsumerProject:    tc.consumerID,
			ServiceConfigID:    testServiceConfigID,
			EnableDebugHeaders: true,
		}
		e := NewMetricsExporter(cfg, zap.NewNop(), c, componenttest.NewNopTelemetrySettings())
		if e.consumerID != tc.want {
			t.Errorf("consumerID differs, got: %s, want: %s", e.consumerID, tc.want)
		}
	}

}

func TestMetricsExporter_ExponentialHistogram(t *testing.T) {
	// Test ExponentialHistogram metric type support
	mockClient := &mockServiceControlClient{}
	exporter := createMetricsExporter(t, mockClient)

	// Create test ExponentialHistogram metric
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	metric := sm.Metrics().AppendEmpty()

	metric.SetName("test_exponential_histogram")
	metric.SetDescription("Test exponential histogram metric")

	// Set up ExponentialHistogram
	expHist := metric.SetEmptyExponentialHistogram()
	expHist.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)

	// Add data point
	dp := expHist.DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	dp.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-time.Minute)))
	dp.SetCount(100)
	dp.SetSum(1000.0)
	dp.SetScale(2)
	dp.SetZeroCount(5)

	// Set positive buckets
	posBuckets := dp.Positive()
	posBuckets.SetOffset(1)
	posBuckets.BucketCounts().FromRaw([]uint64{10, 20, 30, 40})

	// Set negative buckets
	negBuckets := dp.Negative()
	negBuckets.SetOffset(-2)
	negBuckets.BucketCounts().FromRaw([]uint64{5, 10, 15})

	// Add attributes
	dp.Attributes().PutStr("service", "test-service")
	dp.Attributes().PutStr("version", "1.0")

	// Test consumption
	err := exporter.ConsumeMetrics(context.Background(), metrics)

	require.NoError(t, err)

	// Verify the request was sent
	require.Len(t, mockClient.requests, 1)
	req := mockClient.requests[0]
	require.Len(t, req.Operations, 1)

	op := req.Operations[0]
	require.Len(t, op.MetricValueSets, 1)

	mvs := op.MetricValueSets[0]
	require.Equal(t, "test_exponential_histogram", mvs.MetricName)
	require.Len(t, mvs.MetricValues, 1)

	mv := mvs.MetricValues[0]
	require.NotNil(t, mv.Value)

	// Verify it was converted to distribution value
	distValue, ok := mv.Value.(*scpb.MetricValue_DistributionValue)
	require.True(t, ok, "Expected DistributionValue for ExponentialHistogram")
	require.NotNil(t, distValue.DistributionValue)

	dist := distValue.DistributionValue
	require.Equal(t, int64(100), dist.Count)
	require.InDelta(t, 10.0, dist.Mean, 0.001) // sum/count = 1000/100 = 10

	// Verify labels
	require.Equal(t, "test-service", mv.Labels["service"])
	require.Equal(t, "1.0", mv.Labels["version"])
}

func TestMetricsExporter_Summary(t *testing.T) {
	// Test Summary metric type support
	mockClient := &mockServiceControlClient{}
	exporter := createMetricsExporter(t, mockClient)

	// Create test Summary metric
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	metric := sm.Metrics().AppendEmpty()

	metric.SetName("test_summary")
	metric.SetDescription("Test summary metric")

	// Set up Summary
	summary := metric.SetEmptySummary()

	// Add data point
	dp := summary.DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	dp.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(-time.Minute)))
	dp.SetCount(50)
	dp.SetSum(500.0)

	// Add quantile values
	quantiles := dp.QuantileValues()

	q50 := quantiles.AppendEmpty()
	q50.SetQuantile(0.5)
	q50.SetValue(8.0)

	q90 := quantiles.AppendEmpty()
	q90.SetQuantile(0.9)
	q90.SetValue(15.0)

	q99 := quantiles.AppendEmpty()
	q99.SetQuantile(0.99)
	q99.SetValue(20.0)

	// Add attributes
	dp.Attributes().PutStr("endpoint", "/api/test")
	dp.Attributes().PutStr("method", "GET")

	// Test consumption
	err := exporter.ConsumeMetrics(context.Background(), metrics)

	require.NoError(t, err)

	// Verify the request was sent
	require.Len(t, mockClient.requests, 1)
	req := mockClient.requests[0]
	require.Len(t, req.Operations, 1)

	op := req.Operations[0]
	require.Len(t, op.MetricValueSets, 1)

	mvs := op.MetricValueSets[0]
	require.Equal(t, "test_summary", mvs.MetricName)
	require.Len(t, mvs.MetricValues, 1)

	mv := mvs.MetricValues[0]
	require.NotNil(t, mv.Value)

	// Verify it was converted to distribution value
	distValue, ok := mv.Value.(*scpb.MetricValue_DistributionValue)
	require.True(t, ok, "Expected DistributionValue for Summary")
	require.NotNil(t, distValue.DistributionValue)

	dist := distValue.DistributionValue
	require.Equal(t, int64(50), dist.Count)
	require.InDelta(t, 10.0, dist.Mean, 0.001) // sum/count = 500/50 = 10

	// Verify labels
	require.Equal(t, "/api/test", mv.Labels["endpoint"])
	require.Equal(t, "GET", mv.Labels["method"])
}

func TestMetricsExporter_UnsupportedMetricTypes_LogsWarning(t *testing.T) {
	// Test that unsupported metric types log appropriate warnings
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	mockClient := &mockServiceControlClient{}
	config := createDefaultConfig().(*Config)
	exporter := NewMetricsExporter(*config, logger, mockClient, componenttest.NewNopTelemetrySettings())

	// Create metrics with currently unsupported type (before our implementation)
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()

	// Add an unsupported metric type by creating an empty metric
	// and checking the warning is logged
	metric := sm.Metrics().AppendEmpty()
	metric.SetName("unsupported_metric")

	// This should trigger the default case and log a warning
	err := exporter.ConsumeMetrics(context.Background(), metrics)
	require.NoError(t, err)

	// Verify warning was logged
	require.Greater(t, logs.Len(), 0)
	logEntry := logs.All()[0]
	require.Equal(t, zap.WarnLevel, logEntry.Level)
	require.Contains(t, logEntry.Message, "Metric type unsupported")
}

func newFakeClient(errFunc func(context.Context) error) *fakeClient {
	return &fakeClient{
		requests: []*scpb.ReportRequest{},
		errFunc:  errFunc,
	}
}

type fakeClient struct {
	requests []*scpb.ReportRequest
	errFunc  func(context.Context) error
	mutex    sync.Mutex
}

func (c *fakeClient) Report(ctx context.Context, request *scpb.ReportRequest) (*scpb.ReportResponse, error) {
	c.mutex.Lock()
	c.requests = append(c.requests, request)
	c.mutex.Unlock()
	return &scpb.ReportResponse{
		ReportErrors:    nil,
		ServiceConfigId: "fake-id",
	}, c.errFunc(ctx)
}

func (c *fakeClient) Close() error {
	return nil
}

var cleanOperation = cmp.Transformer("cleanOperation", func(op *scpb.Operation) interface{} {
	tmp := *op
	tmp.OperationId = ""
	tmp.StartTime = nil
	tmp.EndTime = nil
	return tmp
})
