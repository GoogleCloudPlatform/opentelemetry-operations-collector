package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

var componentRegistryPath = filepath.Join("components", "registry.yaml")

type ComponentsRegistryGenerator struct {
	FileMode fs.FileMode

	Path string
}

func NewComponentsRegistryGenerator() *ComponentsRegistryGenerator {
	g := &ComponentsRegistryGenerator{
		FileMode: DefaultProjectFileMode,
		Path:     ".",
	}
	return g
}

func (g *ComponentsRegistryGenerator) Generate() error {
	registry := NewRegistry()

	generateComponentsPath := filepath.Join(g.Path, "components")
	registry.Path = filepath.Join(generateComponentsPath, "registry.yaml")

	var dirErrors []error
	if err := os.MkdirAll(generateComponentsPath, g.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}

	if len(dirErrors) > 0 {
		return errors.Join(dirErrors...)
	}

	if err := registry.Save(); err != nil {
		return err
	}

	templates, err := GetComponentsTemplateSet(g, g.FileMode)
	if err != nil {
		return err
	}

	if err := GenerateTemplateSet(generateComponentsPath, templates); err != nil {
		logger.Debug("failed to get component templates", "err", err)
		return err
	}
	return nil
}
