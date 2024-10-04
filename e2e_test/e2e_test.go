//go:build integration_test

package e2e_test

import (
	"context"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/e2e_test/gce"
	"os"
	"testing"
)

func TestGcloud(t *testing.T) {
	projectName := os.Getenv("proj")
	if projectName == "" {
		t.Fatal("No proj enviroment variable found")
	}
	ctx := context.Background()
	logger := gce.SetupLogger(t)
	vmOptions := gce.VMOptions{
		ImageSpec: "cos-cloud:cos-stable",
		Project:   projectName,
		Zone:      "us-central1-a",
	}
	vm, err := gce.CreateInstance(ctx, logger.ToFile("VM_initialization.txt"), vmOptions)
	if err != nil {
		t.Fatal(err)
	}
	var cmd gce.CommandOutput
	cmd, err = gce.RunScriptRemotely(ctx, logger.ToFile("script.txt"), vm, "echo foo", []string{}, make(map[string]string))
	logger.ToMainLog().Printf("cmd output = %s", cmd.Stdout)
}
