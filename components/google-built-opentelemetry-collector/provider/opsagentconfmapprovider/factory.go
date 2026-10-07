package opsagentconfmapprovider

import (
	"go.opentelemetry.io/collector/confmap"
)

type factory struct{}

func NewFactory() confmap.ProviderFactory {
	return &factory{}
}

func (f *factory) Create(set confmap.ProviderSettings) confmap.Provider {
	return newProvider(set)
}
