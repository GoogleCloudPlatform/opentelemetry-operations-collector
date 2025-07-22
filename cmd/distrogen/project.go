package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
)

var (
	DefaultProjectFileMode = fs.ModePerm
)

type ProjectGenerator struct {
	Spec     *DistributionSpec
	FileMode fs.FileMode
}

func NewProjectGenerator(spec *DistributionSpec) (*ProjectGenerator, error) {
	return &ProjectGenerator{
		Spec:     spec,
		FileMode: DefaultProjectFileMode,
	}, nil
}

func (pg *ProjectGenerator) generateSet(outDir string, templateSet TemplateSet) error {
	for _, tmpl := range templateSet {
		if err := tmpl.Render(outDir); err != nil {
			logger.Debug(fmt.Sprintf("failed to render %s", tmpl.Name), "err", err)
			return err
		}
	}
	return nil
}

func (pg *ProjectGenerator) Generate() error {
	componentTemplates, err := GetComponentsTemplateSet(pg, pg.FileMode)
	if err != nil {
		logger.Debug("failed to get component templates", "err", err)
		return err
	}
	makeTemplates, err := GetMakeTemplateSet(pg, pg.FileMode)
	if err != nil {
		logger.Debug("failed to get make templates", "err", err)
		return err
	}
	projectTemplates, err := GetProjectTemplateSet(pg, pg.FileMode)
	if err != nil {
		logger.Debug("failed to get project templates", "err", err)
		return err
	}

	var dirErrors []error
	if err := os.MkdirAll("components", pg.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if err := os.MkdirAll("make", pg.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if err := os.MkdirAll("templates", pg.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if _, err := os.Create(path.Join("templates", EMPTY_FILE_NAME)); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if len(dirErrors) > 0 {
		return errors.Join(dirErrors...)
	}

	// var renderErr error

	if err := pg.generateSet("components", componentTemplates); err != nil {
		return err
	}
	if err := pg.generateSet("make", makeTemplates); err != nil {
		return err
	}
	if err := pg.generateSet(".", projectTemplates); err != nil {
		return err
	}

	return nil

	// renderFail:
	//
	//	errs := []error{renderErr}
	//	if err := os.RemoveAll("components"); err != nil {
	//		logger.Debug("failed to remove components dir", "err", err)
	//		errs = append(errs, err)
	//	}
	//	if err := os.RemoveAll("make"); err != nil {
	//		logger.Debug("failed to remove make dir", "err", err)
	//		errs = append(errs, err)
	//	}
	//	return errors.Join(errs...)
}
