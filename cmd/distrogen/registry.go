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

	"github.com/go-viper/mapstructure/v2"
	"github.com/goccy/go-yaml"
)

//go:embed registry.yaml
var registryContent []byte

var ErrComponentNotFound = errors.New("component not found")

type ReleaseInfo struct {
	Version                       string            `yaml:"version,omitempty"`
	OpenTelemetryCollectorVersion string            `yaml:"opentelemetry_collector_version,omitempty"`
	Replaces                      ComponentReplaces `yaml:"replaces,omitempty"`
}

// RegistryConfig is a collection of components that can be used in
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
type RegistryConfig struct {
	Release    ReleaseInfo        `yaml:"release,omitempty"`
	Receivers  RegistryComponents `yaml:"receivers"`
	Processors RegistryComponents `yaml:"processors"`
	Exporters  RegistryComponents `yaml:"exporters"`
	Connectors RegistryComponents `yaml:"connectors"`
	Extensions RegistryComponents `yaml:"extensions"`
	Providers  RegistryComponents `yaml:"providers"`
	Path       string             `yaml:"-"`
}

func (rc *RegistryConfig) MakeRegistry() *Registry {
	registry := NewRegistry()
	if rc.Receivers != nil {
		registry.Components[Receiver] = rc.Receivers
	}
	if rc.Processors != nil {
		registry.Components[Processor] = rc.Processors
	}
	if rc.Exporters != nil {
		registry.Components[Exporter] = rc.Exporters
	}
	if rc.Connectors != nil {
		registry.Components[Connector] = rc.Connectors
	}
	if rc.Extensions != nil {
		registry.Components[Extension] = rc.Extensions
	}
	if rc.Providers != nil {
		registry.Components[Provider] = rc.Providers
	}
	registry.OpenTelemetryVersions = &otelComponentVersion{
		core:    rc.Release.OpenTelemetryCollectorVersion,
		contrib: rc.Release.OpenTelemetryCollectorVersion,
	}
	registry.Replaces = rc.Release.Replaces
	registry.Version = rc.Release.Version
	return registry
}

var AllComponentTypes = []ComponentType{Receiver, Processor, Exporter, Connector, Extension, Provider}

type RegistryComponentCollection map[ComponentType]RegistryComponents

func NewRegistryComponentCollection() RegistryComponentCollection {
	collection := RegistryComponentCollection{}
	for _, t := range AllComponentTypes {
		collection[t] = RegistryComponents{}
	}
	return collection
}

type Registry struct {
	Components            RegistryComponentCollection `yaml:"components"`
	OpenTelemetryVersions *otelComponentVersion       `yaml:"opentelemetry_versions,omitempty"`
	Version               string                      `yaml:"version,omitempty"`
	Replaces              ComponentReplaces           `yaml:"replaces,omitempty"`
	Path                  string                      `yaml:"-"`
	Used                  bool                        `yaml:"-"`
}

func NewRegistry() *Registry {
	return &Registry{Components: NewRegistryComponentCollection()}
}

func (r *Registry) LookupComponent(componentType ComponentType, name string) (*RegistryComponent, error) {
	if _, ok := r.Components[componentType]; !ok {
		panic(fmt.Sprintf("Invalid component type codepath found, requested componentType was %s. Please report this to maintainers if you see this message.", componentType))
	}
	component, err := r.Components[componentType].LoadComponent(name)
	if err != nil {
		return nil, err
	}

	if r.Version != "" {
		component.ApplyVersion(r.Version)
	} else {
		component.ApplyOTelVersion(r.OpenTelemetryVersions)
	}
	return component, nil
}

func (r *Registry) LoadAllComponents(componentType ComponentType, names []string) (RegistryComponents, CollectionError) {
	components := RegistryComponents{}
	errs := CollectionError{}
	for _, name := range names {
		component, err := r.LookupComponent(componentType, name)
		if err != nil {
			errs[name] = err
			continue
		}
		components[name] = component
		r.Used = true
	}
	return components, errs
}

type Registries []*Registry

func (rs Registries) LookupComponent(componentType ComponentType, name string) (*RegistryComponent, error) {
	var component *RegistryComponent
	var err error
	for _, r := range rs {
		component, err = r.LookupComponent(componentType, name)
		if err == nil {
			r.Used = true
			return component, nil
		}
	}
	return nil, ErrComponentNotFound
}

func (rs Registries) LoadAllComponents(componentType ComponentType, names []string) (RegistryComponents, CollectionError) {
	components := RegistryComponents{}
	errs := CollectionError{}
	for _, name := range names {
		component, err := rs.LookupComponent(componentType, name)
		if err != nil {
			errs[name] = err
			continue
		}
		components[name] = component
	}
	return components, errs
}

// LoadEmbeddedRegistry will load the registry embedded in the
// distrogen binary.
func LoadEmbeddedRegistry() (*Registry, error) {
	r := &RegistryConfig{}
	if err := yaml.Unmarshal(registryContent, r); err != nil {
		return nil, err
	}
	return r.MakeRegistry(), nil
}

// LoadRegistry will load a registry from a yaml file.
func LoadRegistry(path string) (*Registry, error) {
	r := &RegistryConfig{}
	if err := yamlUnmarshalFromFileInto(path, r); err != nil {
		return nil, err
	}
	return r.MakeRegistry(), nil
}

func (r *Registry) Add(componentType ComponentType, component *RegistryComponent) {
	r.Components[componentType][component.Name] = component
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

// UnmarshalYAML implements the yaml.BytesUnmarshaler interface.
// It takes a properly formed Go Module ID string and unpacks
// it into the struct.
func (gm *GoModuleID) UnmarshalYAML(b []byte) error {
	// The module ID may have a version.
	moduleStr := strings.TrimSpace(string(b))
	moduleComponents := strings.Split(moduleStr, " ")
	gm.URL = moduleComponents[0]
	if len(moduleComponents) > 1 {
		gm.Tag = moduleComponents[1]
	}
	return nil
}

// MarshalYAML implements the yaml.BytesMarshaler interface. It leverages
// the String method to allow outputting the value into a YAML document
// in the module ID string form.
func (gm *GoModuleID) MarshalYAML() ([]byte, error) {
	return []byte(gm.String()), nil
}

type otelComponentVersion struct {
	core       string
	coreStable string
	contrib    string
}

// RegistryComponent is the type used as a basis for Registry.
// It contains all the information needed to output a
type RegistryComponent struct {
	Name string `yaml:"-"`

	GoMod         *GoModuleID `yaml:"gomod"`
	Import        string      `yaml:"import,omitempty"`
	Path          string      `yaml:"path,omitempty"`
	Stable        bool        `yaml:"stable,omitempty"`
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

func (c *RegistryComponent) ApplyOTelVersion(otelVersion *otelComponentVersion) {
	if otelVersion == nil {
		return
	}
	c.GoMod.Tag = "v" + otelVersion.core
	if c.Stable {
		c.GoMod.Tag = "v" + otelVersion.coreStable
	} else if c.IsContrib() {
		c.GoMod.Tag = "v" + otelVersion.contrib
	}
}

func (c *RegistryComponent) ApplyVersion(version string) {
	c.GoMod.Tag = "v" + version
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

func (rcs RegistryComponents) LoadComponent(name string) (*RegistryComponent, error) {
	entry, ok := rcs[name]
	if !ok {
		return nil, ErrComponentNotFound
	}
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

type CustomRegistrySource string

const (
	SourceLocal  CustomRegistrySource = "local"
	SourceGitHub CustomRegistrySource = "github"
)

type CustomRegistry struct {
	Name           string               `mapstructure:"name"`
	Source         CustomRegistrySource `mapstructure:"source"`
	RegistryConfig map[string]any       `mapstructure:",remain"`
}

// UnmarshalYAML implements the `yaml.BytesUnmarshaler` interface. It is
// to allow decoding using `mapstructure` to leverage the `remain` tag.
func (c *CustomRegistry) UnmarshalYAML(b []byte) error {
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return err
	}

	return mapstructure.Decode(raw, c)
}

func (c *CustomRegistry) GetRegistryConfig() (*RegistryConfig, error) {
	var loader RegistryLoader
	switch c.Source {
	case SourceLocal:
		var localLoader LocalRegistryLoader
		if err := mapstructure.Decode(c.RegistryConfig, &localLoader); err != nil {
			return nil, err
		}
		loader = &localLoader
	case SourceGitHub:
		var githubLoader GithubRegistryLoader
		if err := mapstructure.Decode(c.RegistryConfig, &githubLoader); err != nil {
			return nil, err
		}
		loader = &githubLoader
	default:
		return nil, fmt.Errorf("unknown registry source: %s", c.Source)
	}
	return loader.LoadRegistryConfig()
}

type RegistryLoader interface {
	LoadRegistryConfig() (*RegistryConfig, error)
}

type LocalRegistryLoader struct {
	Path string `mapstructure:"path"`
}

func (l *LocalRegistryLoader) LoadRegistryConfig() (*RegistryConfig, error) {
	r := &RegistryConfig{}
	err := yamlUnmarshalFromFileInto(l.Path, r)
	return r, err
}

type GithubRegistryLoader struct {
	Repo     string `mapstructure:"repo"`
	Revision string `mapstructure:"revision"`
	Path     string `mapstructure:"path"`
}

func (l *GithubRegistryLoader) LoadRegistryConfig() (*RegistryConfig, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", l.Repo, l.Revision, l.Path)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("could not retrieve registry from github: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	r := RegistryConfig{}
	if err := yaml.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
