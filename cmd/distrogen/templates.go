package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/*
var embeddedTemplatesFS embed.FS

type TemplateFile struct {
	Name     string
	FileName string
	Context  any
	FS       fs.FS
}

func (tf *TemplateFile) getTextTemplate() (*template.Template, error) {
	return template.
		New(tf.FileName).
		ParseFS(tf.FS, tf.FileName)
}

func (tf *TemplateFile) Render(outDir string) error {
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

func (tf *TemplateFile) getTemplateFileName() string {
	return fmt.Sprintf("%s.go.tmpl", tf.Name)
}

var (
	ErrInvalidTemplateName = errors.New("invalid template name, must end with .go.tmpl")
	ErrTemplateNotFound    = errors.New("template not found")
)

type TemplateSet map[string]*TemplateFile

func (ts TemplateSet) AddTemplate(name string, templateContext any, dir fs.FS) error {
	if !strings.HasSuffix(name, ".go.tmpl") {
		return fmt.Errorf("%w: %s", ErrInvalidTemplateName, name)
	}
	ts[name] = &TemplateFile{
		FileName: name,
		Name:     strings.TrimSuffix(name, ".go.tmpl"),
		Context:  templateContext,
		FS:       dir,
	}
	return nil
}

func (ts TemplateSet) GetTemplate(name string) (*TemplateFile, error) {
	tf, ok := ts[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	return tf, nil
}

func (ts TemplateSet) SetTemplateContext(name string, templateContext any) error {
	tf, err := ts.GetTemplate(name)
	if err != nil {
		return err
	}
	tf.Context = templateContext
	return nil
}

func GetTemplateSetFromDir(dir fs.FS, templateContext any) (TemplateSet, error) {
	templates := TemplateSet{}

	err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".go.tmpl") {
			return nil
		}
		return templates.AddTemplate(d.Name(), templateContext, dir)
	})

	return templates, err
}

func GetEmbeddedTemplateSet(templateContext any) (TemplateSet, error) {
	embeddedTemplatesSubFS, err := fs.Sub(embeddedTemplatesFS, "templates")
	if err != nil {
		return nil, err
	}
	return GetTemplateSetFromDir(embeddedTemplatesSubFS, templateContext)
}
