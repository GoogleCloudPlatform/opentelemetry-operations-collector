package main

import (
	"flag"
	"testing"
)

func TestHostmetrics(t *testing.T) {
	// Make a few tweaks to config-example.yaml
	config := readConfig("config-example.yaml")
	writeConfig(adjustConfig(config), "new-config.yaml")
	flag.Set("config", "new-confg.yaml")

	// Run the main function of otelopscol.
	// TODO: set a 20 second timeout on the passed context.
	mainContext(ctx)

	// Example: https://paste.googleplex.com/5536046585479168
	exportedData := readFile("metrics.json")
	// This file will bear a strong resemblance to config_example.yaml
	// from the ops-agent repo.
	expectations := readFile("hostmetrics_expectations.yaml")

	// Logic for applying the assertions in hostmetrics_expectations.yaml
	// to the exported data in metrics.json.
	expectDataLooksLike(exportedData, expectations)
}