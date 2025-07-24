package main

import (
	"io/fs"
	"os"
)

type Distrogen struct {
	Spec     *DistributionSpec
	FileMode fs.FileMode
}

func NewDistrogenGenerator(spec *DistributionSpec) (*Distrogen, error) {
	return &Distrogen{
		Spec:     spec,
		FileMode: DefaultProjectFileMode,
	}, nil
}

func (ig *Distrogen) Generate() error {
	distrogenTemplateSet, err := GetDistrogenTemplateSet(ig, ig.FileMode)
	if err != nil {
		logger.Debug("failed to get component templates", "err", err)
		return err
	}

	if err := os.MkdirAll(".distrogen", ig.FileMode); err != nil {
		return err
	}

	if err := GenerateTemplateSet(".distrogen", distrogenTemplateSet); err != nil {
		return err
	}

	return nil
}
