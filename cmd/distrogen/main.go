package main

import (
	"errors"
	"flag"
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
			log.Println(err)
		} else {
			log.Fatal(err)
		}
	}
}

func run() error {
	specPath := *flagSpec
	if *flagSpec == "" {
		return errNoSpecFlag
	}

	if *flagVerbose {
		logLevel.Set(slog.LevelDebug)
	}

	spec, err := yamlUnmarshalFromFile[DistributionSpec](specPath)
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

	if err := generator.Generate(); err != nil {
		return err
	}
	if err := generator.MoveGeneratedDirToWd(); err != nil {
		return err
	}

	return nil
}
