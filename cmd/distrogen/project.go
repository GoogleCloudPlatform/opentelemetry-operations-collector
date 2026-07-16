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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	DistrogenMetadataDir   = ".distrogen"
	DefaultProjectFileMode = fs.ModePerm
)

var ProjectSpecCachePath = filepath.Join(DistrogenMetadataDir, "spec.yaml")

type ProjectGenerator struct {
	Spec                    *DistributionSpec
	CustomPath              string
	CurrentDistrogenVersion string

	fileMode     fs.FileMode
	cleanTools   bool
	generatePath string
}

func NewProjectGenerator(spec *DistributionSpec) (*ProjectGenerator, error) {
	pg := &ProjectGenerator{
		Spec:                    spec,
		CurrentDistrogenVersion: Version,

		fileMode:     DefaultProjectFileMode,
		generatePath: ".",
	}

	cachedSpec, err := pg.ReadCachedSpec()
	if err != nil {
		if !os.IsNotExist(err) {
			// This is not a fatal error like it is with distribution generation.
			logger.Debug("failed to read cached spec", "err", err)
		} else {
			logger.Debug("no cached spec")
		}
	} else {
		pg.cleanTools = pg.Spec.DetectProjectToolChange(cachedSpec)
	}

	return pg, nil
}

func (pg *ProjectGenerator) Generate() error {
	makeTemplates, err := GetMakeTemplateSet(pg, pg.fileMode)
	if err != nil {
		logger.Debug("failed to get make templates", "err", err)
		return err
	}
	projectTemplates, err := GetProjectTemplateSet(pg, pg.fileMode)
	if err != nil {
		logger.Debug("failed to get project templates", "err", err)
		return err
	}
	distrogenTemplateSet, err := GetDistrogenTemplateSet(pg, pg.fileMode)
	if err != nil {
		logger.Debug("failed to get component templates", "err", err)
		return err
	}
	scriptTemplateSet, err := GetScriptTemplateSet(pg, pg.fileMode)
	if err != nil {
		logger.Debug("failed to get script templates", "err", err)
		return err
	}

	crg := NewComponentsRegistryGenerator()

	if pg.CustomPath != "" {
		pg.generatePath = pg.CustomPath
		crg.Path = pg.generatePath
	}

	if err := crg.Generate(); err != nil {
		return err
	}

	generateMakePath := filepath.Join(pg.generatePath, "make")
	generateScriptsPath := filepath.Join(pg.generatePath, "scripts")

	var dirErrors []error
	if err := os.MkdirAll(generateMakePath, pg.fileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if err := os.MkdirAll(filepath.Join(pg.generatePath, "templates"), pg.fileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if err := os.MkdirAll(generateScriptsPath, pg.fileMode); err != nil {
		dirErrors = append(dirErrors, err)
	}
	if _, err := os.Create(filepath.Join(pg.generatePath, "templates", EMPTY_FILE_NAME)); err != nil {
		dirErrors = append(dirErrors, err)
	}

	if len(dirErrors) > 0 {
		return errors.Join(dirErrors...)
	}

	if err := GenerateTemplateSet(generateMakePath, makeTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(generateScriptsPath, scriptTemplateSet); err != nil {
		return err
	}
	if err := GenerateTemplateSet(filepath.Join(pg.generatePath, "."), projectTemplates); err != nil {
		return err
	}
	if err := GenerateTemplateSet(filepath.Join(pg.generatePath, DistrogenMetadataDir), distrogenTemplateSet); err != nil {
		return err
	}

	if pg.cleanTools {
		toolsDirPath := filepath.Join(pg.generatePath, ToolsDir)
		if err := os.RemoveAll(toolsDirPath); err != nil {
			return fmt.Errorf("failed to clean dir %s: %w", toolsDirPath, err)
		}
	}

	if err := pg.CacheSpec(); err != nil {
		// This time we are logging at info level because the user needs
		// to know regardless of if we're in verbose mode or not. But it
		// is not a fatal error; certain things just won't work.
		logger.Warn(
			"Failed to cache spec. This means automatic tool reinstall detection won't work.",
			"err",
			err,
		)
	}

	return nil
}

func (pg *ProjectGenerator) CacheSpec() error {
	return yamlMarshalToFile(pg.Spec, filepath.Join(pg.generatePath, ProjectSpecCachePath), DefaultProjectFileMode)
}

func (pg *ProjectGenerator) ReadCachedSpec() (*DistributionSpec, error) {
	return yamlUnmarshalFromFile[DistributionSpec](filepath.Join(pg.generatePath, ProjectSpecCachePath))
}
