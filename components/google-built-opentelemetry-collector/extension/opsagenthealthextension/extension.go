package opsagenthealthextension

import (
	"context"

	"github.com/GoogleCloudPlatform/ops-agent/pkg/healthchecks"
	"github.com/GoogleCloudPlatform/ops-agent/pkg/self_metrics"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"
)

type opsagentExtension struct {
	cfg      *Config
	settings extension.Settings
	health   string
}

func newExtension(cfg *Config, set extension.Settings) *opsagentExtension {
	return &opsagentExtension{
		cfg:      cfg,
		settings: set,
		health:   "OK",
	}
}

func (e *opsagentExtension) Start(ctx context.Context, host component.Host) error {
	e.settings.Logger.Info("Starting Ops Agent Health Check Extension")

	// Generate the otlp json files on startup of the extension
	if err := self_metrics.GenerateOpsAgentSelfMetricsOTLPJSON(ctx, e.cfg.ConfigPath, e.cfg.OutDir); err != nil {
		e.settings.Logger.Error("Failed to generate ops agent self metrics OTLP JSON", zap.Error(err))
	} else {
		e.settings.Logger.Info("Generated feature tracking, enabled receivers, and logging ping OTLP JSON files", zap.String("outDir", e.cfg.OutDir))
	}

	go e.monitorHealth(ctx)
	return nil
}

func (e *opsagentExtension) Shutdown(ctx context.Context) error {
	return nil
}

func (e *opsagentExtension) monitorHealth(ctx context.Context) {
	runChecks := func() {
		// Run healthchecks
		fileLogger := healthchecks.CreateHealthChecksLogger("/var/log/google-cloud-ops-agent")
		results := healthchecks.HealthCheckRegistryFactory(false).RunAllHealthChecks(fileLogger)

		extLog := extLogger{s: e.settings.Logger.Sugar()}
		healthchecks.LogHealthCheckResults(results, extLog)

		hasFatal := false
		for _, res := range results {
			for _, err := range res.ErrorSlice() {
				if err != nil {
					if healthErr, ok := err.(healthchecks.HealthCheckError); ok && healthErr.IsFatal {
						hasFatal = true
					}
				}
			}
		}

		if hasFatal {
			e.health = "FAIL"
		} else {
			e.health = "OK"
		}

		e.settings.Logger.Info("Health checks evaluated", zap.String("status", e.health))
	}

	// Run once immediately
	runChecks()
}

func (e *opsagentExtension) GetHealthState() string {
	return e.health
}

type extLogger struct {
	s *zap.SugaredLogger
}

func (l extLogger) Infof(format string, v ...any)  { l.s.Infof(format, v...) }
func (l extLogger) Warnf(format string, v ...any)  { l.s.Warnf(format, v...) }
func (l extLogger) Errorf(format string, v ...any) { l.s.Errorf(format, v...) }
func (l extLogger) Infow(msg string, kv ...any)    { l.s.Infow(msg, kv...) }
func (l extLogger) Warnw(msg string, kv ...any)    { l.s.Warnw(msg, kv...) }
func (l extLogger) Errorw(msg string, kv ...any)   { l.s.Errorw(msg, kv...) }
func (l extLogger) Println(v ...any)               { l.s.Infoln(v...) }
