package main

import (
	"fmt"
	"io/fs"
	"os"
)

type InternalGenerator struct {
	Spec     *DistributionSpec
	FileMode fs.FileMode
	Tools    []string
}

func NewInternalGenerator(spec *DistributionSpec, tools []string) (*InternalGenerator, error) {
	return &InternalGenerator{
		Spec:     spec,
		FileMode: DefaultProjectFileMode,
		Tools:    append(tools, fmt.Sprintf("github.com/GoogleCloudPlatform/opentelemetry-operations-collector/cmd/distrogen@%s", spec.DistrogenVersion)),
	}, nil
}

func (ig *InternalGenerator) Generate() error {
	internalTemplates, err := GetInternalTemplateSet(ig, ig.FileMode)
	if err != nil {
		logger.Debug("failed to get component templates", "err", err)
		return err
	}

	if err := os.MkdirAll("internal", ig.FileMode); err != nil {
		return err
	}

	if err := GenerateTemplateSet("internal", internalTemplates); err != nil {
		return err
	}

	return nil
}
