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

package healthagent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/extension/healthagent/internal/healthpb"
)

func TestGetHealth(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name          string
		ready         bool
		lastError     *componentstatus.Event
		lastOK        *componentstatus.Event
		errorInterval time.Duration
		want          healthpb.HealthStatus
	}{
		{
			name:  "Not ready",
			ready: false,
			want:  healthpb.HealthStatus_UNHEALTHY,
		},
		{
			name:  "No events",
			ready: true,
			want:  healthpb.HealthStatus_HEALTHY,
		},
		{
			name:      "Only OK",
			ready:     true,
			lastOK:    componentstatus.NewEvent(componentstatus.StatusOK),
			want:      healthpb.HealthStatus_HEALTHY,
			lastError: nil,
		},
		{
			name:      "Only error",
			ready:     true,
			lastError: componentstatus.NewRecoverableErrorEvent(assert.AnError),
			want:      healthpb.HealthStatus_UNHEALTHY,
			lastOK:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := &healthAgent{
				config: Config{
					ErrorCheckInterval: tt.errorInterval,
					Scope:              "test_scope",
					Name:               "test_name",
				},
				logger:    logger.Sugar(),
				lastError: tt.lastError,
				lastOK:    tt.lastOK,
				ready:     tt.ready,
			}
			server := newServer(&hc.config, hc.logger, hc)
			resp, err := server.GetHealth(context.Background(), &healthpb.GetHealthRequest{})
			assert.NoError(t, err)
			assert.Equal(t, 1, len(resp.HealthMetrics))
			assert.Equal(t, tt.want, resp.HealthMetrics[0].Status)
			assert.Equal(t, "test_scope", resp.HealthMetrics[0].Scope)
			assert.Equal(t, "test_name", resp.HealthMetrics[0].Name)
		})
	}
}
