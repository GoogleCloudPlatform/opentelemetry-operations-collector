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

	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/extension/healthagent/internal/healthpb"
)

type healthAgentServer struct {
	logger   *zap.SugaredLogger
	scope    string
	name     string
	exporter *healthAgent
	healthpb.UnimplementedHealthAgentServer
}

func newServer(c *Config, l *zap.SugaredLogger, e *healthAgent) *healthAgentServer {
	return &healthAgentServer{
		logger:   l.With("config", c),
		exporter: e,
		scope:    c.Scope,
		name:     c.Name,
	}
}
func (s *healthAgentServer) GetHealth(_ context.Context, _ *healthpb.GetHealthRequest) (*healthpb.GetHealthResponse, error) {
	hs := healthpb.HealthStatus_HEALTHY
	if !s.exporter.isHealthy() {
		hs = healthpb.HealthStatus_UNHEALTHY
	}
	s.logger.Debugf("GetHealth status is %v", hs)
	return &healthpb.GetHealthResponse{
		HealthMetrics: []*healthpb.HealthMetric{
			{
				Scope:  s.scope,
				Name:   s.name,
				Status: hs,
			},
		},
	}, nil
}
