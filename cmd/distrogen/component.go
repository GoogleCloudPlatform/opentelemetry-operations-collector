package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

var errInvalidComponentType = errors.New("invalid component type")

type ComponentGenerator struct {
	Spec     *DistributionSpec
	FileMode fs.FileMode

	Type      ComponentType
	Name      string
	Path      string
	ModuleURL string
}

func NewComponentGenerator(spec *DistributionSpec, componentType ComponentType, componentName string) (*ComponentGenerator, error) {
	if spec.ComponentModuleBase == "" {
		return nil, errors.New("must supply a component_module_base in spec")
	}

	g := &ComponentGenerator{
		Spec:     spec,
		FileMode: DefaultProjectFileMode,
		Type:     componentType,
		Name:     componentName,
	}

	switch componentType {
	case Receiver:
		fallthrough
	case Processor:
		fallthrough
	case Exporter:
		fallthrough
	case Connector:
		fallthrough
	case Extension:
		fallthrough
	case Provider:
		g.Path = filepath.Join(
			"components",
			string(componentType),
			fmt.Sprintf("%s%s", componentName, componentType),
		)

	default:
		return nil, fmt.Errorf("%w: %s", errInvalidComponentType, componentType)
	}

	g.ModuleURL = path.Join(spec.ComponentModuleBase, g.Path)

	return g, nil
}

func (g *ComponentGenerator) Generate() error {
	componentTemplates, err := GetIndividualComponentTemplateSet(g, g.FileMode)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(g.Path, g.FileMode); err != nil {
		return err
	}

	for _, tmpl := range componentTemplates {
		// Setting the filepath to just the filename is equivalent
		// to saying that the file should just go in the root of
		// whatever path was sent to Render.
		tmpl.FilePath = tmpl.Name
		if err := tmpl.Render(g.Path); err != nil {
			return err
		}
	}

	registryPath := "components/registry.yaml"
	registry, err := LoadRegistry(registryPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		registry = NewRegistry()
		registry.Path = registryPath
	}

	registry.Add(g.Type, &RegistryComponent{
		GoMod: &GoModuleID{
			URL:           g.ModuleURL,
			AllowBlankTag: true,
		},
		Name: g.Name,
		Path: "../" + g.Path,
	})
	if err := registry.Save(); err != nil {
		return err
	}

	return nil
}
