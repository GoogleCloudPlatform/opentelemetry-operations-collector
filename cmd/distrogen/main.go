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
	logLevel = new(slog.LevelVar)
	logger   = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		if errors.Is(err, ErrNoDiff) {
			// No diff means we just want to log the error
			// but not exit with code 1.
			log.Println(err)
		} else {
			log.Fatal(err)
		}
	}
}

func run() error {
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
	return yamlMarshalToFile(distro, "generated_spec.yaml")
}

func generateDistribution() error {
	specPath := *flagSpec
	if *flagSpec == "" {
		return errNoSpecFlag
	}

	if *flagVerbose {
		logLevel.Set(slog.LevelDebug)
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

	if *flagCustomTemplates != "" {
		generator.CustomTemplatesDir = os.DirFS(*flagCustomTemplates)
	}

	if err := generator.Generate(); err != nil {
		if err := generator.Clean(); err != nil {
			fmt.Printf("couldn't clean generated dir: %v\n", err)
		}
		return err
	}
	if err := generator.MoveGeneratedDirToWd(); err != nil {
		if err := generator.Clean(); err != nil {
			fmt.Printf("couldn't clean generated dir: %v\n", err)
		}
		return err
	}

	return nil
}
