package main_test

package main

import (
	"context"
	"testing"
	"time"
)

func TestBasic(t *testing.T) {
	ctx := context.WithTimeout(context.Background(), 10 * time.Second)
	// Should fail with a 'no config' error
	mainContext(ctx)
}