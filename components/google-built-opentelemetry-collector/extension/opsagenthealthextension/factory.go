package opsagenthealthextension

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

const (
	typeStr = "opsagenthealth"
)

func NewFactory() extension.Factory {
	return extension.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		createExtension,
		component.StabilityLevelAlpha,
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		ConfigPath: "/etc/google-cloud-ops-agent/config.yaml",
		OutDir:     "/run/google-cloud-ops-agent",
	}
}

func createExtension(ctx context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	return newExtension(cfg.(*Config), set), nil
}
