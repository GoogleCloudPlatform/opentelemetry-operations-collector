package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-cmp/cmp"
)

var ErrNoDiff = errors.New("no differences found with previous generation")

type DistributionSpec struct {
	Name                 string                  `yaml:"name"`
	Description          string                  `yaml:"description"`
	Version              string                  `yaml:"version"`
	OpenTelemetryVersion string                  `yaml:"opentelemetry_version"`
	GoVersion            string                  `yaml:"go_version"`
	BinaryName           string                  `yaml:"binary_name"`
	CollectorCGO         bool                    `yaml:"collector_cgo"`
	DockerRepo           string                  `yaml:"docker_repo"`
	Components           *DistributionComponents `yaml:"components"`
	Replaces             OCBManifestReplaces     `yaml:"replaces,omitempty"`
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
	Connectors ComponentList `yaml:"connector,omitempty"`
	Extensions ComponentList `yaml:"extensions,omitempty"`
}

type DistributionGenerator struct {
	Spec            *DistributionSpec
	GenerateDirName string
	GeneratePath    string
	Registry        *Registry
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
	templates := []TemplateFile{
		{
			Name:    "Makefile",
			Context: d.Spec,
		},
		{
			Name:    "Dockerfile",
			Context: d.Spec,
		},
		{
			Name:    "config.yaml",
			Context: d.Spec,
		},
	}

	manifestContext, err := NewManifestContextFromSpec(d.Spec, d.Registry)
	if err != nil {
		return err
	}
	templates = append(templates, TemplateFile{
		Name:    "manifest.yaml",
		Context: manifestContext,
	})

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
