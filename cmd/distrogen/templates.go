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

// TemplateFile is the information about a template file
// that will be rendered for a distribution.
type TemplateFile struct {
	Name     string
	FileName string
	Context  any
	FS       fs.FS
}

// getTextTemplate retrieves the text tempalte from the template file's
// provided filesystem. This may be the embedded filesystem or another
// set provided by the user.
func (tf *TemplateFile) getTextTemplate() (*template.Template, error) {
	return template.
		New(tf.FileName).
		ParseFS(tf.FS, tf.FileName)
}

// Render will render the template into a file in the
// requested destination.
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

var (
	ErrInvalidTemplateName = errors.New("invalid template name, must end with .go.tmpl")
	ErrTemplateNotFound    = errors.New("template not found")
)

// TemplateSet is a map of template names to a template file.
type TemplateSet map[string]*TemplateFile

// AddTemplate will add a template to the template set. If a template
// is added with a name that already exists, AddTemplate will overwrite
// the template it has.
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

// GetTemplate will retrieve a template from the template set.
func (ts TemplateSet) GetTemplate(name string) (*TemplateFile, error) {
	tf, ok := ts[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	return tf, nil
}

// GetTemplateSetFromDir will walk an FS for any *.go.tmpl files and
// will collect them into a TemplateSet.
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

// GetEmbeddedTemplateSet will get the template set from the template FS embedded
// into the distrogen binary.
func GetEmbeddedTemplateSet(templateContext any) (TemplateSet, error) {
	embeddedTemplatesSubFS, err := fs.Sub(embeddedTemplatesFS, "templates")
	if err != nil {
		return nil, err
	}
	return GetTemplateSetFromDir(embeddedTemplatesSubFS, templateContext)
}
