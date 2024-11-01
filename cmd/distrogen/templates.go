package main

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates/*
var templatesFS embed.FS

type TemplateFile struct {
	Name    string
	Context any
}

func (tf TemplateFile) getTextTemplate() (*template.Template, error) {
	templateFileName := fmt.Sprintf("%s.go.tmpl", tf.Name)
	tmpl, err := template.
		New(templateFileName).
		ParseFS(templatesFS, filepath.Join("templates", templateFileName))
	if err != nil {
		return nil, err
	}
	return tmpl, err
}

func (tf TemplateFile) Render(outDir string) error {
	tmpl, err := tf.getTextTemplate()
	if err != nil {
		return err
	}
	buf := bytes.Buffer{}
	if err := tmpl.Execute(&buf, tf.Context); err != nil {
		return err
	}
	outPath := filepath.Join(outDir, tf.Name)
	return os.WriteFile(outPath, buf.Bytes(), fs.ModePerm)
}

type ManifestContext struct {
	*DistributionSpec

	Receivers  OCBManifestComponents
	Processors OCBManifestComponents
	Exporters  OCBManifestComponents
	Extensions OCBManifestComponents
	Connectors OCBManifestComponents
}

func NewManifestContextFromSpec(spec *DistributionSpec, registry *Registry) (*ManifestContext, error) {
	context := ManifestContext{DistributionSpec: spec}

	errs := make(RegistryLoadError)
	var err RegistryLoadError
	context.Receivers, err = registry.Receivers.LoadAll(spec.Components.Receivers, spec.OpenTelemetryVersion)
	mapMerge(errs, err)
	context.Processors, errs = registry.Processors.LoadAll(spec.Components.Processors, spec.OpenTelemetryVersion)
	mapMerge(errs, err)
	context.Exporters, errs = registry.Exporters.LoadAll(spec.Components.Exporters, spec.OpenTelemetryVersion)
	mapMerge(errs, err)
	context.Connectors, errs = registry.Connectors.LoadAll(spec.Components.Connectors, spec.OpenTelemetryVersion)
	mapMerge(errs, err)
	context.Extensions, errs = registry.Extensions.LoadAll(spec.Components.Extensions, spec.OpenTelemetryVersion)
	mapMerge(errs, err)

	if len(errs) > 0 {
		return nil, errs
	}
	return &context, nil
}
