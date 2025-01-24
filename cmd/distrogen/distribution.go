package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/go-cmp/cmp"
)

var ErrNoDiff = errors.New("no differences found with previous generation")

type DistributionSpec struct {
	Name                       string                  `yaml:"name"`
	DisplayName                string                  `yaml:"display_name"`
	Description                string                  `yaml:"description"`
	Blurb                      string                  `yaml:"blurb"`
	Version                    string                  `yaml:"version"`
	OpenTelemetryVersion       string                  `yaml:"opentelemetry_version"`
	OpenTelemetryStableVersion string                  `yaml:"opentelemetry_stable_version"`
	GoVersion                  string                  `yaml:"go_version"`
	BinaryName                 string                  `yaml:"binary_name"`
	CollectorCGO               bool                    `yaml:"collector_cgo"`
	DockerRepo                 string                  `yaml:"docker_repo"`
	Components                 *DistributionComponents `yaml:"components"`
	Replaces                   OCBManifestReplaces     `yaml:"replaces,omitempty"`
	CustomValues               map[string]any          `yaml:"custom_values,omitempty"`
	FeatureGates               []string                `yaml:"feature_gates"`
}

func (s *DistributionSpec) Diff(s2 *DistributionSpec) bool {
	diff := cmp.Diff(s, s2)
	return diff != ""
}

type ComponentList []string

type DistributionComponents struct {
	Receivers  ComponentList `yaml:"receivers,omitempty"`
	Processors ComponentList `yaml:"processors,omitempty"`
	Exporters  ComponentList `yaml:"exporters,omitempty"`
	Connectors ComponentList `yaml:"connectors,omitempty"`
	Extensions ComponentList `yaml:"extensions,omitempty"`
	Providers  ComponentList `yaml:"providers,omitempty"`
}

type DistributionGenerator struct {
	Spec               *DistributionSpec
	GenerateDirName    string
	GeneratePath       string
	Registry           *Registry
	CustomTemplatesDir fs.FS
}

func NewDistributionGenerator(spec *DistributionSpec, registry *Registry, forceGenerate bool) (*DistributionGenerator, error) {
	d := DistributionGenerator{
		Spec:     spec,
		Registry: registry,
	}
	d.GenerateDirName = spec.Name

	if !forceGenerate {
		specCache, err := yamlUnmarshalFromFile[DistributionSpec](filepath.Join(d.GenerateDirName, "spec.yaml"))
		if err != nil {
			logger.Debug(fmt.Sprintf("generated spec could not be read: %v", err))
			if !os.IsNotExist(err) {
				return nil, err
			}
		} else {
			if !d.Spec.Diff(specCache) {
				return nil, ErrNoDiff
			}
		}
	}

	tmpDir, err := os.MkdirTemp(".", d.GenerateDirName)
	if err != nil {
		return nil, err
	}
	d.GeneratePath = tmpDir
	return &d, nil
}

func (d *DistributionGenerator) Generate() error {
	templates, err := GetEmbeddedTemplateSet(d.Spec)
	if err != nil {
		return err
	}

	if d.CustomTemplatesDir != nil {
		customTemplates, err := GetTemplateSetFromDir(d.CustomTemplatesDir, d.Spec)
		if err != nil {
			return err
		}

		// This merge means that any custom templates named the same as the embedded
		// defaults will overwrite the embedded version with the custom version.
		mapMerge(templates, customTemplates)
	}

	manifestContext, err := NewManifestContextFromSpec(d.Spec, d.Registry)
	if err != nil {
		return err
	}
	templates.SetTemplateContext("manifest.yaml.go.tmpl", manifestContext)
	templates.SetTemplateContext("README.md.go.tmpl", manifestContext)

	for _, tmpl := range templates {
		if err := tmpl.Render(d.GeneratePath); err != nil {
			return err
		}
	}
	if err := d.WriteSpec(); err != nil {
		return err
	}

	return nil
}

func (d *DistributionGenerator) WriteSpec() error {
	return yamlMarshalToFile(d.Spec, filepath.Join(d.GeneratePath, "spec.yaml"))
}

func (d *DistributionGenerator) MoveGeneratedDirToWd() (err error) {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	generateDest := filepath.Join(wd, d.GenerateDirName)
	bkpPath := generateDest + "-bkp"

	// Check if the distribution directory exists, rename it to backup
	// if it does.
	if _, err := os.Open(generateDest); err == nil {
		if err := os.Rename(generateDest, bkpPath); err != nil {
			return err
		}

		// Delete the backup. Sets the named `err` return value
		// if removal of backup fails.
		defer func() {
			err = os.RemoveAll(bkpPath)
		}()
	}

	// Move generated directory to working directory.
	if err := os.Rename(d.GeneratePath, generateDest); err != nil {
		return err
	}

	return nil
}

func (d *DistributionGenerator) Clean() error {
	if err := os.RemoveAll(d.GeneratePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type ManifestContext struct {
	*DistributionSpec

	Receivers  OCBManifestComponents
	Processors OCBManifestComponents
	Exporters  OCBManifestComponents
	Extensions OCBManifestComponents
	Connectors OCBManifestComponents
	Providers  OCBManifestComponents
}

func NewManifestContextFromSpec(spec *DistributionSpec, registry *Registry) (*ManifestContext, error) {
	context := ManifestContext{DistributionSpec: spec}

	errs := make(RegistryLoadError)
	var err RegistryLoadError
	context.Receivers, err = registry.Receivers.LoadAll(spec.Components.Receivers, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion)
	mapMerge(errs, err)
	context.Processors, err = registry.Processors.LoadAll(spec.Components.Processors, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion)
	mapMerge(errs, err)
	context.Exporters, err = registry.Exporters.LoadAll(spec.Components.Exporters, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion)
	mapMerge(errs, err)
	context.Connectors, err = registry.Connectors.LoadAll(spec.Components.Connectors, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion)
	mapMerge(errs, err)
	context.Extensions, err = registry.Extensions.LoadAll(spec.Components.Extensions, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion)
	mapMerge(errs, err)
	context.Providers, err = registry.Providers.LoadAll(spec.Components.Providers, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion)
	mapMerge(errs, err)

	if len(errs) > 0 {
		return nil, errs
	}
	return &context, nil
}

// FIXME: This whole implementation is a hack for demo purposes.
// Refactor if agreed upon as a feature.
type READMEContext struct {
	*DistributionSpec

	Receivers  map[string]*OCBManifestComponent
	Processors map[string]*OCBManifestComponent
	Exporters  map[string]*OCBManifestComponent
	Extensions map[string]*OCBManifestComponent
	Connectors map[string]*OCBManifestComponent
	Providers  map[string]*OCBManifestComponent
}

func NewREADMEContextFromSpec(spec *DistributionSpec, registry *Registry) (*READMEContext, error) {
	context := READMEContext{DistributionSpec: spec}

	errs := make(RegistryLoadError)
	var err RegistryLoadError
	context.Receivers, err = loadComponentMap(context.Components.Receivers, registry.Receivers)
	mapMerge(errs, err)
	context.Processors, err = loadComponentMap(context.Components.Processors, registry.Processors)
	mapMerge(errs, err)
	context.Exporters, err = loadComponentMap(context.Components.Exporters, registry.Exporters)
	mapMerge(errs, err)
	context.Connectors, err = loadComponentMap(context.Components.Connectors, registry.Connectors)
	mapMerge(errs, err)
	context.Extensions, err = loadComponentMap(context.Components.Extensions, registry.Extensions)
	mapMerge(errs, err)
	context.Providers, err = loadComponentMap(context.Components.Providers, registry.Providers)
	mapMerge(errs, err)

	if len(errs) > 0 {
		return nil, errs
	}
	return &context, nil
}

func loadComponentMap(components []string, registryList RegistryList) (map[string]*OCBManifestComponent, RegistryLoadError) {
	result := make(map[string]*OCBManifestComponent)
	errs := make(RegistryLoadError)
	var err error
	for _, componentName := range components {
		result[componentName], err = registryList.Load(componentName)
		if err != nil {
			errs[componentName] = err
		}
	}
	return result, errs
}
