package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	DefaultProjectFileMode = fs.ModePerm
)

type ProjectGenerator struct {
	Spec       *DistributionSpec
	FileMode   fs.FileMode
	CustomPath string
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

	generatePath := "."
	if pg.CustomPath != "" {
		generatePath = pg.CustomPath
	}

	generateComponentsPath := filepath.Join(generatePath, "components")
	generateMakePath := filepath.Join(generatePath, "make")

	var dirErrors []error
	if err := os.MkdirAll(generateComponentsPath, pg.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if err := os.MkdirAll(generateMakePath, pg.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if err := os.MkdirAll(filepath.Join(generatePath, "templates"), pg.FileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if _, err := os.Create(filepath.Join(generatePath, "templates", EMPTY_FILE_NAME)); err != nil {
		dirErrors = append(dirErrors, err)
	}

	if err := os.MkdirAll(filepath.Join(generatePath, ".distrogen"), pg.FileMode); err != nil {
		return err
	}
	if len(dirErrors) > 0 {
		return errors.Join(dirErrors...)
	}

	if err := GenerateTemplateSet(generateComponentsPath, componentTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(generateMakePath, makeTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(filepath.Join(generatePath, "."), projectTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(filepath.Join(generatePath, ".distrogen"), distrogenTemplateSet); err != nil {
		return err
	}

	return nil
}
