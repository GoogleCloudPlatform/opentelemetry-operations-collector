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
	"testing"
	"time"

	logging "cloud.google.com/go/logging"
	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
	logtypepb "google.golang.org/genproto/googleapis/logging/type"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createLogOperation(lvs []*scpb.LogEntry, labels map[string]string) *scpb.Operation {
	return &scpb.Operation{
		ConsumerId:    testConsumerID,
		OperationName: "log_entry",
		Labels:        labels,
		LogEntries:    lvs,
	}
}

func createSingleLogOp(lvs []*scpb.LogEntry) []*scpb.Operation {
	return []*scpb.Operation{createLogOperation(lvs, map[string]string{})}
}

func createSingleLogOpWithResourceLabels(lvs []*scpb.LogEntry, labels map[string]string) []*scpb.Operation {
	return []*scpb.Operation{createLogOperation(lvs, labels)}
}

type logData struct {
	Resource pcommon.Resource
	Logs     []plog.LogRecord
}

func logDataToPlog(data []logData) plog.Logs {
	resourceLogsMap := make(map[pcommon.Resource][]plog.LogRecord)
	for _, d := range data {
		if _, ok := resourceLogsMap[d.Resource]; !ok {
			resourceLogsMap[d.Resource] = []plog.LogRecord{}
		}
		resourceLogsMap[d.Resource] = append(resourceLogsMap[d.Resource], d.Logs...)
	}

	logs := plog.NewLogs()
	rms := logs.ResourceLogs()
	rms.EnsureCapacity(len(resourceLogsMap))

	for resource, logs := range resourceLogsMap {
		rm := rms.AppendEmpty()
		resource.CopyTo(rm.Resource())

		rm.ScopeLogs().EnsureCapacity(1)
		sm := rm.ScopeLogs().AppendEmpty()
		met := sm.LogRecords()
		met.EnsureCapacity(len(logs))
		for i, m := range logs {
			met.AppendEmpty()
			m.CopyTo(met.At(i))
		}
	}

	return logs
}

func TestLogWithoutTimestamp(t *testing.T) {
	requestTs := timestamppb.New(testLogTimestamp.AsTime())

	cfg := Config{
		ServiceName:        testServiceID,
		ConsumerProject:    testConsumerID,
		ServiceConfigID:    testServiceConfigID,
		EnableDebugHeaders: true,
		LogConfig: LogConfig{
			DefaultLogName: "default-log-name",
		},
	}
	e := NewLogsExporter(cfg, zap.NewNop(), newFakeClient(noError), componenttest.NewNopTelemetrySettings())
	log := plog.NewLogRecord()

	parsed, err := e.logMapper.parseLogEntry(log, testLogTime)
	require.NoError(t, err)
	expected := &scpb.LogEntry{
		Name:      "default-log-name",
		Timestamp: requestTs,
		Labels:    map[string]string{}}
	require.Equal(t, expected, parsed,
		"exporter should use process time as timestamp when the log entry does not have a timestamp or observed timestamp")
}

func TestLogsAddAndBuild(t *testing.T) {

	s, err := time.Parse(time.RFC3339, "2019-09-03T11:16:10Z")
	if err != nil {
		t.Fatalf("Cannot set the start time: %v", err)
	}
	timestamp := pcommon.NewTimestampFromTime(s)
	requestTs := timestamppb.New(timestamp.AsTime())

	tests := []struct {
		name          string
		logs          []logData
		want          []*scpb.Operation
		config        func(*Config)
		expectError   bool
		expectedError error
	}{
		{
			name: "empty log, empty monitoredresource",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "empty log, empty monitoredresource, with observerd timestamp",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.SetObservedTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "log with json, empty monitoredresource",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetEmptyMap().PutStr("this", "is json")
						log.SetObservedTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
					Payload: &scpb.LogEntry_StructPayload{StructPayload: &structpb.Struct{Fields: map[string]*structpb.Value{
						"this": {Kind: &structpb.Value_StringValue{StringValue: "is json"}},
					}}},
				},
			}),
		},
		{
			name: "log with invalid json byte body returns raw byte string",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetEmptyBytes().FromRaw([]byte(`"this is not json"`))
						log.SetObservedTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
					Payload:   &scpb.LogEntry_TextPayload{TextPayload: "InRoaXMgaXMgbm90IGpzb24i"},
				},
			}),
		},
		{
			name: "log with json and httpRequest, empty monitoredresource",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetEmptyMap().PutStr("message", "hello!")
						log.Attributes().PutEmptyBytes(HTTPRequestAttributeKey).FromRaw([]byte(`{
							"requestMethod": "GET",
							"requestURL": "https://www.example.com",
							"requestSize": "1",
							"status": "200",
							"responseSize": "1",
							"userAgent": "test",
							"remoteIP": "192.168.0.1",
							"serverIP": "192.168.0.2",
							"referer": "https://www.example2.com",
							"cacheHit": false,
							"cacheValidatedWithOriginServer": false,
							"cacheFillBytes": "1",
							"protocol": "HTTP/2"
						}`))
						log.SetObservedTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
					Payload: &scpb.LogEntry_StructPayload{StructPayload: &structpb.Struct{Fields: map[string]*structpb.Value{
						"message": {Kind: &structpb.Value_StringValue{StringValue: "hello!"}},
					}}},
					HttpRequest: &scpb.HttpRequest{
						RequestMethod:                  "GET",
						UserAgent:                      "test",
						Referer:                        "https://www.example2.com",
						RequestUrl:                     "https://www.example.com",
						Protocol:                       "HTTP/2",
						RequestSize:                    1,
						Status:                         200,
						ResponseSize:                   1,
						ServerIp:                       "192.168.0.2",
						RemoteIp:                       "192.168.0.1",
						CacheHit:                       false,
						CacheValidatedWithOriginServer: false,
						CacheFillBytes:                 1,
						CacheLookup:                    false,
					},
				},
			}),
		},
		{
			name: "log with httpRequest attribute unsupported type",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetEmptyMap().PutStr("message", "hello!")
						log.Attributes().PutBool(HTTPRequestAttributeKey, true)
						log.SetObservedTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
					Payload: &scpb.LogEntry_StructPayload{StructPayload: &structpb.Struct{Fields: map[string]*structpb.Value{
						"message": {Kind: &structpb.Value_StringValue{StringValue: "hello!"}},
					}}},
				},
			}),
		},
		{
			name: "log body with string value",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetStr("{\"message\": \"hello!\"}")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: `{"message": "hello!"}`,
					},
					Labels: map[string]string{},
				},
			}),
		},
		{
			name: "log with log name set in attributes",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(LogNameAttributeKey, "foo-log")
						log.Body().SetStr("{\"message\": \"hello!\"}")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "foo-log",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: `{"message": "hello!"}`,
					},
					Labels: map[string]string{},
				},
			}),
		},
		{
			name: "set default log name through config",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetStr("test1")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "customized-log-default-name",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test1",
					},
					Labels: map[string]string{},
				},
			}),
			config: func(c *Config) {
				c.LogConfig.DefaultLogName = "customized-log-default-name"
			},
		},
		{
			name: "set default log name through config, but log name attribute should take priority",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(LogNameAttributeKey, "foo-log")
						log.Body().SetStr("test1")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "foo-log",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test1",
					},
					Labels: map[string]string{},
				},
			}),
			config: func(c *Config) {
				c.LogConfig.DefaultLogName = "customized-log-default-name"
			},
		},
		{
			name: "set insert id from insert id attribute",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(InsertIdAttributeKey, "foo-insert-id")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					InsertId:  "foo-insert-id",
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "parse severity number correctly",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.SetSeverityNumber(18)
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Severity:  logtypepb.LogSeverity(logging.Error),
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "log with invalid severity number",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.SetSeverityNumber(100)
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			expectError: true,
		},
		{
			name: "parse severity text correctly",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.SetSeverityText("ERROR")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Severity:  logtypepb.LogSeverity(logging.Error),
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "parse severity number over severiy text",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						// 11 is NOTICE
						log.SetSeverityNumber(11)
						log.SetSeverityText("ERROR")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Severity:  logtypepb.LogSeverity(logging.Notice),
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "parse severity text over severiy number when number is invalid",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.SetSeverityNumber(0)
						log.SetSeverityText("fatal3")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Severity:  logtypepb.LogSeverity(logging.Alert),
					Labels:    map[string]string{},
				},
			}),
		},
		{
			name: "log with valid sourceLocation (bytes)",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(LogNameAttributeKey, "foo-log")
						log.Attributes().PutEmptyBytes(SourceLocationAttributeKey).FromRaw(
							[]byte(`{"file": "test.php", "line":100, "function":"helloWorld"}`),
						)
						log.Body().SetStr("test1")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "foo-log",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test1",
					},
					SourceLocation: &scpb.LogEntrySourceLocation{
						File:     "test.php",
						Line:     100,
						Function: "helloWorld",
					},
					Labels: map[string]string{},
				},
			}),
		},
		{
			name: "log with invalid sourceLocation (bytes)",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(LogNameAttributeKey, "foo-log")
						log.Attributes().PutEmptyBytes(SourceLocationAttributeKey).FromRaw(
							[]byte(`{"file": 100}`),
						)
						log.Body().SetStr("test1")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			expectError: true,
		},
		{
			name: "log with valid sourceLocation (map)",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						sourceLocationMap := log.Attributes().PutEmptyMap(SourceLocationAttributeKey)
						sourceLocationMap.PutStr("file", "test.php")
						sourceLocationMap.PutInt("line", 100)
						sourceLocationMap.PutStr("function", "helloWorld")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					SourceLocation: &scpb.LogEntrySourceLocation{
						File:     "test.php",
						Line:     100,
						Function: "helloWorld",
					},
					Labels: map[string]string{},
				},
			}),
		},
		{
			name: "log with invalid sourceLocation (map)",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						sourceLocationMap := log.Attributes().PutEmptyMap(SourceLocationAttributeKey)
						sourceLocationMap.PutStr("line", "100")
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			expectError: true,
		},
		{
			name: "log with valid sourceLocation (string)",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(
							SourceLocationAttributeKey,
							`{"file": "test.php", "line":100, "function":"helloWorld"}`,
						)
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					SourceLocation: &scpb.LogEntrySourceLocation{
						File:     "test.php",
						Line:     100,
						Function: "helloWorld",
					},
					Labels: map[string]string{},
				},
			}),
		},
		{
			name: "log with invalid sourceLocation (string)",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutStr(
							SourceLocationAttributeKey,
							`{"file": 100}`,
						)
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			expectError: true,
		},
		{
			name: "log with unsupported sourceLocation type",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Attributes().PutBool(SourceLocationAttributeKey, true)
						log.SetTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: emptyResource(),
				}},
			expectError: true,
			expectedError: &attributeProcessingError{
				Key: SourceLocationAttributeKey,
				Err: &unsupportedValueTypeError{ValueType: pcommon.ValueTypeBool},
			},
		},
		{
			name: "two logs",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log1 := plog.NewLogRecord()
						log1.Attributes().PutStr(LogNameAttributeKey, "foo-log-1")
						log1.Body().SetStr("test1")
						log1.SetTimestamp(timestamp)
						log2 := plog.NewLogRecord()
						log2.Attributes().PutStr(LogNameAttributeKey, "foo-log-2")
						log2.Body().SetStr("test2")
						log2.SetTimestamp(timestamp)
						return []plog.LogRecord{log1, log2}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "foo-log-1",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test1",
					},
					Labels: map[string]string{},
				},
				{
					Name:      "foo-log-2",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test2",
					},
					Labels: map[string]string{},
				},
			}),
		},
		{
			name: "two logs with log labels",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log1 := plog.NewLogRecord()
						log1.Attributes().PutStr(LogNameAttributeKey, "foo-log-1")
						log1.Body().SetStr("test1")
						log1.SetTimestamp(timestamp)
						log1.Attributes().PutStr("log-label1", "foo-label-1")
						log1.Attributes().PutStr("log-label2", "foo-label-2")
						log2 := plog.NewLogRecord()
						log2.Attributes().PutStr(LogNameAttributeKey, "foo-log-2")
						log2.Body().SetStr("test2")
						log2.SetTimestamp(timestamp)
						log2.Attributes().PutStr("log-label2", "foo-label-2")
						return []plog.LogRecord{log1, log2}
					}(),
					Resource: emptyResource(),
				}},
			want: createSingleLogOp([]*scpb.LogEntry{
				{
					Name:      "foo-log-1",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test1",
					},
					Labels: map[string]string{
						"log-label1": "foo-label-1",
						"log-label2": "foo-label-2",
					},
				},
				{
					Name:      "foo-log-2",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test2",
					},
					Labels: map[string]string{
						"log-label2": "foo-label-2",
					},
				},
			}),
		},
		{
			name: "log with json, sample monitoredresource",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetEmptyMap().PutStr("this", "is json")
						log.SetObservedTimestamp(timestamp)
						return []plog.LogRecord{log}
					}(),
					Resource: sampleResource(),
				}},
			want: createSingleLogOpWithResourceLabels([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels:    map[string]string{},
					Payload: &scpb.LogEntry_StructPayload{StructPayload: &structpb.Struct{Fields: map[string]*structpb.Value{
						"this": {Kind: &structpb.Value_StringValue{StringValue: "is json"}},
					}}},
				},
			},
				map[string]string{
					testServiceConfigIdKey: testServiceConfigID,
					testServiceKey:         testServiceID,
					testProjectIdKey:       testConsumerID,
				}),
		},
		{
			name: "log with json, sample monitoredresource and labels",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log := plog.NewLogRecord()
						log.Body().SetEmptyMap().PutStr("this", "is json")
						log.SetObservedTimestamp(timestamp)
						log.Attributes().PutStr("log-label1", "foo-label-1")
						log.Attributes().PutStr(testServiceConfigIdKey, testServiceConfigID)
						return []plog.LogRecord{log}
					}(),
					Resource: sampleResource(),
				}},
			want: createSingleLogOpWithResourceLabels([]*scpb.LogEntry{
				{
					Name:      "default-log-name",
					Timestamp: requestTs,
					Labels: map[string]string{
						"log-label1":           "foo-label-1",
						testServiceConfigIdKey: testServiceConfigID,
					},
					Payload: &scpb.LogEntry_StructPayload{StructPayload: &structpb.Struct{Fields: map[string]*structpb.Value{
						"this": {Kind: &structpb.Value_StringValue{StringValue: "is json"}},
					}}},
				},
			},
				map[string]string{
					testServiceConfigIdKey: testServiceConfigID,
					testServiceKey:         testServiceID,
					testProjectIdKey:       testConsumerID,
				}),
		},
		{
			name: "two logs with same monitored resource",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log1 := plog.NewLogRecord()
						log1.Attributes().PutStr(LogNameAttributeKey, "foo-log-1")
						log1.Body().SetStr("test1")
						log1.SetTimestamp(timestamp)
						log2 := plog.NewLogRecord()
						log2.Attributes().PutStr(LogNameAttributeKey, "foo-log-2")
						log2.Body().SetStr("test2")
						log2.SetTimestamp(timestamp)
						return []plog.LogRecord{log1, log2}
					}(),
					Resource: sampleResource(),
				}},
			want: createSingleLogOpWithResourceLabels([]*scpb.LogEntry{
				{
					Name:      "foo-log-1",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test1",
					},
					Labels: map[string]string{},
				},
				{
					Name:      "foo-log-2",
					Timestamp: requestTs,
					Payload: &scpb.LogEntry_TextPayload{
						TextPayload: "test2",
					},
					Labels: map[string]string{},
				},
			},
				map[string]string{
					testServiceConfigIdKey: testServiceConfigID,
					testServiceKey:         testServiceID,
					testProjectIdKey:       testConsumerID,
				}),
		},
		{
			name: "three logs with different monitored resource",
			logs: []logData{
				logData{
					Logs: func() []plog.LogRecord {
						log1 := plog.NewLogRecord()
						log1.Attributes().PutStr(LogNameAttributeKey, "foo-log-1")
						log1.Body().SetStr("test1")
						log1.SetTimestamp(timestamp)
						log2 := plog.NewLogRecord()
						log2.Attributes().PutStr(LogNameAttributeKey, "foo-log-2")
						log2.Body().SetStr("test2")
						log2.SetTimestamp(timestamp)
						return []plog.LogRecord{log1, log2}
					}(),
					Resource: sampleResource(),
				},
				logData{
					Logs: func() []plog.LogRecord {
						log1 := plog.NewLogRecord()
						log1.Attributes().PutStr(LogNameAttributeKey, "foo-log-1")
						log1.Body().SetStr("test1")
						log1.SetTimestamp(timestamp)
						return []plog.LogRecord{log1}
					}(),
					Resource: emptyResource(),
				},
			},
			want: append(
				createSingleLogOpWithResourceLabels([]*scpb.LogEntry{
					{
						Name:      "foo-log-1",
						Timestamp: requestTs,
						Payload: &scpb.LogEntry_TextPayload{
							TextPayload: "test1",
						},
						Labels: map[string]string{},
					},
					{
						Name:      "foo-log-2",
						Timestamp: requestTs,
						Payload: &scpb.LogEntry_TextPayload{
							TextPayload: "test2",
						},
						Labels: map[string]string{},
					},
				},
					map[string]string{
						testServiceConfigIdKey: testServiceConfigID,
						testServiceKey:         testServiceID,
						testProjectIdKey:       testConsumerID,
					}),
				createSingleLogOp([]*scpb.LogEntry{
					{
						Name:      "foo-log-1",
						Timestamp: requestTs,
						Payload: &scpb.LogEntry_TextPayload{
							TextPayload: "test1",
						},
						Labels: map[string]string{},
					},
				})...,
			),
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
				LogConfig: LogConfig{
					DefaultLogName: "default-log-name",
					OperationName:  "log_entry",
				},
			}
			if tc.config != nil {
				tc.config(&cfg)
			}
			e := NewLogsExporter(cfg, zap.NewNop(), c, componenttest.NewNopTelemetrySettings())

			err := e.ConsumeLogs(context.Background(), logDataToPlog(tc.logs))

			if tc.expectError {
				assert.NotNil(t, err)
				if tc.expectedError != nil {
					assert.Equal(t, tc.expectedError.Error(), err.Error())
				}
			} else {
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
			}
		})
	}
}
