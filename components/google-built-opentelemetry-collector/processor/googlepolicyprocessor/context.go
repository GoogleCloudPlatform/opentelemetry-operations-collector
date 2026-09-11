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

package googlepolicyprocessor

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// LogContext holds the contextual information needed to evaluate a single log record.
type LogContext struct {
	Record            plog.LogRecord
	Resource          pcommon.Resource
	Scope             pcommon.InstrumentationScope
	ResourceSchemaURL string
	ScopeSchemaURL    string
}

// MetricContext holds the contextual information needed to evaluate a single metric datapoint.
type MetricContext struct {
	Metric                 pmetric.Metric
	DatapointAttributes    pcommon.Map
	AggregationTemporality pmetric.AggregationTemporality
	Resource               pcommon.Resource
	Scope                  pcommon.InstrumentationScope
	ResourceSchemaURL      string
	ScopeSchemaURL         string
}

// TraceContext holds the contextual information needed to evaluate a single trace span.
type TraceContext struct {
	Span              ptrace.Span
	Resource          pcommon.Resource
	Scope             pcommon.InstrumentationScope
	ResourceSchemaURL string
	ScopeSchemaURL    string
}
