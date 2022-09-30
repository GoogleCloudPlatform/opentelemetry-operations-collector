package main

import (
	"context"
	"flag"
	"testing"
	"time"
	"os"
)

func TestHostmetrics(t *testing.T) {
	flag.Set("config", "config-for-testing.yaml")

	ctx, cancel = context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Run the main function of otelopscol.
	mainContext(ctx)

	data, err := os.ReadFile("metrics.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal(string(data))
}