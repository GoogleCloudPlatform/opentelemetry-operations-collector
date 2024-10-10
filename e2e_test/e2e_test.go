//go:build integration_test

package e2e_test

import (
	"context"
	"fmt"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/e2e_test/gce"
	"github.com/google/uuid"
	"log"
	"os"
	"testing"
	"time"
)

func TestGcloud(t *testing.T) {
	log.Printf("USE_INTERNAL: %s\n", os.Getenv("USE_INTERNAL_IP"))
	projectName := os.Getenv("PROJECT_NAME")
	if projectName == "" {
		t.Fatal("No PROJECT_NAME environment variable found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	t.Cleanup(cancel)
	ctx = gce.WithGcloudConfigDir(ctx, t.TempDir())
	logger := gce.SetupLogger(t)
	vmOptions := gce.VMOptions{
		ImageSpec: "cos-cloud:cos-stable",
		Project:   projectName,
		Zone:      "us-central1-a",
		Name:      fmt.Sprintf("gboc-%s", uuid.New().String()),
	}
	vm, err := gce.CreateInstance(ctx, log.Default(), vmOptions)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("vm successfully created with name: %s", vm.Name)
	var cmd gce.CommandOutput
	cmd, err = gce.RunScriptRemotely(ctx, logger.ToFile("script.txt"), vm, "echo foo", []string{}, make(map[string]string))
	if err != nil {
		t.Fatal("could not run script", err)
	}
	logger.ToMainLog().Printf("cmd output = %s", cmd.Stdout)
	err = gce.DeleteInstance(logger.ToMainLog(), vm)
	if err != nil {
		t.Fatal("could not delete instance", err)
	}
}
