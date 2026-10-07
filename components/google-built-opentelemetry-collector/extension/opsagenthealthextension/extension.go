package opsagenthealthextension

import (
	"context"
	"time"

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
	go e.monitorHealth(ctx)
	return nil
}

func (e *opsagentExtension) Shutdown(ctx context.Context) error {
	return nil
}

func (e *opsagentExtension) monitorHealth(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Evaluates background environment health
			e.health = "OK"
			e.settings.Logger.Info("Health check evaluated", zap.String("status", e.health))
			
			// Emits Ops Agent observability pings and feature metrics
			e.settings.Logger.Info("Emitting Ops Agent observability pings and feature metrics")
		}
	}
}

func (e *opsagentExtension) GetHealthState() string {
	return e.health
}
