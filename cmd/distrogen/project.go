package main

import (
	"errors"
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
	distrogenTemplateSet, err := GetDistrogenTemplateSet(pg, pg.FileMode)
	if err != nil {
		logger.Debug("failed to get component templates", "err", err)
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
	if err := os.MkdirAll(".distrogen", pg.FileMode); err != nil {
		return err
	}
	if len(dirErrors) > 0 {
		return errors.Join(dirErrors...)
	}

	if err := GenerateTemplateSet("components", componentTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet("make", makeTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(".", projectTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(".distrogen", distrogenTemplateSet); err != nil {
		return err
	}

	return nil
}
