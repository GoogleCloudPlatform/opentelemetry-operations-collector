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

package wasmreceiver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	// commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	// resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

type wasmPluginScraper struct {
	cfg      *Config
	settings component.TelemetrySettings

	unmarshaller *pmetric.ProtoUnmarshaler

	pluginModule api.Module
	pluginScrape api.Function
}

func newWasmPluginScraper(settings receiver.Settings, cfg *Config) *wasmPluginScraper {
	return &wasmPluginScraper{
		settings: settings.TelemetrySettings,
		cfg:      cfg,
	}
}

func (s *wasmPluginScraper) start(ctx context.Context, host component.Host) error {
	// Create a new WebAssembly Runtime.
	r := wazero.NewRuntime(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	metricWasm, err := os.ReadFile(s.cfg.WasmPath)
	if err != nil {
		return err
	}

	_, err = r.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(s.getWasmLogFunc()).Export("log").
		Instantiate(ctx)
	if err != nil {
		return err
	}

	modConfig := wazero.NewModuleConfig().WithStartFunctions("_initialize")
	if s.cfg.AllowFileAccess != "" {
		modConfig = modConfig.WithFS(os.DirFS(s.cfg.AllowFileAccess))
	}

	// Instantiate a WebAssembly module that imports the "log" function defined
	// in "env" and exports "memory" and functions we'll use in this example.
	mod, err := r.InstantiateWithConfig(ctx, metricWasm, modConfig)
	if err != nil {
		return err
	}
	s.pluginModule = mod

	// Get references to WebAssembly functions we'll use in this example.
	s.pluginScrape = s.pluginModule.ExportedFunction("scrape")

	if s.pluginScrape == nil {
		return errors.New("wasm scrape function not found")
	}

	s.unmarshaller = &pmetric.ProtoUnmarshaler{}

	return nil
}

func (s *wasmPluginScraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	result, err := s.pluginScrape.Call(ctx)
	if err != nil {
		log.Panicln(err)
	}

	pbPtr := uint32(result[0] >> 32)
	pbSize := uint32(result[0])

	if pbPtr == 0 {
		return pmetric.NewMetrics(), nil
	}

	// The pointer is a linear memory offset, which is where we write the name.
	bytes, ok := s.pluginModule.Memory().Read(pbPtr, pbSize)
	if !ok {
		return pmetric.NewMetrics(), fmt.Errorf("Memory.Read(%d, %d) out of range of memory size %d",
			pbPtr, pbSize, s.pluginModule.Memory().Size())
	}

	metrics, err := s.unmarshaller.UnmarshalMetrics(bytes)
	if err != nil {
		return pmetric.NewMetrics(), fmt.Errorf("failed to unmarshal: %v", err)
	}

	return metrics, nil
}

func (s *wasmPluginScraper) getWasmLogFunc() func(_ context.Context, m api.Module, offset, byteCount uint32) {
	return func(_ context.Context, m api.Module, offset, byteCount uint32) {
		buf, ok := m.Memory().Read(offset, byteCount)
		if !ok {
			log.Panicf("Memory.Read(%d, %d) out of range", offset, byteCount)
		}
		s.settings.Logger.Error(string(buf))
	}
}
