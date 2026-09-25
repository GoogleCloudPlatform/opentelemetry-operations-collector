// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

//go:embed registry.yaml
var registryContent []byte

var ErrComponentNotFound = errors.New("component not found")

type ComponentType string

const (
	Receiver  ComponentType = "receiver"
	Processor ComponentType = "processor"
	Exporter  ComponentType = "exporter"
	Connector ComponentType = "connector"
	Extension ComponentType = "extension"
	Provider  ComponentType = "provider"
)

// Registry is a collection of components that can be used in
// a collector distribution.
type Registry struct {
	Receivers  RegistryComponents `yaml:"receivers"`
	Processors RegistryComponents `yaml:"processors"`
	Exporters  RegistryComponents `yaml:"exporters"`
	Connectors RegistryComponents `yaml:"connectors"`
	Extensions RegistryComponents `yaml:"extensions"`
	Providers  RegistryComponents `yaml:"providers"`
	Path       string             `yaml:"-"`

	// moduleVersions is the released version of every module published by the
	// core and contrib repositories. It is empty until ResolveOTelModuleVersions
	// is called, in which case components fall back to the release version of
	// the repository they come from.
	moduleVersions otelModuleVersions `yaml:"-"`
}

// NewRegistry will create an empty registry object with the
// component lists preallocated.
func NewRegistry() *Registry {
	return &Registry{
		Receivers:  RegistryComponents{},
		Processors: RegistryComponents{},
		Exporters:  RegistryComponents{},
		Connectors: RegistryComponents{},
		Extensions: RegistryComponents{},
		Providers:  RegistryComponents{},
	}
}

// LoadEmbeddedRegistry will load the registry embedded in the
// distrogen binary.
func LoadEmbeddedRegistry() (*Registry, error) {
	var r Registry
	if err := yaml.Unmarshal(registryContent, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// LoadRegistry will load a registry from a yaml file.
func LoadRegistry(path string) (*Registry, error) {
	r := NewRegistry()
	err := yamlUnmarshalFromFileInto(path, r)
	r.Path = path
	return r, err
}

// Merge will merge another registry into this one. If the provided
// registry contains any of the same entry keys as the current
// registry, it will be overridden.
func (r *Registry) Merge(r2 *Registry) {
	mapMerge(r.Receivers, r2.Receivers)
	mapMerge(r.Processors, r2.Processors)
	mapMerge(r.Exporters, r2.Exporters)
	mapMerge(r.Connectors, r2.Connectors)
	mapMerge(r.Extensions, r2.Extensions)
	mapMerge(r.Providers, r2.Providers)
}

func (r *Registry) Add(componentType ComponentType, component *RegistryComponent) {
	switch componentType {
	case Receiver:
		r.Receivers[component.Name] = component
	case Processor:
		r.Processors[component.Name] = component
	case Exporter:
		r.Exporters[component.Name] = component
	case Connector:
		r.Connectors[component.Name] = component
	case Extension:
		r.Extensions[component.Name] = component
	case Provider:
		r.Providers[component.Name] = component
	}
}

func (r *Registry) Save() error {
	if r.Path == "" {
		return errors.New("cannot save registry: no path set")
	}

	if err := yamlMarshalToFile(r, r.Path, DefaultProjectFileMode); err != nil {
		return err
	}

	return nil
}

// GoModuleID is intended for stringifying/unmarshalling to
// a Go module ID, i.e. github.com/package/name v0.0.0 format.
type GoModuleID struct {
	URL           string
	Tag           string
	AllowBlankTag bool
}

// String outputs the GoModuleID details in proper format.
func (gm *GoModuleID) String() string {
	tag := gm.Tag
	if tag == "" {
		// There are certain cases (like local paths) where
		// the tag for a Go Module ID is allowed to be blank.
		if gm.AllowBlankTag {
			return gm.URL
		}

		// Otherwise if there is no tag specified, then it is assumed that this module
		// will be replaced. Use the tag v0.0.0 since it will be ignored
		// in the replace anyway.
		logger.Debug("no tag detected for module, using v0.0.0", slog.String("module", gm.URL))
		tag = "v0.0.0"
	}
	return fmt.Sprintf("%s %s", gm.URL, tag)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
// It takes a properly formed Go Module ID string and unpacks
// it into the struct.
func (gm *GoModuleID) UnmarshalYAML(value *yaml.Node) error {
	// The module ID may have a version.
	moduleStr := value.Value
	moduleComponents := strings.Split(moduleStr, " ")
	gm.URL = moduleComponents[0]
	if len(moduleComponents) > 1 {
		gm.Tag = moduleComponents[1]
	}
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface. It leverages
// the String method to allow outputting the value into a YAML document
// in the module ID string form.
func (gm *GoModuleID) MarshalYAML() (interface{}, error) {
	return gm.String(), nil
}

type otelComponentVersion struct {
	core    string
	contrib string

	// modules is the released version of each upstream module, taken from the
	// versions.yaml of the repository that publishes it.
	modules otelModuleVersions
}

// RegistryComponent is the type used as a basis for Registry.
// It contains all the information needed to output a
type RegistryComponent struct {
	Name string `yaml:"-"`

	GoMod         *GoModuleID `yaml:"gomod"`
	Import        string      `yaml:"import,omitempty"`
	Path          string      `yaml:"path,omitempty"`
	StartRevision string      `yaml:"start_revision,omitempty"`
	DocsURL       string      `yaml:"docs_url,omitempty"`
}

// RenderDocsURL renders the docs URL into a template.
func (c *RegistryComponent) RenderDocsURL() string {
	if c.DocsURL == "" {
		return "No docs linked for component"
	}
	return c.DocsURL
}

// IsContrib determines whether the module comes from the opentelemetry-collector-contrib repo.
func (c *RegistryComponent) IsContrib() bool {
	return strings.Contains(c.GoMod.URL, "github.com/open-telemetry/opentelemetry-collector-contrib")
}

// ApplyOTelVersion sets the module tag for this component. The version each
// upstream module was released at is read from the versions.yaml of the
// repository that publishes it, which is the only way to know whether a module
// is part of a stable 1.x module set.
//
// Modules that upstream does not publish, i.e. components that have been
// removed from contrib or components local to a project, are tagged with the
// release version of the repository they belong to.
func (c *RegistryComponent) ApplyOTelVersion(otelVersion otelComponentVersion) {
	if tag, ok := otelVersion.modules[c.GoMod.URL]; ok {
		c.GoMod.Tag = tag
		return
	}

	logger.Debug("module not published in versions.yaml, using the repository release version", slog.String("module", c.GoMod.URL))
	c.GoMod.Tag = "v" + otelVersion.core
	if c.IsContrib() {
		c.GoMod.Tag = "v" + otelVersion.contrib
	}
}

// OCBManifestComponent is a reflection of the fields for an
// entry in an OCB manifest yaml.
type OCBManifestComponent struct {
	GoMod  *GoModuleID `yaml:"gomod"`
	Import string      `yaml:"import,omitempty"`
	Name   string      `yaml:"string,omitempty"`
	Path   string      `yaml:"path,omitempty"`
}

// GetOCBComponent will return an OCBManifestComponent using
// the details from this RegistryComponent.
func (c *RegistryComponent) GetOCBComponent() OCBManifestComponent {
	return OCBManifestComponent{
		GoMod:  c.GoMod,
		Import: c.Import,
		Name:   c.Name,
		Path:   c.Path,
	}
}

// RegistryComponentRelease is a particular tag of a component that declares
// the Collector library version it supports.
type RegistryComponentRelease struct {
	Tag                         string `yaml:"version"`
	OpenTelemetryVersion        string `yaml:"opentelemetry_version"`
	OpenTelemetryContribVersion string `yaml:"opentelemetry_contrib_version,omitempty"`
}

// RegistryComponents is a map of registry component names to component
// details.
type RegistryComponents map[string]*RegistryComponent

// LoadAllComponents will take a list of component names and load them
// from the registry, attaching the appropriate version tag.
func (rl RegistryComponents) LoadAllComponents(names []string, otelVersion otelComponentVersion) (RegistryComponents, CollectionError) {
	components := RegistryComponents{}
	errs := make(CollectionError)

	for _, name := range names {
		entry, err := rl.LoadComponent(name, otelVersion)
		if err != nil {
			errs[name] = ErrComponentNotFound
			continue
		}
		components[name] = entry
	}

	return components, errs
}

func (rl RegistryComponents) LoadComponent(name string, otelVersion otelComponentVersion) (*RegistryComponent, error) {
	entry, ok := rl[name]
	if !ok {
		return nil, ErrComponentNotFound
	}
	entry.ApplyOTelVersion(otelVersion)
	return entry, nil
}

// Validate is intended to be called before template rendering.
// This way, calling the Render method from the template can assume
// no error.
func (cs RegistryComponents) Validate() error {
	_, err := yaml.Marshal(cs)
	return err
}

// RenderOCBComponents will render the registry entries as
func (cs RegistryComponents) RenderOCBComponents() string {
	if len(cs) == 0 {
		return ""
	}

	renderComponents := []OCBManifestComponent{}
	for _, c := range cs {
		renderComponents = append(renderComponents, c.GetOCBComponent())
	}

	// The component list is sorted here to ensure that re-generating will always
	// have a consistent order.
	slices.SortFunc(renderComponents, func(a OCBManifestComponent, b OCBManifestComponent) int {
		return strings.Compare(a.GoMod.URL, b.GoMod.URL)
	})

	return renderYaml(renderComponents)
}

// otelVersionsYamlURL is the versions.yaml of an OpenTelemetry repository, which
// declares the version that every module of that repository is released at.
const otelVersionsYamlURL = "https://raw.githubusercontent.com/open-telemetry/%s/refs/tags/v%s/versions.yaml"

const (
	otelCoreRepo    = "opentelemetry-collector"
	otelContribRepo = "opentelemetry-collector-contrib"
)

// otelModuleVersions maps a Go module path to the version that module was
// released at, i.e. go.opentelemetry.io/collector/pdata -> v1.67.0.
type otelModuleVersions map[string]string

// otelVersionsYaml reflects the versions.yaml of an OpenTelemetry repository.
// Modules are grouped into module sets that are released together, so a
// repository release covers several versions at once: an unstable set tagged
// v0.x.x alongside one or more stable sets tagged v1.x.x.
type otelVersionsYaml struct {
	ModuleSets map[string]struct {
		Version string   `yaml:"version"`
		Modules []string `yaml:"modules"`
	} `yaml:"module-sets"`
}

// ResolveOTelModuleVersions reads the versions.yaml of the core and contrib
// repositories at the given releases, so that every component can be tagged
// with the version it was actually released at. This is what allows a spec to
// declare only the repository releases it is based on, rather than restating
// the version of each stable module set.
func (r *Registry) ResolveOTelModuleVersions(coreVersion string, contribVersion string) error {
	moduleVersions, err := resolveOTelModuleVersions(coreVersion, contribVersion)
	if err != nil {
		return err
	}
	r.moduleVersions = moduleVersions
	return nil
}

// resolveOTelModuleVersions collects the module versions published by the core
// and contrib repositories. Either release may be empty, in which case that
// repository is skipped.
func resolveOTelModuleVersions(coreVersion string, contribVersion string) (otelModuleVersions, error) {
	moduleVersions := otelModuleVersions{}

	for _, repo := range []struct {
		name    string
		version string
	}{
		{otelCoreRepo, coreVersion},
		{otelContribRepo, contribVersion},
	} {
		if repo.version == "" {
			continue
		}
		versions, err := fetchOTelModuleVersions(repo.name, repo.version)
		if err != nil {
			return nil, err
		}
		mapMerge(moduleVersions, versions)
	}

	return moduleVersions, nil
}

// fetchOTelModuleVersions reads the versions.yaml of an OpenTelemetry
// repository at the given release. The release may be given either as a bare
// version or as a tag, i.e. both 0.161.0 and v0.161.0 are accepted.
func fetchOTelModuleVersions(repo string, version string) (otelModuleVersions, error) {
	url := fmt.Sprintf(otelVersionsYamlURL, repo, strings.TrimPrefix(version, "v"))
	logger.Debug("fetching module versions", slog.String("url", url))

	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("could not fetch %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not fetch %s: %s", url, response.Status)
	}

	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", url, err)
	}

	versions, err := parseOTelModuleVersions(content)
	if err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", url, err)
	}
	return versions, nil
}

// parseOTelModuleVersions flattens the module sets of a versions.yaml document
// into a lookup of module path to the version that module is released at.
func parseOTelModuleVersions(content []byte) (otelModuleVersions, error) {
	var versionsYaml otelVersionsYaml
	if err := yaml.Unmarshal(content, &versionsYaml); err != nil {
		return nil, err
	}

	versions := otelModuleVersions{}
	for _, moduleSet := range versionsYaml.ModuleSets {
		for _, module := range moduleSet.Modules {
			versions[module] = moduleSet.Version
		}
	}
	return versions, nil
}
