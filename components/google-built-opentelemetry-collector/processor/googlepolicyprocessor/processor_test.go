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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func TestProcessTraces(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()

	out, err := p.processTraces(context.Background(), td)
	require.NoError(t, err)
	assert.Equal(t, td, out)
}

func TestProcessMetrics(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()

	out, err := p.processMetrics(context.Background(), md)
	require.NoError(t, err)
	assert.Equal(t, md, out)
}

func TestProcessLogs(t *testing.T) {
	p := newGooglePolicyProcessor(&Config{}, zap.NewNop())
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()

	out, err := p.processLogs(context.Background(), ld)
	require.NoError(t, err)
	assert.Equal(t, ld, out)
}
