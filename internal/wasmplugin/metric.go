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

package main

import (
	"os"
	"strconv"
	"strings"
	"unsafe"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

//go:wasmexport scrape
func scrape() uint64 {
	seconds, err := getUptimeSeconds()
	if err != nil {
		return returnError(err)
	}

	rm := metricspb.ResourceMetrics{
		Resource: &resourcepb.Resource{
			Attributes: []*commonpb.KeyValue{
				{
					Key:   "service.name",
					Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hi"}},
				},
			},
		},
		ScopeMetrics: []*metricspb.ScopeMetrics{
			{
				Scope: &commonpb.InstrumentationScope{
					Name:    "app",
					Version: "1.0.0",
				},
				Metrics: []*metricspb.Metric{
					{
						Name: "system.uptime",
						Data: &metricspb.Metric_Gauge{
							Gauge: &metricspb.Gauge{
								DataPoints: []*metricspb.NumberDataPoint{
									{
										Value: &metricspb.NumberDataPoint_AsInt{AsInt: seconds},
									},
								},
							},
						},
					},
					// {
					// 	Name: "mock_server.requests",
					// 	Data: &metricspb.Metric_Gauge{
					// 		Gauge: &metricspb.Gauge{
					// 			DataPoints: []*metricspb.NumberDataPoint{
					// 				{
					// 					Value: &metricspb.NumberDataPoint_AsInt{AsInt: requests},
					// 				},
					// 			},
					// 		},
					// 	},
					// },
				},
			},
		},
	}

	metrics := metricspb.MetricsData{
		ResourceMetrics: []*metricspb.ResourceMetrics{&rm},
	}

	pmsg, err := proto.Marshal(metrics.ProtoReflect().Interface())
	if err != nil {
		return returnError(err)
	}
	ptr, size := bytesWasmRuntimeReadable(pmsg)

	return (uint64(ptr) << uint64(32)) | uint64(size)
}

//go:wasmimport env log
func _log(ptr, size uint32)

func returnError(err error) uint64 {
	ptr, size := bytesWasmRuntimeReadable([]byte(err.Error()))
	_log(ptr, size)
	return 0
}

func bytesWasmRuntimeReadable(b []byte) (uint32, uint32) {
	ptr := unsafe.Pointer(&b)
	size := len(b)
	copy(unsafe.Slice((*byte)(ptr), size), b)
	return uint32(uintptr(ptr)), uint32(size)
}

func getUptimeSeconds() (int64, error) {
	uptimeContent, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	secondsStr := strings.Split(strings.Split(string(uptimeContent), " ")[0], ".")[0]
	return strconv.ParseInt(secondsStr, 10, 64)
}

// main is required for the `wasi` target, even if it isn't used.
// See https://wazero.io/languages/tinygo/#why-do-i-have-to-define-main
func main() {}
