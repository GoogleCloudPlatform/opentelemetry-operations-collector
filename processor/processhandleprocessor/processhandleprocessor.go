package processhandleprocessor

import (
	"context"
	"errors"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

const (
	handleMetricName        = "process.memory.handles"
	handleMetricDescription = "The handles held by the process."

	hostmetricsProcessScope = "otelcol/hostmetricsreceiver/process"
)

type ProcessHandleProcessor struct {
	logger *zap.Logger
	cfg    *Config
}

// TODO: Might not need this
func newProcessHandleProcessor(logger *zap.Logger, cfg *Config) *ProcessHandleProcessor {
	return &ProcessHandleProcessor{
		logger: logger,
		cfg:    cfg,
	}
}

// ProcessMetrics is the callback the engine calls when this processor is configured in a pipeline.
// It is designed to sit immediately after a hostmetrics receiver with process metrics enabled.
// It will go through all the metrics it's received, and if it finds a scope with the name
// "otel/hostmetrics/process" then it will add a new metric to that scope with the process'
// handle count.
func (p *ProcessHandleProcessor) ProcessMetrics(_ context.Context, metrics pmetric.Metrics) (pmetric.Metrics, error) {
	var processHandleCountMap HandleCountMap

	allResourceMetrics := metrics.ResourceMetrics()
	for i := 0; i < allResourceMetrics.Len(); i++ {
		resourceMetric := allResourceMetrics.At(i)

		// Check if the scope metrics are the scope "otel/hostmetrics/process"
		// and get the scope metrics if they are.
		rsm := resourceMetric.ScopeMetrics()
		smPtr := getHostmetricsProcessScopeMetrics(rsm)
		if smPtr == nil {
			continue
		}
		sm := *smPtr

		// Get every process' handle counts. Do the syscall here so that we
		// only make the call once we have at least one set of process metrics
		// in the current overall batch.
		if len(processHandleCountMap) == 0 {
			var err error
			processHandleCountMap, err = NewHandleCountMap()
			if err != nil {
				return metrics, err
			}
		}

		// Find the process ID for this resource metric.
		pid, pidFound := findPidResourceAttribute(resourceMetric.Resource())
		if !pidFound {
			continue
		}

		m := sm.Metrics()

		// Find the start and current timestamp for the metrics in this scope
		// so that our new metric that we add matches the rest.
		startTimestamp, timestamp, tsFound := findScopeTimestamps(m)
		if !tsFound {
			return metrics, errors.New("cannot determine start timestamp")
		}

		// If the pid has a handle count from the syscall, create the metric for it
		// and add it to this scope.
		if handleCount, ok := processHandleCountMap[pid]; ok {
			addHandlesMetric(m, pid, int64(handleCount), startTimestamp, timestamp)
		}
	}

	return metrics, nil
}

func getHostmetricsProcessScopeMetrics(smSlice pmetric.ScopeMetricsSlice) *pmetric.ScopeMetrics {
	// Hostmetrics Process scraper builds the scope in a very particular way. There will only be exactly
	// one of these scopes in a ResourceMetric if it's there, since the ResourceMetric is designed
	// to represent one process.
	if smSlice.Len() != 1 {
		return nil
	}
	sm := smSlice.At(0)
	if sm.Scope().Name() != hostmetricsProcessScope {
		return nil
	}
	return &sm
}

func findPidResourceAttribute(resource pcommon.Resource) (int64, bool) {
	attributeMap := resource.Attributes()
	pidVal, ok := attributeMap.Get("process.pid")
	if !ok {
		return 0, false
	}
	return pidVal.Int(), true
}

func findScopeTimestamps(metrics pmetric.MetricSlice) (startTimestamp, timestamp pcommon.Timestamp, found bool) {
	// Loop through all the metrics in this scope. If we find any with datapoints we can use, we take the
	// start timestamp and current timestamp and treat them as the representative for this scope.
	for i := 0; i < metrics.Len(); i++ {
		m := metrics.At(0)
		var dps pmetric.NumberDataPointSlice
		// As of writing, all metrics in the process scope are either Gauge or Sum.
		switch m.Type() {
		case pmetric.MetricTypeGauge:
			dps = m.Gauge().DataPoints()
		case pmetric.MetricTypeSum:
			dps = m.Sum().DataPoints()
		default:
			continue
		}
		if dps.Len() <= 0 {
			continue
		}
		startTimestamp = dps.At(0).StartTimestamp()
		timestamp = dps.At(0).Timestamp()
		found = true
		break
	}
	return startTimestamp, timestamp, found
}

func addHandlesMetric(
	m pmetric.MetricSlice,
	pid, handleCount int64,
	startTs, ts pcommon.Timestamp,
) {
	newMetric := m.AppendEmpty()
	newMetric.SetName(handleMetricName)
	newMetric.SetDescription(handleMetricDescription)
	newMetric.SetEmptyGauge()
	newDatapoint := newMetric.Gauge().DataPoints().AppendEmpty()
	newDatapoint.SetIntValue(handleCount)
	newDatapoint.SetStartTimestamp(startTs)
	newDatapoint.SetTimestamp(ts)
}
