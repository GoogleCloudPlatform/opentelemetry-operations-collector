package main

import (
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed registry.yaml
var registryContent []byte

var ErrComponentNotFound = errors.New("component not found")

type Registry struct {
	Receivers  RegistryList `yaml:"receivers"`
	Processors RegistryList `yaml:"processors"`
	Exporters  RegistryList `yaml:"exporters"`
	Connectors RegistryList `yaml:"connectors"`
	Extensions RegistryList `yaml:"extensions"`
	Providers  RegistryList `yaml:"providers"`
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
	mapMerge(r.Providers, r2.Providers)
}

type RegistryList map[string]*RegistryComponent

func (rl RegistryList) Load(name string) (*RegistryComponent, error) {
	entry, ok := rl[name]
	if !ok {
		return nil, ErrComponentNotFound
	}
	return entry, nil
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

func (rl RegistryList) LoadAll(names []string, version string, stableVersion string, contribVersion string) (RegistryComponents, RegistryLoadError) {
	components := RegistryComponents{}
	errs := make(RegistryLoadError)

	for _, name := range names {
		entry, err := rl.Load(name)
		if err != nil {
			errs[name] = err
			continue
		}

		entry.GoMod.Tag = "v" + version
		if entry.Stable {
			entry.GoMod.Tag = "v" + stableVersion
		} else if entry.IsContrib() {
			entry.GoMod.Tag = "v" + contribVersion
		}
		components[name] = entry
	}

	return components, errs
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

// RegistryComponent is the type used as a basis for Registry.
// It contains all the information needed to output a
type RegistryComponent struct {
	GoMod         *GoModuleID `yaml:"gomod"`
	Import        string      `yaml:"import,omitempty"`
	Name          string      `yaml:"string,omitempty"`
	Path          string      `yaml:"path,omitempty"`
	Stable        bool        `yaml:"stable,omitempty"`
	StartRevision string      `yaml:"start_revision,omitempty"`
	DocsURL       string      `yaml:"docs_url"`
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

// RegistryComponents is a map of registry component names to component
// details.
type RegistryComponents map[string]*RegistryComponent

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
