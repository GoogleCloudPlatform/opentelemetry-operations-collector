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

package integrationtest

import (
	"context"
	"testing"
	"time"

	sc "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter/integrationtest/testcases"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter/internal/metadata"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"google.golang.org/protobuf/encoding/protojson"
	"gotest.tools/v3/golden"
)

var (
	testLogTime = time.Date(2020, 2, 11, 20, 26, 13, 789, time.UTC)
)

func TestLogs(t *testing.T) {
	ctx := context.Background()

	for _, test := range testcases.LogsTestCases {
		t.Run(test.Name, func(t *testing.T) {
			logs := test.LoadOTLPLogsInput(t, testLogTime)

			mockServer, err := sc.StartNetworkMockServer()
			defer sc.StopMockServer(mockServer)
			require.NoError(t, err)

			config := test.LoadConfig(t)
			config.ServiceControlEndpoint = mockServer.Address
			config.UseInsecure = true
			// Disable queuing, so that ConsumeLogs is called synchronously.
			config.QueueConfig.Enabled = false

			factory := sc.NewFactory()
			settings := exportertest.NewNopSettings(metadata.Type)
			exporter, err := factory.CreateLogs(ctx, settings, config)
			require.NoError(t, err)
			err = exporter.Start(ctx, componenttest.NewNopHost())
			require.NoError(t, err)

			err = exporter.ConsumeLogs(ctx, logs)
			require.NoError(t, err, "Failed to export logs to local test server at %s", mockServer.Address)
			require.Greater(t, mockServer.CallCount(), 0)
			requests := mockServer.Requests

			allRequests := ""
			for _, req := range requests {
				testcases.NormalizeRequestFixture(t, req)
				reqJson, err := protojson.MarshalOptions{
					Multiline: true,
					Indent:    "  ",
				}.Marshal(req)
				require.NoError(t, err)
				allRequests += string(reqJson) + "\n"
			}

			golden.Assert(t, allRequests, test.ExpectFixturePath)
			t.Cleanup(func() { require.NoError(t, exporter.Shutdown(ctx)) })
		})
	}
}
