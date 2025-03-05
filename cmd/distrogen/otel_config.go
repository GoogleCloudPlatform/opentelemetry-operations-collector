package main

import (
	"errors"
	"fmt"
)

var ErrSectionNotFound = errors.New("could not find section")

func ComponentsFromOTelConfig(otelConfig map[string]any) (*DistributionComponents, error) {
	components := &DistributionComponents{}
	var err error
	components.Receivers, err = readComponentsFromSection("receivers", otelConfig)
	if err != nil && !errors.Is(err, ErrSectionNotFound) {
		return nil, err
	}
	components.Processors, err = readComponentsFromSection("processors", otelConfig)
	if err != nil && !errors.Is(err, ErrSectionNotFound) {
		return nil, err
	}
	components.Exporters, err = readComponentsFromSection("exporters", otelConfig)
	if err != nil && !errors.Is(err, ErrSectionNotFound) {
		return nil, err
	}
	components.Connectors, err = readComponentsFromSection("connectors", otelConfig)
	if err != nil && !errors.Is(err, ErrSectionNotFound) {
		return nil, err
	}
	components.Extensions, err = readComponentsFromSection("extensions", otelConfig)
	if err != nil && !errors.Is(err, ErrSectionNotFound) {
		return nil, err
	}
	return components, nil
}

func readComponentsFromSection(sectionName string, otelConfig map[string]any) ([]string, error) {
	var section map[string]any
	rawSection, ok := otelConfig[sectionName]
	if !ok {
		return nil, fmt.Errorf("reading section %s: %w", sectionName, ErrSectionNotFound)
	}
	section, ok = rawSection.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reading section %s: invalid section data", sectionName)
	}
	return mapKeys(section), nil
}
