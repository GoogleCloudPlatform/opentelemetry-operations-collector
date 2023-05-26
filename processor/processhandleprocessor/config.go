package processhandleprocessor

import "go.opentelemetry.io/collector/component"

type Config struct{}

func createDefaultConfig() component.Config {
	return &Config{}
}
