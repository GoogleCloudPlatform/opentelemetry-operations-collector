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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/pborman/uuid"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
	logtypepb "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultMaxEntrySize = 256 * 1024 // 256 KB
	LogNameAttributeKey = "log_name"
	// TODO: we should evaluate if this is needed; if not, can remove this and
	// let the API generate a UUID - same behavior as the Cloud Logging exporter
	InsertIdAttributeKey = "logging.googleapis.com/insertId"
	// FluentBit exporter uses logging.googleapis.com/* prefix and Cloud Logging
	// exporter uses `gcp.*` prefix
	SourceLocationAttributeKey = "logging.googleapis.com/sourceLocation"
	HTTPRequestAttributeKey    = "httpRequest"
	LogDefaultOperationName    = "log_entry"
)

// severityMapping maps the integer severity level values from OTel [0-24]
// to matching Cloud Logging severity levels.
// Service Control' severity uses logtypepb's severity levels, so this mapping
// is exactly the same as Cloud Logging exporter's severity mapping.
var severityMapping = []logtypepb.LogSeverity{
	logtypepb.LogSeverity_DEFAULT,   // Default, 0
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     //
	logtypepb.LogSeverity_DEBUG,     // 1-8 -> Debug
	logtypepb.LogSeverity_INFO,      //
	logtypepb.LogSeverity_INFO,      // 9-10 -> Info
	logtypepb.LogSeverity_NOTICE,    //
	logtypepb.LogSeverity_NOTICE,    // 11-12 -> Notice
	logtypepb.LogSeverity_WARNING,   //
	logtypepb.LogSeverity_WARNING,   //
	logtypepb.LogSeverity_WARNING,   //
	logtypepb.LogSeverity_WARNING,   // 13-16 -> Warning
	logtypepb.LogSeverity_ERROR,     //
	logtypepb.LogSeverity_ERROR,     //
	logtypepb.LogSeverity_ERROR,     //
	logtypepb.LogSeverity_ERROR,     // 17-20 -> Error
	logtypepb.LogSeverity_CRITICAL,  //
	logtypepb.LogSeverity_CRITICAL,  // 21-22 -> Critical
	logtypepb.LogSeverity_ALERT,     // 23 -> Alert
	logtypepb.LogSeverity_EMERGENCY, // 24 -> Emergency
}

// otelSeverityForText maps the generic aliases of SeverityTexts to SeverityNumbers.
// This can be useful if SeverityText is manually set to one of the values from the data
// model in a way that doesn't automatically parse the SeverityNumber as well
// (see https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/issues/442)
// Otherwise, this is the mapping that is automatically used by the Stanza log severity parser
// (https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.54.0/pkg/stanza/operator/helper/severity_builder.go#L34-L57)
var otelSeverityForText = map[string]plog.SeverityNumber{
	"trace":  plog.SeverityNumberTrace,
	"trace2": plog.SeverityNumberTrace2,
	"trace3": plog.SeverityNumberTrace3,
	"trace4": plog.SeverityNumberTrace4,
	"debug":  plog.SeverityNumberDebug,
	"debug2": plog.SeverityNumberDebug2,
	"debug3": plog.SeverityNumberDebug3,
	"debug4": plog.SeverityNumberDebug4,
	"info":   plog.SeverityNumberInfo,
	"info2":  plog.SeverityNumberInfo2,
	"info3":  plog.SeverityNumberInfo3,
	"info4":  plog.SeverityNumberInfo4,
	"warn":   plog.SeverityNumberWarn,
	"warn2":  plog.SeverityNumberWarn2,
	"warn3":  plog.SeverityNumberWarn3,
	"warn4":  plog.SeverityNumberWarn4,
	"error":  plog.SeverityNumberError,
	"error2": plog.SeverityNumberError2,
	"error3": plog.SeverityNumberError3,
	"error4": plog.SeverityNumberError4,
	"fatal":  plog.SeverityNumberFatal,
	"fatal2": plog.SeverityNumberFatal2,
	"fatal3": plog.SeverityNumberFatal3,
	"fatal4": plog.SeverityNumberFatal4,
}

type attributeProcessingError struct {
	Err error
	Key string
}

func (e *attributeProcessingError) Error() string {
	return fmt.Sprintf("could not process attribute %s: %s", e.Key, e.Err.Error())
}

type unsupportedValueTypeError struct {
	ValueType pcommon.ValueType
}

func (e *unsupportedValueTypeError) Error() string {
	return fmt.Sprintf("unsupported value type %v", e.ValueType)
}

type LogsExporter struct {
	*Exporter
	logMapper logMapper
}

type logMapper struct {
	logger       *zap.SugaredLogger
	cfg          Config
	maxEntrySize int
}

// NewLogsExporter returns service control logs exporter
func NewLogsExporter(config Config, logger *zap.Logger, c ServiceControlClient, tel component.TelemetrySettings) *LogsExporter {
	e := newExporter(config, logger, c, tel)
	return &LogsExporter{
		Exporter: e,
		logMapper: logMapper{
			logger:       logger.Sugar(),
			cfg:          config,
			maxEntrySize: defaultMaxEntrySize,
		},
	}
}

func (e *LogsExporter) ConsumeLogs(ctx context.Context, ld plog.Logs) error {
	req, err := e.createReportRequest(ld)
	if err != nil {
		return err
	}
	if len(req.Operations) == 0 {
		// Nothing to export.
		return nil
	}
	return e.pushReportRequest(ctx, req)
}

func (e *LogsExporter) createReportRequest(ld plog.Logs) (*scpb.ReportRequest, error) {
	now := time.Now()
	request := scpb.ReportRequest{
		Operations:      make([]*scpb.Operation, 0),
		ServiceConfigId: e.serviceConfigID,
		ServiceName:     e.serviceName,
	}

	//TODO: in FluentBit, the exporter would create a MR to log entries map, and
	// create one operation to contain all log entries. This could be problematic
	// if there are too many logs, either within a same MR, or across all MRs.
	// Instead, we should look into split them into separate requests
	for i := range ld.ResourceLogs().Len() {
		rl := ld.ResourceLogs().At(i)
		ops, err := e.createRequestOperation(rl, now)
		if err != nil {
			return nil, err
		}
		request.Operations = append(request.Operations, ops)
	}

	return &request, nil
}

func (e *LogsExporter) createRequestOperation(rl plog.ResourceLogs, now time.Time) (*scpb.Operation, error) {
	le, mr, consumerId, err := e.createEntries(rl)
	if err != nil {
		return nil, err
	}

	op := scpb.Operation{
		ConsumerId:    consumerId,
		OperationName: e.logMapper.cfg.LogConfig.OperationName,
		// Ensure start_time < end_time:
		// https://yaqs.corp.google.com/eng/q/5422158029493633024.
		// Keep start_time = now - 1ms.
		StartTime:   timestamppb.New(now.Add(-1 * time.Second)),
		EndTime:     timestamppb.New(now),
		OperationId: uuid.New(),
		Labels:      mr,
		LogEntries:  le,
	}
	return &op, nil
}

func (e *LogsExporter) createEntries(rl plog.ResourceLogs) ([]*scpb.LogEntry, map[string]string, string, error) {
	var errs []error
	entries := make([]*scpb.LogEntry, 0)
	processTime := time.Now()
	resourceAttributes, consumerId := e.parseResourceAttributes(rl.Resource())
	for j := range rl.ScopeLogs().Len() {
		sl := rl.ScopeLogs().At(j)
		// TODO: handle otel instrumentation scope labels, i.e., instrumentation_source
		// and instrumentation_version
		for k := range sl.LogRecords().Len() {
			logRecord := sl.LogRecords().At(k)
			entry, err := e.logMapper.parseLogEntry(logRecord, processTime)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			entries = append(entries, entry)
		}
	}

	return entries, resourceAttributes, consumerId, errors.Join(errs...)
}

func (l logMapper) getLogName(log plog.LogRecord) (string, error) {
	logNameAttr, exists := log.Attributes().Get(LogNameAttributeKey)
	if exists {
		return logNameAttr.AsString(), nil
	}
	if len(l.cfg.LogConfig.DefaultLogName) > 0 {
		return l.cfg.LogConfig.DefaultLogName, nil
	}
	return "", fmt.Errorf("encountered log without 'log_name' field, while 'default_log_name' value in configuration is empty")
}

// parseLogEntry creates a Service Control LogEntry from otel LogRecord. Service
// Control API does not support log splits, so one otel log will always be one
// log entry.
func (l logMapper) parseLogEntry(logRecord plog.LogRecord, processTime time.Time) (*scpb.LogEntry, error) {
	ts := logRecord.Timestamp().AsTime()
	if logRecord.Timestamp() == 0 || ts.IsZero() {
		// if timestamp is unset, fall back to observed_time_unix_nano as recommended
		//   (see https://github.com/open-telemetry/opentelemetry-proto/blob/4abbb78/opentelemetry/proto/logs/v1/logs.proto#L176-L179)
		if logRecord.ObservedTimestamp() != 0 {
			ts = logRecord.ObservedTimestamp().AsTime()
		} else {
			// if observed_time is 0, use the process time, which is the current time
			ts = processTime
		}
	}

	logName, err := l.getLogName(logRecord)
	if err != nil {
		return nil, err
	}

	entry := &scpb.LogEntry{
		Name:      logName,
		Timestamp: timestamppb.New(ts),
		Labels:    map[string]string{},
	}

	// build our own map off OTel attributes so we don't have to call .Get() for each special case
	// (.Get() ranges over all attributes each time)
	attrsMap := make(map[string]pcommon.Value)
	logRecord.Attributes().Range(func(k string, v pcommon.Value) bool {
		attrsMap[k] = v
		return true
	})

	// Parse LogEntry InsertId struct from OTel attribute
	// TODO: we should evaluate if this is needed; if not, can remove this and
	// let the API generate a UUID - same behavior as the Cloud Logging exporter
	if insertIdAttr, ok := attrsMap[InsertIdAttributeKey]; ok {
		entry.InsertId = insertIdAttr.AsString()
		delete(attrsMap, InsertIdAttributeKey)
	}
	// FluentBit would generate UUIDs in the exporter; here we would let server
	// assign UUIDs

	// parse LogEntrySourceLocation struct from OTel attribute
	if sourceLocation, ok := attrsMap[SourceLocationAttributeKey]; ok {
		var logEntrySourceLocation scpb.LogEntrySourceLocation
		err := unmarshalAttribute(sourceLocation, &logEntrySourceLocation)
		if err != nil {
			return nil, &attributeProcessingError{Key: SourceLocationAttributeKey, Err: err}
		}
		entry.SourceLocation = &logEntrySourceLocation
		delete(attrsMap, SourceLocationAttributeKey)
	}

	// parse HttpRequest
	if httpRequestAttr, ok := attrsMap[HTTPRequestAttributeKey]; ok {
		httpRequest, err := l.parseHTTPRequest(httpRequestAttr)
		if err != nil {
			l.logger.Debug("Unable to parse httpRequest", zap.Error(err))
		}
		entry.HttpRequest = httpRequest
		delete(attrsMap, HTTPRequestAttributeKey)
	}

	// parse Severity
	if logRecord.SeverityNumber() < 0 || int(logRecord.SeverityNumber()) > len(severityMapping)-1 {
		return nil, fmt.Errorf("unknown SeverityNumber %v", logRecord.SeverityNumber())
	}
	severityNumber := logRecord.SeverityNumber()
	// Log severity levels are based on numerical values defined by Otel/GCP, which are informally mapped to generic text values such as "ALERT", "Debug", etc.
	// In some cases, a SeverityText value can be automatically mapped to a matching SeverityNumber.
	// If not (for example, when directly setting the SeverityText on a Log entry with the Transform processor), then the
	// SeverityText might be something like "ALERT" while the SeverityNumber is still "0".
	// In this case, we will attempt to map the text ourselves to one of the defined Otel SeverityNumbers.
	// We do this by checking that the SeverityText is NOT "default" (ie, it exists in our map) and that the SeverityNumber IS "0".
	// (This also excludes other unknown/custom severity text values, which may have user-defined mappings in the collector)
	if severityForText, ok := otelSeverityForText[strings.ToLower(logRecord.SeverityText())]; ok && severityNumber == 0 {
		severityNumber = severityForText
	}
	entry.Severity = severityMapping[severityNumber]

	// parse remaining OTel attributes to GCP labels
	for k, v := range attrsMap {
		if k == LogNameAttributeKey {
			continue
		}
		if _, ok := entry.Labels[k]; !ok {
			entry.Labels[k] = v.AsString()
		}
	}

	// Handle map and bytes as JSON-structured logs if they are successfully converted.
	switch logRecord.Body().Type() {
	case pcommon.ValueTypeMap:
		s, err := structpb.NewStruct(logRecord.Body().Map().AsRaw())
		if err == nil {
			entry.Payload = &scpb.LogEntry_StructPayload{StructPayload: s}
			return entry, nil
		}
		l.logger.Warn(fmt.Sprintf("map body cannot be converted to a json payload, exporting as raw string: %+v", err))
	case pcommon.ValueTypeBytes:
		s, err := toProtoStruct(logRecord.Body().Bytes().AsRaw())
		if err == nil {
			entry.Payload = &scpb.LogEntry_StructPayload{StructPayload: s}
			return entry, nil
		}
		l.logger.Debug(fmt.Sprintf("bytes body cannot be converted to a json payload, exporting as base64 string: %+v", err))
	}

	// Fields: LogEntry.trace, LogEntry.operation, LogEntry.protoPayload
	// are not parsed
	// Service Control LogEntry does not contain: traceId, SpanId, traceSampled

	logBodyString := logRecord.Body().AsString()
	if len(logBodyString) == 0 {
		return entry, nil
	}

	// Service Control LogEntry representation does not support
	// splits. In FluentBit, long log entries are dropped.
	overheadBytes := proto.Size(entry)
	if (len([]byte(logBodyString)) + overheadBytes) > l.maxEntrySize {
		return nil, fmt.Errorf("entry size is too big: got: %d bytes, want: < %d bytes; timestamp: %s",
			len([]byte(logBodyString))+overheadBytes,
			l.maxEntrySize,
			entry.Timestamp)
	}
	entry.Payload = &scpb.LogEntry_TextPayload{TextPayload: logBodyString}

	return entry, nil
}

// JSON keys derived from:
// https://cloud.google.com/service-infrastructure/docs/service-control/reference/rest/v1/Operation#httprequest
type httpRequestLog struct {
	RemoteIP                       string `json:"remoteIp"`
	RequestURL                     string `json:"requestUrl"`
	Latency                        string `json:"latency"`
	Referer                        string `json:"referer"`
	ServerIP                       string `json:"serverIp"`
	UserAgent                      string `json:"userAgent"`
	RequestMethod                  string `json:"requestMethod"`
	Protocol                       string `json:"protocol"`
	ResponseSize                   int64  `json:"responseSize,string"`
	RequestSize                    int64  `json:"requestSize,string"`
	CacheFillBytes                 int64  `json:"cacheFillBytes,string"`
	Status                         int32  `json:"status,string"`
	CacheLookup                    bool   `json:"cacheLookup"`
	CacheHit                       bool   `json:"cacheHit"`
	CacheValidatedWithOriginServer bool   `json:"cacheValidatedWithOriginServer"`
}

func (l logMapper) parseHTTPRequest(httpRequestAttr pcommon.Value) (*scpb.HttpRequest, error) {
	var parsedHTTPRequest httpRequestLog
	err := unmarshalAttribute(httpRequestAttr, &parsedHTTPRequest)
	if err != nil {
		return nil, &attributeProcessingError{Key: HTTPRequestAttributeKey, Err: err}
	}

	pb := &scpb.HttpRequest{
		RequestMethod:                  parsedHTTPRequest.RequestMethod,
		RequestUrl:                     fixUTF8(parsedHTTPRequest.RequestURL),
		RequestSize:                    parsedHTTPRequest.RequestSize,
		Status:                         parsedHTTPRequest.Status,
		ResponseSize:                   parsedHTTPRequest.ResponseSize,
		UserAgent:                      parsedHTTPRequest.UserAgent,
		ServerIp:                       parsedHTTPRequest.ServerIP,
		RemoteIp:                       parsedHTTPRequest.RemoteIP,
		Referer:                        parsedHTTPRequest.Referer,
		CacheHit:                       parsedHTTPRequest.CacheHit,
		CacheValidatedWithOriginServer: parsedHTTPRequest.CacheValidatedWithOriginServer,
		Protocol:                       parsedHTTPRequest.Protocol,
		CacheFillBytes:                 parsedHTTPRequest.CacheFillBytes,
		CacheLookup:                    parsedHTTPRequest.CacheLookup,
	}
	if parsedHTTPRequest.Latency != "" {
		latency, err := time.ParseDuration(parsedHTTPRequest.Latency)
		if err == nil && latency != 0 {
			pb.Latency = durationpb.New(latency)
		}
	}
	return pb, nil
}

// toProtoStruct converts v, which must marshal into a JSON object,
// into a Google Struct proto.
// Mostly copied from
// https://github.com/googleapis/google-cloud-go/blob/69705144832c715cf23832602ad9338b911dff9a/logging/logging.go#L577
func toProtoStruct(v any) (*structpb.Struct, error) {
	// v is a Go value that supports JSON marshaling. We want a Struct
	// protobuf. Some day we may have a more direct way to get there, but right
	// now the only way is to marshal the Go value to JSON, unmarshal into a
	// map, and then build the Struct proto from the map.
	jb, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("logging: json.Marshal: %w", err)
	}
	var m map[string]any
	err = json.Unmarshal(jb, &m)
	if err != nil {
		return nil, fmt.Errorf("logging: json.Unmarshal: %w", err)
	}
	return structpb.NewStruct(m)
}

// fixUTF8 is a helper that fixes an invalid UTF-8 string by replacing
// invalid UTF-8 runes with the Unicode replacement character (U+FFFD).
// See Issue https://github.com/googleapis/google-cloud-go/issues/1383.
// Coped from https://github.com/googleapis/google-cloud-go/blob/69705144832c715cf23832602ad9338b911dff9a/logging/logging.go#L557
func fixUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	// Otherwise time to build the sequence.
	buf := new(bytes.Buffer)
	buf.Grow(len(s))
	for _, r := range s {
		if utf8.ValidRune(r) {
			buf.WriteRune(r)
		} else {
			buf.WriteRune('\uFFFD')
		}
	}
	return buf.String()
}

func unmarshalAttribute(v pcommon.Value, out any) error {
	var valueBytes []byte
	switch v.Type() {
	case pcommon.ValueTypeBytes:
		valueBytes = v.Bytes().AsRaw()
	case pcommon.ValueTypeMap, pcommon.ValueTypeStr:
		valueBytes = []byte(v.AsString())
	default:
		return &unsupportedValueTypeError{ValueType: v.Type()}
	}
	// TODO: Investigate doing this without the JSON unmarshal. Getting the attribute as a map
	// instead of a slice of bytes could do, but would need a lot of type casting and checking
	// assertions with it.
	return json.Unmarshal(valueBytes, out)
}
