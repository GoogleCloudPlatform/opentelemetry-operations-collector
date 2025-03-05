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

// DistributionSpec is the specification for a new OpenTelemetry Collector distribution.
// It contains all the information that will be formatted into the default set of
// templates/user provided templates.
type DistributionSpec struct {
	Name                        string                  `yaml:"name"`
	DisplayName                 string                  `yaml:"display_name"`
	Description                 string                  `yaml:"description"`
	Blurb                       string                  `yaml:"blurb"`
	Version                     string                  `yaml:"version"`
	OpenTelemetryVersion        string                  `yaml:"opentelemetry_version"`
	OpenTelemetryContribVersion string                  `yaml:"opentelemetry_contrib_version"`
	OpenTelemetryStableVersion  string                  `yaml:"opentelemetry_stable_version"`
	GoVersion                   string                  `yaml:"go_version"`
	BinaryName                  string                  `yaml:"binary_name"`
	CollectorCGO                bool                    `yaml:"collector_cgo"`
	DockerRepo                  string                  `yaml:"docker_repo"`
	Components                  *DistributionComponents `yaml:"components"`
	Replaces                    ComponentReplaces       `yaml:"replaces,omitempty"`
	CustomValues                map[string]any          `yaml:"custom_values,omitempty"`
	FeatureGates                FeatureGates            `yaml:"feature_gates"`
}

// Diff will compare two different DistributionSpecs.
func (s *DistributionSpec) Diff(s2 *DistributionSpec) bool {
	diff := cmp.Diff(s, s2)
	return diff != ""
}

// NewDistributionSpec loads the DistributionSpec from a yaml file.
func NewDistributionSpec(path string) (*DistributionSpec, error) {
	spec, err := yamlUnmarshalFromFile[DistributionSpec](path)
	if err != nil {
		return nil, err
	}
	// It is a rare case where the contrib version falls out of sync with
	// the canonical OpenTelemetry version, most of the time it is the same.
	if spec.OpenTelemetryContribVersion == "" {
		spec.OpenTelemetryContribVersion = spec.OpenTelemetryVersion
	}
	return spec, nil
}

// DistributionComponents is a set of components with RegistryComponent names
// that defines all the components included in this collector distribution.
type DistributionComponents struct {
	Receivers  []string `yaml:"receivers,omitempty"`
	Processors []string `yaml:"processors,omitempty"`
	Exporters  []string `yaml:"exporters,omitempty"`
	Connectors []string `yaml:"connectors,omitempty"`
	Extensions []string `yaml:"extensions,omitempty"`
	Providers  []string `yaml:"providers,omitempty"`
}

// DistributionGenerator contains all the facilities to generate a distribution
// from a DistributionSpec.
type DistributionGenerator struct {
	Spec               *DistributionSpec
	GenerateDirName    string
	GeneratePath       string
	Registry           *Registry
	CustomTemplatesDir fs.FS
}

// NewDistributionGenerator creates a DistributionGenerator.
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

// Generate will generate the distribution. It will generate the distribution
// in a temporary local directory, and upon there no errors in the generation
// will move it into the destination path.
func (d *DistributionGenerator) Generate() error {
	templateContext, err := NewTemplateContextFromSpec(d.Spec, d.Registry)
	if err != nil {
		return err
	}
	templates, err := GetEmbeddedTemplateSet(templateContext)
	if err != nil {
		return err
	}

	if d.CustomTemplatesDir != nil {
		customTemplates, err := GetTemplateSetFromDir(d.CustomTemplatesDir, templateContext)
		if err != nil {
			return err
		}

		// This merge means that any custom templates named the same as the embedded
		// defaults will overwrite the embedded version with the custom version.
		mapMerge(templates, customTemplates)
	}

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

// WriteSpec renders the DistributionSpec in a yaml file that lives in the generated
// distribution. This is a human readable way to keep track of what spec was used for
// this existing generation, as well as a method of detecting whether a new generation
// needs to be done at all (if no spec changes no need to generate).
func (d *DistributionGenerator) WriteSpec() error {
	return yamlMarshalToFile(d.Spec, filepath.Join(d.GeneratePath, "spec.yaml"))
}

// MoveGeneratedDirToWd performs the final step of the generation, moving the generated temp
// directory to the destination path. It tries to do this in a way where nothing is destroyed
// until everything is confirmed to work.
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

type TemplateContext struct {
	*DistributionSpec

	Receivers  RegistryComponents
	Processors RegistryComponents
	Exporters  RegistryComponents
	Extensions RegistryComponents
	Connectors RegistryComponents
	Providers  RegistryComponents
}

func NewTemplateContextFromSpec(spec *DistributionSpec, registry *Registry) (*TemplateContext, error) {
	context := TemplateContext{DistributionSpec: spec}

	errs := make(RegistryLoadError)
	var err RegistryLoadError
	context.Receivers, err = registry.Receivers.LoadAll(spec.Components.Receivers, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion, spec.OpenTelemetryContribVersion)
	mapMerge(errs, err)
	context.Processors, err = registry.Processors.LoadAll(spec.Components.Processors, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion, spec.OpenTelemetryContribVersion)
	mapMerge(errs, err)
	context.Exporters, err = registry.Exporters.LoadAll(spec.Components.Exporters, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion, spec.OpenTelemetryContribVersion)
	mapMerge(errs, err)
	context.Connectors, err = registry.Connectors.LoadAll(spec.Components.Connectors, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion, spec.OpenTelemetryContribVersion)
	mapMerge(errs, err)
	context.Extensions, err = registry.Extensions.LoadAll(spec.Components.Extensions, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion, spec.OpenTelemetryContribVersion)
	mapMerge(errs, err)
	context.Providers, err = registry.Providers.LoadAll(spec.Components.Providers, spec.OpenTelemetryVersion, spec.OpenTelemetryStableVersion, spec.OpenTelemetryContribVersion)
	mapMerge(errs, err)

	if len(errs) > 0 {
		return nil, errs
	}
	return &context, nil
}

type FeatureGates []string

// Render will render the feature gates in a comma separated list.
func (fgs FeatureGates) Render() string {
	// This case should never come up in template rendering,
	// but it's here as a backup in case.
	if len(fgs) == 0 {
		return ""
	}

	gates := ""
	first := true
	for _, fg := range fgs {
		gates += fg
		if first {
			first = false
		} else {
			gates += ","
		}
	}
	return gates
}

// ComponentReplace is a Go module replacement that will be
// rendered into the OCB manifest.
type ComponentReplace struct {
	From   *GoModuleID `yaml:"from"`
	To     *GoModuleID `yaml:"to"`
	Reason string      `yaml:"reason"`
}

// String renders the component replace for an OCB manifest.
func (r *ComponentReplace) String() string {
	r.From.AllowBlankTag = true
	r.To.AllowBlankTag = true
	// This is pretty awkward and it would be nice to implement yaml.Marshaler
	// on here instead, but this was the only nice way I could find to render
	// the Reason field as a comment above the replacement entry.
	return fmt.Sprintf("# %s\n- %s => %s", r.Reason, r.From, r.To)
}

// ComponentReplaces is a collection of component replacements.
type ComponentReplaces []*ComponentReplace

// Render renders the component replaces all at once
// for an OCB manifest.
func (rs ComponentReplaces) Render() string {
	result := ""
	for _, r := range rs {
		result += fmt.Sprintf("%s\n", r)
	}
	return result
}
