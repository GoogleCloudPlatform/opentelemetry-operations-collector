// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package varnishreceiver

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/varnishreceiver/internal/metadata"
)

type varnishScraper struct {
	client            client
	config            *Config
	telemetrySettings component.TelemetrySettings
	mb                *metadata.MetricsBuilder
	cacheName         string
}

func newVarnishScraper(settings receiver.CreateSettings, config *Config) *varnishScraper {
	return &varnishScraper{
		telemetrySettings: settings.TelemetrySettings,
		config:            config,
		mb:                metadata.NewMetricsBuilder(metadata.DefaultMetricsSettings(), settings),
	}
}

func (v *varnishScraper) start(_ context.Context, host component.Host) error {
	v.client = newVarnishClient(v.config, host, v.telemetrySettings)
	return v.setCacheName()
}

// setCacheName sets the cache name to the targeted varnish instance.
func (v *varnishScraper) setCacheName() error {
	if v.config.CacheDir == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
		v.cacheName = hostname
		return nil
	}

	v.cacheName = filepath.Base(v.config.CacheDir)
	return nil
}

func (v *varnishScraper) scrape(context.Context) (pmetric.Metrics, error) {
	stats, err := v.client.GetStats()
	if err != nil {
		v.telemetrySettings.Logger.Error("Failed to execute varnishstat",
			zap.String("Cache Dir:", v.config.CacheDir),
			zap.String("Executable Directory:", v.config.ExecDir),
			zap.Error(err),
		)
		return pmetric.NewMetrics(), err
	}

	now := pcommon.NewTimestampFromTime(time.Now())

	v.recordVarnishBackendConnectionsCountDataPoint(now, stats)
	v.recordVarnishCacheOperationsCountDataPoint(now, stats)
	v.recordVarnishThreadOperationsCountDataPoint(now, stats)
	v.recordVarnishSessionCountDataPoint(now, stats)
	v.recordVarnishClientRequestsCountDataPoint(now, stats)
	v.recordVarnishClientRequestErrorCountDataPoint(now, stats)

	v.mb.RecordVarnishObjectExpiredDataPoint(now, stats.MAINNExpired.Value)
	v.mb.RecordVarnishObjectNukedDataPoint(now, stats.MAINNLruNuked.Value)
	v.mb.RecordVarnishObjectMovedDataPoint(now, stats.MAINNLruMoved.Value)
	v.mb.RecordVarnishObjectCountDataPoint(now, stats.MAINNObject.Value)
	v.mb.RecordVarnishBackendRequestCountDataPoint(now, stats.MAINBackendReq.Value)

	return v.mb.Emit(metadata.WithVarnishCacheName(v.cacheName)), nil
}
