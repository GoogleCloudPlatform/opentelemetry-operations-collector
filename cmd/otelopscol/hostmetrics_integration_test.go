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

	exportedData := readFile("metrics.json")
	expectations := readFile("hostmetrics_expectations.yaml")

	// Logic for applying the assertions in hostmetrics_expectations.yaml
	// to the exported data in metrics.json.
	expectDataLooksLike(exportedData, expectations)
}