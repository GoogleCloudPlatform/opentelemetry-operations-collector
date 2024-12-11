package main

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed registry.yaml
var registryContent []byte

var ErrComponentNotFound = errors.New("component not found")

type GoModuleID struct {
	URL           string
	Tag           string
	AllowBlankTag bool
}

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
		tag = "v0.0.0"
	}
	return fmt.Sprintf("%s %s", gm.URL, tag)
}

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

func (gm *GoModuleID) MarshalYAML() (interface{}, error) {
	return gm.String(), nil
}

type OCBManifestComponent struct {
	GoMod  *GoModuleID `yaml:"gomod"`
	Import string      `yaml:"import,omitempty"`
	Name   string      `yaml:"string,omitempty"`
	Path   string      `yaml:"path,omitempty"`
}

type OCBManifestComponents []*OCBManifestComponent

// Validate is intended to be called before template rendering.
// This way, calling the Render method from the template can assume
// no error.
func (cs OCBManifestComponents) Validate() error {
	_, err := yaml.Marshal(cs)
	return err
}

// Render outputs the OCBManifestComponents array as a yaml
// string. Used in manifest.yaml.go.tmpl.
func (cs OCBManifestComponents) Render() string {
	if len(cs) == 0 {
		return ""
	}
	content, _ := yaml.Marshal(cs)
	return string(content)
}

type OCBManifestReplace struct {
	From   *GoModuleID `yaml:"from"`
	To     *GoModuleID `yaml:"to"`
	Reason string      `yaml:"reason"`
}

func (r *OCBManifestReplace) String() string {
	r.From.AllowBlankTag = true
	r.To.AllowBlankTag = true
	return fmt.Sprintf("# %s\n- %s => %s", r.Reason, r.From, r.To)
}

type OCBManifestReplaces []*OCBManifestReplace

func (rs OCBManifestReplaces) Render() string {
	result := ""
	for _, r := range rs {
		result += fmt.Sprintf("%s\n", r)
	}
	return result
}

type RegistryList map[string]*OCBManifestComponent

func (rl RegistryList) Load(name string) (*OCBManifestComponent, error) {
	component, ok := rl[name]
	if !ok {
		return nil, ErrComponentNotFound
	}
	return component, nil
}

type RegistryLoadError map[string]error

func (e RegistryLoadError) Error() string {
	msg := ""
	for name, err := range e {
		combinedErr := fmt.Errorf("%s: %w", name, err)
		msg += fmt.Sprintf("%v\n", combinedErr)
	}
	return msg
}

func (rl RegistryList) LoadAll(names []string, version string) (OCBManifestComponents, RegistryLoadError) {
	components := OCBManifestComponents{}
	errs := make(RegistryLoadError)

	for _, name := range names {
		component, err := rl.Load(name)
		if err != nil {
			errs[name] = err
		} else {
			component.GoMod.Tag = "v" + version
			components = append(components, component)
		}
	}

	return components, errs
}

type Registry struct {
	Receivers  RegistryList `yaml:"receivers"`
	Processors RegistryList `yaml:"processors"`
	Exporters  RegistryList `yaml:"exporters"`
	Connectors RegistryList `yaml:"connectors"`
	Extensions RegistryList `yaml:"extensions"`
}

func LoadEmbeddedRegistry() (*Registry, error) {
	var r Registry
	if err := yaml.Unmarshal(registryContent, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func LoadRegistry(path string) (*Registry, error) {
	return yamlUnmarshalFromFile[Registry](path)
}

func (r *Registry) Merge(r2 *Registry) {
	mapMerge(r.Receivers, r2.Receivers)
	mapMerge(r.Processors, r2.Processors)
	mapMerge(r.Exporters, r2.Exporters)
	mapMerge(r.Connectors, r2.Connectors)
	mapMerge(r.Extensions, r2.Extensions)
}