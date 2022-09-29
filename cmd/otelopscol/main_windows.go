// Copyright 2020 Google LLC
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

//go:build windows
// +build windows

package main

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/service"
	"golang.org/x/sys/windows/svc"
)

func run(ctx context.Context, params service.CollectorSettings) error {
	// TODO: this should possibly be using svc.IsWindowsService() instead.
	isInteractive, err := svc.IsAnInteractiveSession()
	if err != nil {
		return fmt.Errorf("failed to determine if we are running in an interactive session: %w", err)
	}

	if isInteractive {
		return runInteractive(ctx, params)
	}
	return runService(params)
}

func runService(params service.CollectorSettings) error {
	// do not need to supply service name when startup is invoked through Service Control Manager directly
	if err := svc.Run("", service.NewSvcHandler(params)); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}
