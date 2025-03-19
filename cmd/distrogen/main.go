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
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
)

var (
	logLevel            = new(slog.LevelVar)
	logger              = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	unexpectErrExitCode = 2
)

func main() {
	flag.Parse()
	var exitCodeErr *ExitCodeError
	if err := run(); err != nil {
		if errors.Is(err, ErrNoDiff) {
			// No diff means we just want to log the error
			// but not exit with code 1.
			log.Println(err)
		} else if errors.As(err, &exitCodeErr) {
			logger.Error(fmt.Sprintf("unexpected error: %v", err))
			os.Exit(exitCodeErr.exitCode)
		} else {
			log.Fatal(err)
		}
	}
}

func run() error {
	if *flagVerbose {
		logLevel.Set(slog.LevelDebug)
	}

	if *flagQuery != "" {
		return querySpec()
	}
	if *flagOtelConfig != "" {
		return generateSpec()
	}
	return generateDistribution()
}

func generateSpec() error {
	distro := &DistributionSpec{}
	otelConfigMap, err := yamlUnmarshalFromFile[map[string]any](*flagOtelConfig)
	if err != nil {
		return err
	}
	distro.Components, err = ComponentsFromOTelConfig(*otelConfigMap)
	if err != nil {
		return err
	}
	return yamlMarshalToFile(distro, "generated_spec.yaml", DefaultFileMode)
}

func querySpec() error {
	if *flagSpec == "" {
		return errNoSpecFlag
	}

	spec, err := NewDistributionSpec(*flagSpec)
	if err != nil {
		return err
	}

	val, err := spec.Query(*flagQuery)
	if err != nil {
		return err
	}
	// Using Println instead of logger since the results
	// may be piped to another program.
	fmt.Println(val)
	return nil
}

func generateDistribution() error {
	specPath := *flagSpec
	if *flagSpec == "" {
		return errNoSpecFlag
	}

	spec, err := NewDistributionSpec(specPath)
	if err != nil {
		return err
	}

	registry, err := LoadEmbeddedRegistry()
	if err != nil {
		return err
	}

	for _, registryPath := range *flagRegistry {
		additionalRegistry, err := LoadRegistry(registryPath)
		if err != nil {
			return err
		}
		registry.Merge(additionalRegistry)
	}

	generator, err := NewDistributionGenerator(spec, registry, *flagForce)
	if err != nil {
		return err
	}
	defer generator.Clean()

	if *flagCustomTemplates != "" {
		generator.CustomTemplatesDir = os.DirFS(*flagCustomTemplates)
	}

	if err := generator.Generate(); err != nil {
		return err
	}

	var resultErr error
	if *flagCompare {
		resultErr = generator.Compare()
	} else {
		resultErr = generator.MoveGeneratedDirToWd()
	}

	return resultErr
}
