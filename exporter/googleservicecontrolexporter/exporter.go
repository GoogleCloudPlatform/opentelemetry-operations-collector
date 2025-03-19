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
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/pborman/uuid"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
	"google.golang.org/genproto/googleapis/api/distribution"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	operationName = "OpenTelemetry Reported Metrics"
	// OTel resource attribute that can override the static `consumer_project` setting from factory.go.
	dynamicConsumerAttribute = "servicecontrol.consumer_id"

	// We don't print every failed request, since it may make logs unreadable.
	logEveryNthError = 20
	// 5000 is enough for an entire small request to fit, and it's not very big.
	// For collectd agent, we had value=1024 and it was too small.
	requestTrimSize = 5000

	// Constant for adding debug encrypted request header
	// go/encrypted-debug-headers
	debugHeaderKey            = "X-Return-Encrypted-Headers"
	debugHeaderVal            = "all_response"
	debugHeaderTimeoutMinutes = 3
)

var (
	consumerIDSlashPrefix = map[string]bool{
		"projects":      true,
		"folders":       true,
		"organizations": true,
	}
	consumerIDColonPrefix = map[string]bool{
		"project":        true,
		"project_number": true,
		"api_key":        true,
	}
)

// Exporter is a type that implements MetricsExporter interface for ServiceControl API
type Exporter struct {
	exporterStartTime time.Time
	serviceName       string
	consumerID        string
	serviceConfigID   string

	client ServiceControlClient
	tel    component.TelemetrySettings
	host   component.Host
	logger *zap.SugaredLogger
	// Mutex for `errCnt` variable.
	errMu  sync.Mutex
	errCnt int
	// For adding debug encrypted header (b/347298668)
	enableDebugHeaders        bool
	debugHeaderMutex          sync.Mutex
	debugHeaderExpirationTime time.Time
	nowFunc                   func() time.Time
}

// Start starts Exporter
func (e *Exporter) Start(_ context.Context, host component.Host) error {
	e.host = host
	return nil
}

// Shutdown cancels ongoing requests
func (e *Exporter) Shutdown(_ context.Context) error {
	e.client.Close()
	return nil
}

// NewExporter returns service control exporter
func NewExporter(logger *zap.Logger, c ServiceControlClient, serviceName, consumerID, serviceConfigID string, enableDebugHeaders bool, tel component.TelemetrySettings) *Exporter {
	return &Exporter{
		// Sugared logger has a more convenient API: https://pkg.go.dev/go.uber.org/zap#SugaredLogger.
		logger:             logger.Sugar(),
		serviceName:        serviceName,
		consumerID:         parseConsumerID(consumerID),
		client:             c,
		serviceConfigID:    serviceConfigID,
		errCnt:             0,
		exporterStartTime:  time.Now(),
		tel:                tel,
		enableDebugHeaders: enableDebugHeaders,
		nowFunc:            time.Now,
	}
}

func parseConsumerID(consumerID string) string {
	ssplit := strings.Split(consumerID, "/")
	_, sok := consumerIDSlashPrefix[ssplit[0]]
	if len(ssplit) > 1 && sok {
		return consumerID
	}
	csplit := strings.Split(consumerID, ":")
	_, cok := consumerIDColonPrefix[csplit[0]]
	if len(csplit) > 1 && cok {
		return consumerID
	}
	return "projects/" + consumerID
}

func (e *Exporter) logFailedReportReq(req *scpb.ReportRequest, err error) {
	e.errMu.Lock()
	defer e.errMu.Unlock()
	e.errCnt += 1
	e.logger.Warnf("Failed to export metrics: operation_id %v, error %v", req.Operations[0].OperationId, err)

	if e.errCnt%logEveryNthError == 1 {
		// If you are using dynamic consumer ids, this message will not show a correct consumer id. Look for the actual id in the request dump below.
		debugStr := fmt.Sprintf("service name: %v, default consumer id: %v, service config id: %v", e.serviceName, e.consumerID, e.serviceConfigID)
		rdump := req.String()
		// Trim the serialized request (if very large) to make it more readable.
		if len(rdump) > requestTrimSize {
			rdump = rdump[:requestTrimSize] + "..."
		}
		e.logger.Warnf("Failed Service Control Report request [%v]: %v", debugStr, rdump)
	}
}

func shouldRetry(err error) bool {
	// We want to retry Unavailable and Deadline Exceeded errors:
	// go/sc-retry
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.DeadlineExceeded || st.Code() == codes.Unavailable {
			return true
		}
	}
	return false
}

// ConsumeMetrics creates report requests from provided metrics and sends them to Service Control API.
// This func is called by several goroutines concurrently.
func (e *Exporter) ConsumeMetrics(ctx context.Context, m pmetric.Metrics) error {
	// Check if we need to add the debug encrypted header
	if e.enableDebugHeaders {
		e.debugHeaderMutex.Lock()
		if e.nowFunc().Before(e.debugHeaderExpirationTime) {
			ctx = metadata.AppendToOutgoingContext(ctx, debugHeaderKey, debugHeaderVal)
		}
		e.debugHeaderMutex.Unlock()
	}

	req := e.createReportRequest(m.ResourceMetrics())
	if len(req.Operations) == 0 {
		// Nothing to export.
		return nil
	}
	// This is thread-safe due to https://grpc.io/docs/languages/go/generated-code/:
	// "client-side RPC invocations and server-side RPC handlers are thread-safe and are meant to be run on concurrent goroutines".
	resp, err := e.client.Report(ctx, req)

	if err != nil {
		// ReportStatus tells health check that we had an error.
		componentstatus.ReportStatus(e.host, componentstatus.NewRecoverableErrorEvent(err))
		e.logFailedReportReq(req, err)

		if shouldRetry(err) {
			if e.enableDebugHeaders {
				// Get retriable error and enable debug header: Add encrypted debug header for 3 min
				e.debugHeaderMutex.Lock()
				if e.nowFunc().After(e.debugHeaderExpirationTime) {
					e.debugHeaderExpirationTime = e.nowFunc().Add(debugHeaderTimeoutMinutes * time.Minute)
				}
				e.debugHeaderMutex.Unlock()
			}
			return err
		}
		// "Permanent" tells OTel retry machinery that request should not be retried.
		return consumererror.NewPermanent(err)
	}

	for _, re := range resp.GetReportErrors() {
		e.logger.Warnf("Service Control Report() partially failed, operation %s rejected: %+v", re.OperationId, re.Status)
	}

	// ReportStatus tells health check that everything is OK.
	componentstatus.ReportStatus(e.host, componentstatus.NewEvent(componentstatus.StatusOK))

	return nil
}

// Capabilities returns the Capabilities associated with the metrics exporter.
func (e *Exporter) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// This function converts OTLP representation of the metrics (see definition in [1])
// to Service Control representation of the metrics (see definition in [2]).
//
// [1] https://github.com/open-telemetry/opentelemetry-proto/blob/main/opentelemetry/proto/metrics/v1/metrics.proto
// [2] https://cloud.google.com/service-infrastructure/docs/service-control/reference/rest/v1/Operation
func (e *Exporter) createReportRequest(rms pmetric.ResourceMetricsSlice) *scpb.ReportRequest {
	now := time.Now()
	request := scpb.ReportRequest{
		Operations:      make([]*scpb.Operation, 0),
		ServiceConfigId: e.serviceConfigID,
		ServiceName:     e.serviceName,
	}

	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		resource := rm.Resource()
		scopeMetricsSlice := rm.ScopeMetrics()
		resourceAttributes := attributesToStringMap(resource.Attributes())

		// By default, use consumerID from the exporter configuration.
		consumerID := e.consumerID

		// Allow users to override the consumerID by providing a resource attribute.
		if v, found := resourceAttributes[dynamicConsumerAttribute]; found {
			// Delete the attribute: it is only for Metrics Agent to understand the correct consumer id.
			// Service Control does not know about this label, and will complain if we send it.
			delete(resourceAttributes, dynamicConsumerAttribute)
			consumerID = v
		}

		for j := 0; j < scopeMetricsSlice.Len(); j++ {
			metrics := scopeMetricsSlice.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				request.Operations = append(request.Operations, e.createOperation(resourceAttributes, metrics.At(k), now, consumerID))
			}
		}
	}

	return &request
}

// We create a dedicated Operation for each metric. The API would drop the
// entire Operation if any metric point is invalid (e.g., old timestamp). Having
// one metric per operation can avoid valid metric points get dropped.
// see go/slm-monitoring-opentelemetry-batching for details.
func (e *Exporter) createOperation(resourceAttributes map[string]string, metric pmetric.Metric, now time.Time, consumerID string) *scpb.Operation {
	start := now
	op := scpb.Operation{
		ConsumerId:    consumerID,
		OperationName: operationName,
		EndTime:       timestamppb.New(now),
		// These labels are monitored resource labels.
		// Metric labels are stored in MetricValue proto.
		Labels:          resourceAttributes,
		MetricValueSets: make([]*scpb.MetricValueSet, 1),
		OperationId:     uuid.New(),
	}

	mvs, st := e.createMetricValueSet(metric)
	op.MetricValueSets[0] = mvs
	if !st.IsZero() && st.Before(start) {
		start = st
	}
	op.StartTime = timestamppb.New(start)

	return &op
}

func (e *Exporter) createMetricValueSet(metric pmetric.Metric) (*scpb.MetricValueSet, time.Time) {
	vs := &scpb.MetricValueSet{
		MetricName: metric.Name(),
	}

	var startTime time.Time
	var mv []*scpb.MetricValue
	t := metric.Type()
	switch t {
	case pmetric.MetricTypeGauge:
		mv, startTime = e.createNumericMetricValues(metric.Gauge().DataPoints(), pmetric.AggregationTemporalityUnspecified)
	case pmetric.MetricTypeSum:
		mv, startTime = e.createNumericMetricValues(metric.Sum().DataPoints(), metric.Sum().AggregationTemporality())
	case pmetric.MetricTypeHistogram:
		mv, startTime = e.createHistogramMetricValues(metric.Histogram())
	// TODO(b/401006109): handle ExponentialHistogram and Summary types
	default:
		e.logger.Warn("Metric type unsupported", zap.String("type", t.String()))
	}

	vs.MetricValues = mv
	return vs, startTime
}

func (e *Exporter) getStartEndTimes(aggr pmetric.AggregationTemporality, start time.Time, end time.Time) (time.Time, time.Time) {
	if aggr == pmetric.AggregationTemporalityUnspecified {
		// This is a Gauge metric.
		// According to https://cloud.google.com/monitoring/api/ref_v3/rpc/google.monitoring.v3#timeinterval
		// GAUGE values must have start_time==end_time.
		// According to OpenTelemetry specification, `StartTimestamp` is optional for Gauge metrics:
		// https://github.com/open-telemetry/opentelemetry-proto/blob/v0.18.0/opentelemetry/proto/metrics/v1/metrics.proto#L183-L184.
		// Hence, we rewrite `start`, not `end`.
		return end, end
	}

	// We know that our metric is either Cumulative or Delta.
	// https://cloud.google.com/monitoring/api/ref_v3/rpc/google.monitoring.v3#timeinterval
	// says that those metrics must have `start_time < end_time`, and a valid start_time.
	//
	// According to OpenTelemetry API, all metric points must have End time, but are not required to have Start time:
	// https://github.com/open-telemetry/opentelemetry-proto/blob/main/opentelemetry/proto/metrics/v1/metrics.proto#L144-L161.
	// Service Control API requires Start time in certain cases, so in this function we provide default values for StartTime.
	if start.Unix() == 0 || start == end {
		switch aggr {
		case pmetric.AggregationTemporalityCumulative:
			// This is a Cumulative metric.
			// According to https://cloud.google.com/monitoring/api/ref_v3/rpc/google.monitoring.v3#timeinterval
			// Cumulative values must have a start time which represents the beginning of the metric stream.
			// If start time is not provided, we're using Exporter's startup time as a default,
			// similar to what we do in collectd: go/collectd-reset
			start = e.exporterStartTime
		case pmetric.AggregationTemporalityDelta:
			// This is a Delta metric.
			// go/oneplatform-api-monitoring#should-i-use-delta-metrics-or-cumulative-metrics suggests that
			// we can use [time.Now() - 1ms, time.Now()] as the time interval.
			start = end.Add(-1 * time.Microsecond)
		default:
			e.logger.Errorf("Unexpected aggregation type: %v", aggr)
		}
	}

	return start, end
}

func (e *Exporter) createNumericMetricValues(points pmetric.NumberDataPointSlice, aggr pmetric.AggregationTemporality) ([]*scpb.MetricValue, time.Time) {
	var earliestStart time.Time
	ret := make([]*scpb.MetricValue, points.Len())

	for i := 0; i < points.Len(); i++ {
		point := points.At(i)
		start := point.StartTimestamp().AsTime()
		end := point.Timestamp().AsTime()

		start, end = e.getStartEndTimes(aggr, start, end)

		if earliestStart.IsZero() || start.Before(earliestStart) {
			earliestStart = start
		}

		mv := &scpb.MetricValue{
			Labels:    attributesToStringMap(point.Attributes()),
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(end),
		}

		switch point.ValueType() {
		case pmetric.NumberDataPointValueTypeInt:
			v := point.IntValue()
			mv.Value = &scpb.MetricValue_Int64Value{v}
		case pmetric.NumberDataPointValueTypeDouble:
			v := point.DoubleValue()
			mv.Value = &scpb.MetricValue_DoubleValue{v}
		}

		ret[i] = mv
	}

	return ret, earliestStart
}

func (e *Exporter) createHistogramMetricValues(m pmetric.Histogram) ([]*scpb.MetricValue, time.Time) {
	var earliestStart time.Time
	points := m.DataPoints()
	ret := make([]*scpb.MetricValue, points.Len())

	for i := 0; i < points.Len(); i++ {
		point := points.At(i)
		start := point.StartTimestamp().AsTime()
		end := point.Timestamp().AsTime()

		start, end = e.getStartEndTimes(m.AggregationTemporality(), start, end)

		if earliestStart.IsZero() || start.Before(earliestStart) {
			earliestStart = start
		}

		mv := &scpb.MetricValue{
			Labels:    attributesToStringMap(point.Attributes()),
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(end),
		}
		mv.Value = &scpb.MetricValue_DistributionValue{translateDistributionValue(point)}
		ret[i] = mv
	}

	return ret, earliestStart
}

func attributesToStringMap(attr pcommon.Map) map[string]string {
	m := map[string]string{}
	attr.Range(func(k string, v pcommon.Value) bool {
		m[k] = v.Str()
		return true
	})
	return m
}

func translateDistributionValue(value pmetric.HistogramDataPoint) *scpb.Distribution {
	result := &scpb.Distribution{
		Count: int64(value.Count()),
		BucketOption: &scpb.Distribution_ExplicitBuckets_{
			&scpb.Distribution_ExplicitBuckets{
				Bounds: value.ExplicitBounds().AsRaw(),
			},
		},
		BucketCounts: toInt64Slice(value.BucketCounts().AsRaw()),
	}

	exemplars := value.Exemplars()
	// We compute `sum` from `exemplars`, instead of getting it from `value.Sum()`.
	// Sum is optional in pmetric.HistogramDataPoint.
	// Note that Exemplars are optional as well.
	var sum float64
	for i := 0; i < exemplars.Len(); i++ {
		ex := exemplars.At(i)
		// Service Control only has double value type:
		// https://github.com/googleapis/googleapis/blob/40bad3ea0d48ecf250296ea7438035b8e45227dd/google/api/distribution.proto#L147,
		// so we convert everything to float64.
		var value float64
		switch ex.ValueType() {
		case pmetric.ExemplarValueTypeDouble:
			value = ex.DoubleValue()
		case pmetric.ExemplarValueTypeInt:
			value = float64(ex.IntValue())
		}
		sum += value
		result.Exemplars = append(result.Exemplars, &distribution.Distribution_Exemplar{
			Value:     value,
			Timestamp: timestamppb.New(ex.Timestamp().AsTime()),
		})
	}

	// Service Control API does not like if we set non-zero mean and deviation when count=0:
	// https://github.com/googleapis/google-api-go-client/blob/8a616df18563c9fedaead92873d200cd8c2d0503/servicecontrol/v1/servicecontrol-gen.go#L853
	if value.Count() > 0 {
		if exemplars.Len() > 0 {
			// We can calculate everything ourselves.
			mean := sum / float64(value.Count())
			result.Mean = mean

			// SumOfSquaredDeviation calculation:
			// https://github.com/googleapis/google-api-go-client/blob/8a616df18563c9fedaead92873d200cd8c2d0503/servicecontrol/v1/servicecontrol-gen.go#L860
			var sumDev float64
			for _, exemplar := range result.Exemplars {
				dev := exemplar.Value - mean
				sumDev += dev * dev
			}
			result.SumOfSquaredDeviation = sumDev
		} else {
			// We can only hope that `value.Sum()` is populated, and calculate `result.Mean` from that.
			// There is no way to calculate `result.SumOfSquaredDeviation`.
			result.Mean = value.Sum() / float64(value.Count())
		}
	}
	return result
}

func toInt64Slice(v []uint64) []int64 {
	ret := make([]int64, len(v))
	for i := 0; i < len(v); i++ {
		ret[i] = int64(v[i])
		// Cap the value to max int64. Unsigned values >= 2^63 are converted to negative values.
		if ret[i] < 0 {
			ret[i] = math.MaxInt64
		}
	}
	return ret
}
