// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
)

// AttributeDirection specifies the a value direction attribute.
type AttributeDirection int

const (
	_ AttributeDirection = iota
	AttributeDirectionTx
	AttributeDirectionRx
)

// String returns the string representation of the AttributeDirection.
func (av AttributeDirection) String() string {
	switch av {
	case AttributeDirectionTx:
		return "tx"
	case AttributeDirectionRx:
		return "rx"
	}
	return ""
}

// MapAttributeDirection is a helper map of string to AttributeDirection attribute value.
var MapAttributeDirection = map[string]AttributeDirection{
	"tx": AttributeDirectionTx,
	"rx": AttributeDirectionRx,
}

// AttributeMemoryState specifies the a value memory_state attribute.
type AttributeMemoryState int

const (
	_ AttributeMemoryState = iota
	AttributeMemoryStateUsed
	AttributeMemoryStateFree
)

// String returns the string representation of the AttributeMemoryState.
func (av AttributeMemoryState) String() string {
	switch av {
	case AttributeMemoryStateUsed:
		return "used"
	case AttributeMemoryStateFree:
		return "free"
	}
	return ""
}

// MapAttributeMemoryState is a helper map of string to AttributeMemoryState attribute value.
var MapAttributeMemoryState = map[string]AttributeMemoryState{
	"used": AttributeMemoryStateUsed,
	"free": AttributeMemoryStateFree,
}

// AttributePipe specifies the a value pipe attribute.
type AttributePipe int

const (
	_ AttributePipe = iota
	AttributePipeTensor
	AttributePipeFp64
	AttributePipeFp32
	AttributePipeFp16
)

// String returns the string representation of the AttributePipe.
func (av AttributePipe) String() string {
	switch av {
	case AttributePipeTensor:
		return "tensor"
	case AttributePipeFp64:
		return "fp64"
	case AttributePipeFp32:
		return "fp32"
	case AttributePipeFp16:
		return "fp16"
	}
	return ""
}

// MapAttributePipe is a helper map of string to AttributePipe attribute value.
var MapAttributePipe = map[string]AttributePipe{
	"tensor": AttributePipeTensor,
	"fp64":   AttributePipeFp64,
	"fp32":   AttributePipeFp32,
	"fp16":   AttributePipeFp16,
}

type metricDcgmGpuMemoryBytesUsed struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.memory.bytes_used metric with initial data.
func (m *metricDcgmGpuMemoryBytesUsed) init() {
	m.data.SetName("dcgm.gpu.memory.bytes_used")
	m.data.SetDescription("Current number of GPU memory bytes used by state. Summing the values of all states yields the total GPU memory space.")
	m.data.SetUnit("By")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuMemoryBytesUsed) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, memoryStateAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
	dp.Attributes().PutStr("memory_state", memoryStateAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuMemoryBytesUsed) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuMemoryBytesUsed) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuMemoryBytesUsed(cfg MetricConfig) metricDcgmGpuMemoryBytesUsed {
	m := metricDcgmGpuMemoryBytesUsed{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuProfilingDramUtilization struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.profiling.dram_utilization metric with initial data.
func (m *metricDcgmGpuProfilingDramUtilization) init() {
	m.data.SetName("dcgm.gpu.profiling.dram_utilization")
	m.data.SetDescription("Fraction of cycles data was being sent or received from GPU memory.")
	m.data.SetUnit("1")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuProfilingDramUtilization) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetDoubleValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuProfilingDramUtilization) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuProfilingDramUtilization) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuProfilingDramUtilization(cfg MetricConfig) metricDcgmGpuProfilingDramUtilization {
	m := metricDcgmGpuProfilingDramUtilization{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuProfilingNvlinkTrafficRate struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.profiling.nvlink_traffic_rate metric with initial data.
func (m *metricDcgmGpuProfilingNvlinkTrafficRate) init() {
	m.data.SetName("dcgm.gpu.profiling.nvlink_traffic_rate")
	m.data.SetDescription("The average rate of bytes received from the GPU over NVLink over the sample period, not including protocol headers.")
	m.data.SetUnit("By/s")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuProfilingNvlinkTrafficRate) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuProfilingNvlinkTrafficRate) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuProfilingNvlinkTrafficRate) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuProfilingNvlinkTrafficRate(cfg MetricConfig) metricDcgmGpuProfilingNvlinkTrafficRate {
	m := metricDcgmGpuProfilingNvlinkTrafficRate{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuProfilingPcieTrafficRate struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.profiling.pcie_traffic_rate metric with initial data.
func (m *metricDcgmGpuProfilingPcieTrafficRate) init() {
	m.data.SetName("dcgm.gpu.profiling.pcie_traffic_rate")
	m.data.SetDescription("The average rate of bytes sent from the GPU over the PCIe bus over the sample period, including both protocol headers and data payloads.")
	m.data.SetUnit("By/s")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuProfilingPcieTrafficRate) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val int64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, directionAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetIntValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
	dp.Attributes().PutStr("direction", directionAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuProfilingPcieTrafficRate) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuProfilingPcieTrafficRate) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuProfilingPcieTrafficRate(cfg MetricConfig) metricDcgmGpuProfilingPcieTrafficRate {
	m := metricDcgmGpuProfilingPcieTrafficRate{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuProfilingPipeUtilization struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.profiling.pipe_utilization metric with initial data.
func (m *metricDcgmGpuProfilingPipeUtilization) init() {
	m.data.SetName("dcgm.gpu.profiling.pipe_utilization")
	m.data.SetDescription("Fraction of cycles the corresponding GPU pipe was active, averaged over time and all multiprocessors.")
	m.data.SetUnit("1")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuProfilingPipeUtilization) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, pipeAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetDoubleValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
	dp.Attributes().PutStr("pipe", pipeAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuProfilingPipeUtilization) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuProfilingPipeUtilization) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuProfilingPipeUtilization(cfg MetricConfig) metricDcgmGpuProfilingPipeUtilization {
	m := metricDcgmGpuProfilingPipeUtilization{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuProfilingSmOccupancy struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.profiling.sm_occupancy metric with initial data.
func (m *metricDcgmGpuProfilingSmOccupancy) init() {
	m.data.SetName("dcgm.gpu.profiling.sm_occupancy")
	m.data.SetDescription("Fraction of resident warps on a multiprocessor relative to the maximum number supported, averaged over time and all multiprocessors.")
	m.data.SetUnit("1")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuProfilingSmOccupancy) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetDoubleValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuProfilingSmOccupancy) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuProfilingSmOccupancy) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuProfilingSmOccupancy(cfg MetricConfig) metricDcgmGpuProfilingSmOccupancy {
	m := metricDcgmGpuProfilingSmOccupancy{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuProfilingSmUtilization struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.profiling.sm_utilization metric with initial data.
func (m *metricDcgmGpuProfilingSmUtilization) init() {
	m.data.SetName("dcgm.gpu.profiling.sm_utilization")
	m.data.SetDescription("Fraction of time at least one warp was active on a multiprocessor, averaged over all multiprocessors.")
	m.data.SetUnit("1")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuProfilingSmUtilization) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetDoubleValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuProfilingSmUtilization) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuProfilingSmUtilization) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuProfilingSmUtilization(cfg MetricConfig) metricDcgmGpuProfilingSmUtilization {
	m := metricDcgmGpuProfilingSmUtilization{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

type metricDcgmGpuUtilization struct {
	data     pmetric.Metric // data buffer for generated metric.
	config   MetricConfig   // metric config provided by user.
	capacity int            // max observed number of data points added to the metric.
}

// init fills dcgm.gpu.utilization metric with initial data.
func (m *metricDcgmGpuUtilization) init() {
	m.data.SetName("dcgm.gpu.utilization")
	m.data.SetDescription("Fraction of time the GPU was not idle.")
	m.data.SetUnit("1")
	m.data.SetEmptyGauge()
	m.data.Gauge().DataPoints().EnsureCapacity(m.capacity)
}

func (m *metricDcgmGpuUtilization) recordDataPoint(start pcommon.Timestamp, ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	if !m.config.Enabled {
		return
	}
	dp := m.data.Gauge().DataPoints().AppendEmpty()
	dp.SetStartTimestamp(start)
	dp.SetTimestamp(ts)
	dp.SetDoubleValue(val)
	dp.Attributes().PutStr("model", modelAttributeValue)
	dp.Attributes().PutStr("gpu_number", gpuNumberAttributeValue)
	dp.Attributes().PutStr("uuid", uuidAttributeValue)
}

// updateCapacity saves max length of data point slices that will be used for the slice capacity.
func (m *metricDcgmGpuUtilization) updateCapacity() {
	if m.data.Gauge().DataPoints().Len() > m.capacity {
		m.capacity = m.data.Gauge().DataPoints().Len()
	}
}

// emit appends recorded metric data to a metrics slice and prepares it for recording another set of data points.
func (m *metricDcgmGpuUtilization) emit(metrics pmetric.MetricSlice) {
	if m.config.Enabled && m.data.Gauge().DataPoints().Len() > 0 {
		m.updateCapacity()
		m.data.MoveTo(metrics.AppendEmpty())
		m.init()
	}
}

func newMetricDcgmGpuUtilization(cfg MetricConfig) metricDcgmGpuUtilization {
	m := metricDcgmGpuUtilization{config: cfg}
	if cfg.Enabled {
		m.data = pmetric.NewMetric()
		m.init()
	}
	return m
}

// MetricsBuilder provides an interface for scrapers to report metrics while taking care of all the transformations
// required to produce metric representation defined in metadata and user config.
type MetricsBuilder struct {
	config                                  MetricsBuilderConfig // config of the metrics builder.
	startTime                               pcommon.Timestamp    // start time that will be applied to all recorded data points.
	metricsCapacity                         int                  // maximum observed number of metrics per resource.
	metricsBuffer                           pmetric.Metrics      // accumulates metrics data before emitting.
	buildInfo                               component.BuildInfo  // contains version information.
	metricDcgmGpuMemoryBytesUsed            metricDcgmGpuMemoryBytesUsed
	metricDcgmGpuProfilingDramUtilization   metricDcgmGpuProfilingDramUtilization
	metricDcgmGpuProfilingNvlinkTrafficRate metricDcgmGpuProfilingNvlinkTrafficRate
	metricDcgmGpuProfilingPcieTrafficRate   metricDcgmGpuProfilingPcieTrafficRate
	metricDcgmGpuProfilingPipeUtilization   metricDcgmGpuProfilingPipeUtilization
	metricDcgmGpuProfilingSmOccupancy       metricDcgmGpuProfilingSmOccupancy
	metricDcgmGpuProfilingSmUtilization     metricDcgmGpuProfilingSmUtilization
	metricDcgmGpuUtilization                metricDcgmGpuUtilization
}

// metricBuilderOption applies changes to default metrics builder.
type metricBuilderOption func(*MetricsBuilder)

// WithStartTime sets startTime on the metrics builder.
func WithStartTime(startTime pcommon.Timestamp) metricBuilderOption {
	return func(mb *MetricsBuilder) {
		mb.startTime = startTime
	}
}

func NewMetricsBuilder(mbc MetricsBuilderConfig, settings receiver.Settings, options ...metricBuilderOption) *MetricsBuilder {
	mb := &MetricsBuilder{
		config:                                  mbc,
		startTime:                               pcommon.NewTimestampFromTime(time.Now()),
		metricsBuffer:                           pmetric.NewMetrics(),
		buildInfo:                               settings.BuildInfo,
		metricDcgmGpuMemoryBytesUsed:            newMetricDcgmGpuMemoryBytesUsed(mbc.Metrics.DcgmGpuMemoryBytesUsed),
		metricDcgmGpuProfilingDramUtilization:   newMetricDcgmGpuProfilingDramUtilization(mbc.Metrics.DcgmGpuProfilingDramUtilization),
		metricDcgmGpuProfilingNvlinkTrafficRate: newMetricDcgmGpuProfilingNvlinkTrafficRate(mbc.Metrics.DcgmGpuProfilingNvlinkTrafficRate),
		metricDcgmGpuProfilingPcieTrafficRate:   newMetricDcgmGpuProfilingPcieTrafficRate(mbc.Metrics.DcgmGpuProfilingPcieTrafficRate),
		metricDcgmGpuProfilingPipeUtilization:   newMetricDcgmGpuProfilingPipeUtilization(mbc.Metrics.DcgmGpuProfilingPipeUtilization),
		metricDcgmGpuProfilingSmOccupancy:       newMetricDcgmGpuProfilingSmOccupancy(mbc.Metrics.DcgmGpuProfilingSmOccupancy),
		metricDcgmGpuProfilingSmUtilization:     newMetricDcgmGpuProfilingSmUtilization(mbc.Metrics.DcgmGpuProfilingSmUtilization),
		metricDcgmGpuUtilization:                newMetricDcgmGpuUtilization(mbc.Metrics.DcgmGpuUtilization),
	}

	for _, op := range options {
		op(mb)
	}
	return mb
}

// updateCapacity updates max length of metrics and resource attributes that will be used for the slice capacity.
func (mb *MetricsBuilder) updateCapacity(rm pmetric.ResourceMetrics) {
	if mb.metricsCapacity < rm.ScopeMetrics().At(0).Metrics().Len() {
		mb.metricsCapacity = rm.ScopeMetrics().At(0).Metrics().Len()
	}
}

// ResourceMetricsOption applies changes to provided resource metrics.
type ResourceMetricsOption func(pmetric.ResourceMetrics)

// WithResource sets the provided resource on the emitted ResourceMetrics.
// It's recommended to use ResourceBuilder to create the resource.
func WithResource(res pcommon.Resource) ResourceMetricsOption {
	return func(rm pmetric.ResourceMetrics) {
		res.CopyTo(rm.Resource())
	}
}

// WithStartTimeOverride overrides start time for all the resource metrics data points.
// This option should be only used if different start time has to be set on metrics coming from different resources.
func WithStartTimeOverride(start pcommon.Timestamp) ResourceMetricsOption {
	return func(rm pmetric.ResourceMetrics) {
		var dps pmetric.NumberDataPointSlice
		metrics := rm.ScopeMetrics().At(0).Metrics()
		for i := 0; i < metrics.Len(); i++ {
			switch metrics.At(i).Type() {
			case pmetric.MetricTypeGauge:
				dps = metrics.At(i).Gauge().DataPoints()
			case pmetric.MetricTypeSum:
				dps = metrics.At(i).Sum().DataPoints()
			}
			for j := 0; j < dps.Len(); j++ {
				dps.At(j).SetStartTimestamp(start)
			}
		}
	}
}

// EmitForResource saves all the generated metrics under a new resource and updates the internal state to be ready for
// recording another set of data points as part of another resource. This function can be helpful when one scraper
// needs to emit metrics from several resources. Otherwise calling this function is not required,
// just `Emit` function can be called instead.
// Resource attributes should be provided as ResourceMetricsOption arguments.
func (mb *MetricsBuilder) EmitForResource(rmo ...ResourceMetricsOption) {
	rm := pmetric.NewResourceMetrics()
	ils := rm.ScopeMetrics().AppendEmpty()
	ils.Scope().SetName("github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver")
	ils.Scope().SetVersion(mb.buildInfo.Version)
	ils.Metrics().EnsureCapacity(mb.metricsCapacity)
	mb.metricDcgmGpuMemoryBytesUsed.emit(ils.Metrics())
	mb.metricDcgmGpuProfilingDramUtilization.emit(ils.Metrics())
	mb.metricDcgmGpuProfilingNvlinkTrafficRate.emit(ils.Metrics())
	mb.metricDcgmGpuProfilingPcieTrafficRate.emit(ils.Metrics())
	mb.metricDcgmGpuProfilingPipeUtilization.emit(ils.Metrics())
	mb.metricDcgmGpuProfilingSmOccupancy.emit(ils.Metrics())
	mb.metricDcgmGpuProfilingSmUtilization.emit(ils.Metrics())
	mb.metricDcgmGpuUtilization.emit(ils.Metrics())

	for _, op := range rmo {
		op(rm)
	}

	if ils.Metrics().Len() > 0 {
		mb.updateCapacity(rm)
		rm.MoveTo(mb.metricsBuffer.ResourceMetrics().AppendEmpty())
	}
}

// Emit returns all the metrics accumulated by the metrics builder and updates the internal state to be ready for
// recording another set of metrics. This function will be responsible for applying all the transformations required to
// produce metric representation defined in metadata and user config, e.g. delta or cumulative.
func (mb *MetricsBuilder) Emit(rmo ...ResourceMetricsOption) pmetric.Metrics {
	mb.EmitForResource(rmo...)
	metrics := mb.metricsBuffer
	mb.metricsBuffer = pmetric.NewMetrics()
	return metrics
}

// RecordDcgmGpuMemoryBytesUsedDataPoint adds a data point to dcgm.gpu.memory.bytes_used metric.
func (mb *MetricsBuilder) RecordDcgmGpuMemoryBytesUsedDataPoint(ts pcommon.Timestamp, val int64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, memoryStateAttributeValue AttributeMemoryState) {
	mb.metricDcgmGpuMemoryBytesUsed.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue, memoryStateAttributeValue.String())
}

// RecordDcgmGpuProfilingDramUtilizationDataPoint adds a data point to dcgm.gpu.profiling.dram_utilization metric.
func (mb *MetricsBuilder) RecordDcgmGpuProfilingDramUtilizationDataPoint(ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	mb.metricDcgmGpuProfilingDramUtilization.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue)
}

// RecordDcgmGpuProfilingNvlinkTrafficRateDataPoint adds a data point to dcgm.gpu.profiling.nvlink_traffic_rate metric.
func (mb *MetricsBuilder) RecordDcgmGpuProfilingNvlinkTrafficRateDataPoint(ts pcommon.Timestamp, val int64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricDcgmGpuProfilingNvlinkTrafficRate.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue, directionAttributeValue.String())
}

// RecordDcgmGpuProfilingPcieTrafficRateDataPoint adds a data point to dcgm.gpu.profiling.pcie_traffic_rate metric.
func (mb *MetricsBuilder) RecordDcgmGpuProfilingPcieTrafficRateDataPoint(ts pcommon.Timestamp, val int64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, directionAttributeValue AttributeDirection) {
	mb.metricDcgmGpuProfilingPcieTrafficRate.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue, directionAttributeValue.String())
}

// RecordDcgmGpuProfilingPipeUtilizationDataPoint adds a data point to dcgm.gpu.profiling.pipe_utilization metric.
func (mb *MetricsBuilder) RecordDcgmGpuProfilingPipeUtilizationDataPoint(ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string, pipeAttributeValue AttributePipe) {
	mb.metricDcgmGpuProfilingPipeUtilization.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue, pipeAttributeValue.String())
}

// RecordDcgmGpuProfilingSmOccupancyDataPoint adds a data point to dcgm.gpu.profiling.sm_occupancy metric.
func (mb *MetricsBuilder) RecordDcgmGpuProfilingSmOccupancyDataPoint(ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	mb.metricDcgmGpuProfilingSmOccupancy.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue)
}

// RecordDcgmGpuProfilingSmUtilizationDataPoint adds a data point to dcgm.gpu.profiling.sm_utilization metric.
func (mb *MetricsBuilder) RecordDcgmGpuProfilingSmUtilizationDataPoint(ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	mb.metricDcgmGpuProfilingSmUtilization.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue)
}

// RecordDcgmGpuUtilizationDataPoint adds a data point to dcgm.gpu.utilization metric.
func (mb *MetricsBuilder) RecordDcgmGpuUtilizationDataPoint(ts pcommon.Timestamp, val float64, modelAttributeValue string, gpuNumberAttributeValue string, uuidAttributeValue string) {
	mb.metricDcgmGpuUtilization.recordDataPoint(mb.startTime, ts, val, modelAttributeValue, gpuNumberAttributeValue, uuidAttributeValue)
}

// Reset resets metrics builder to its initial state. It should be used when external metrics source is restarted,
// and metrics builder should update its startTime and reset it's internal state accordingly.
func (mb *MetricsBuilder) Reset(options ...metricBuilderOption) {
	mb.startTime = pcommon.NewTimestampFromTime(time.Now())
	for _, op := range options {
		op(mb)
	}
}
