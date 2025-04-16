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
	"fmt"
	"net"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/extension/healthagent/internal/healthpb"
)

type healthAgent struct {
	config       Config
	logger       *zap.SugaredLogger
	server       *grpc.Server
	set          extension.Settings
	healthMetric metric.Int64ObservableGauge
	mu           sync.Mutex
	ready        bool
	lastError    *componentstatus.Event
	lastOK       *componentstatus.Event
}

func newHealthAgent(config Config, set extension.Settings) *healthAgent {
	return &healthAgent{
		config: config,
		set:    set,
		logger: set.Logger.Sugar(),
	}
}

// isHealthy returns true iff the agent is HEALTHY.
func (hc *healthAgent) isHealthy() bool {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	if !hc.ready {
		hc.logger.Debug("Pipelines are not ready yet")
		return false
	}
	if hc.lastOK == nil && hc.lastError == nil {
		// Should not happen, but let's return HEALTHY since we haven't seen any errors.
		return true
	}
	if hc.lastError == nil {
		// There was never an error => HEALTHY
		return true
	}
	if hc.lastOK == nil {
		// There was never OK => UNHEALTHY
		hc.logger.Infof("There was never OK => UNHEALTHY")
		return false
	}
	// If lastError happenned after lastOk => UNHEALTHY
	// else, if lastError is within (time.Now() - ErrorCheckInterval, time.Now()] => UNHEALTHY
	// else => HEALTHY
	if hc.lastError.Timestamp().After(hc.lastOK.Timestamp()) {
		hc.logger.Infof("lastError happenned after lastOk, hc.lastError: %v, hc.lastOK: %v", hc.lastError.Timestamp(), hc.lastOK.Timestamp())
		return false
	}
	if hc.lastError.Timestamp().After(time.Now().Add(-hc.config.ErrorCheckInterval)) {
		hc.logger.Infof("lastError is within (time.Now() - ErrorCheckInterval, time.Now()], hc.lastError: %v, hc.lastOK: %v", hc.lastError.Timestamp(), hc.lastOK.Timestamp())
		return false
	}
	return true
}
func (hc *healthAgent) startGRPCServer(host component.Host) error {
	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%s", hc.config.Port))
	if err != nil {
		return err
	}
	hc.server = grpc.NewServer()
	healthpb.RegisterHealthAgentServer(hc.server, newServer(&hc.config, hc.logger, hc))
	go func() {
		err := hc.server.Serve(lis)
		if err != nil {
			componentstatus.ReportStatus(host, componentstatus.NewFatalErrorEvent(err))
		}
	}()
	return nil
}
func updateStatus(st **componentstatus.Event, event *componentstatus.Event) {
	if *st == nil || (*st).Timestamp().Before(event.Timestamp()) {
		*st = event
	}
}

// healthAgent subscribes to OpenTelemetry updates about components via ComponentStatusChanged function.
// Any component (exporter\receiver\processor) that reports its status will be caught here.
// Code references:
// - https://github.com/open-telemetry/opentelemetry-collector/blob/v0.92.0/extension/extension.go#L62
// - https://github.com/open-telemetry/opentelemetry-collector/blob/v0.92.0/component/status.go#L27-L42
// - https://github.com/open-telemetry/opentelemetry-collector/pull/8169#issuecomment-1670048722
func (hc *healthAgent) ComponentStatusChanged(source *componentstatus.InstanceID, event *componentstatus.Event) {
	if event.Status() != componentstatus.StatusOK && event.Status() != componentstatus.StatusRecoverableError {
		return
	}
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.logger.Debugf("Health check status updated to %s, based on signal from component %s", event.Status().String(), source.ComponentID().String())
	if event.Status() == componentstatus.StatusOK {
		updateStatus(&hc.lastOK, event)
	} else {
		updateStatus(&hc.lastError, event)
	}
}

// Start and Shutdown are defined here:
// https://github.com/open-telemetry/opentelemetry-collector/blob/v0.55.0/component/component.go#L45-L72.
func (hc *healthAgent) Start(_ context.Context, host component.Host) error {
	err := hc.startGRPCServer(host)
	if err != nil {
		return err
	}
	// Check if the host implements componentstatus.Reporter
	if _, ok := host.(componentstatus.Reporter); ok {
		hc.logger.Info("Health Agent Host implements componentstatus.Reporter")
	} else {
		hc.logger.Info("Health Agent Host does not implement componentstatus.Reporter")
	}
	return nil
}
func (hc *healthAgent) Shutdown(context.Context) error {
	// If `lis` creation failed in startGRPCServer, then hc.server is nil.
	if hc.server == nil {
		return nil
	}
	hc.server.GracefulStop() // Calls `Stop()` on `lis` from `startGRPCServer`.
	return nil
}

// Ready and NotReady are defined here:
// https://github.com/open-telemetry/opentelemetry-collector/blob/v0.55.0/component/extension.go#L30-L45.
func (hc *healthAgent) Ready() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.ready = true
	return nil
}
func (hc *healthAgent) NotReady() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.ready = false
	return nil
}
