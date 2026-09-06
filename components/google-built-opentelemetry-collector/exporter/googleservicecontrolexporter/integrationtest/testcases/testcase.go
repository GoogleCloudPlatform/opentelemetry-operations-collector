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

package testcases

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/otelcol/otelcoltest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TestCase struct {
	Name                 string
	ConfigPath           string
	OTLPInputFixturePath string
	ExpectFixturePath    string
}

// LoadOTLPLogsInput reads the logs from file and clean the timestamp
func (tc *TestCase) LoadOTLPLogsInput(
	t testing.TB,
	timestamp time.Time,
) plog.Logs {
	fixtureBytes, err := os.ReadFile(tc.OTLPInputFixturePath)
	require.NoError(t, err)
	unmarshaler := &plog.JSONUnmarshaler{}
	logs, err := unmarshaler.UnmarshalLogs(fixtureBytes)
	require.NoError(t, err)

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		rl := logs.ResourceLogs().At(i)
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sls := rl.ScopeLogs().At(j)
			for k := 0; k < sls.LogRecords().Len(); k++ {
				log := sls.LogRecords().At(k)
				if log.Timestamp() != 0 {
					log.SetTimestamp(pcommon.NewTimestampFromTime(timestamp))
				}
			}
		}
	}
	return logs
}

func (tc *TestCase) LoadConfig(
	t testing.TB) *googleservicecontrolexporter.Config {
	factories, err := otelcoltest.NopFactories()
	assert.NoError(t, err)

	factory := googleservicecontrolexporter.NewFactory()
	factories.Exporters[metadata.Type] = factory
	cfg, err := otelcoltest.LoadConfigAndValidate(tc.ConfigPath, factories)

	require.Nil(t, err)
	require.NotNil(t, cfg)

	conf := cfg.Exporters[component.NewID(metadata.Type)]
	return conf.(*googleservicecontrolexporter.Config)
}

// NormalizeLogFixture normalizes timestamps which create noise in the fixture
// because they can vary each test run.
func NormalizeRequestFixture(t testing.TB, fixture *scpb.ReportRequest) {
	for _, op := range fixture.Operations {
		op.StartTime = &timestamppb.Timestamp{}
		op.EndTime = &timestamppb.Timestamp{}
		op.OperationId = ""
	}
}

// NormalizeJson normalizes the JSON bytes; the protojson.Marshal() function
// does not guarantee the the output will be stable across runs; it may change
// orders or the number of whitespaces between the keys and values, etc. This
// function makes sure we get stable results for our golden tests
func NormalizeJson(jsonBytes []byte) ([]byte, error) {
	var v interface{}
	err := json.Unmarshal(jsonBytes, &v)
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(v, "", "  ")
}
