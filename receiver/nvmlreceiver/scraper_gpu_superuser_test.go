// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build gpu && superuser
// +build gpu,superuser

package nvmlreceiver

import (
	"context"
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver/testcudakernel"
)

func TestScrapeWithGpuProcessAccounting(t *testing.T) {
	scraper := newNvmlScraper(createDefaultConfig().(*Config), componenttest.NewNopReceiverCreateSettings())
	require.NotNil(t, scraper)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	testcudakernel.SubmitCudaTestKernel()

	metrics, err := scraper.scrape(context.Background())
	validateScraperResult(t, metrics, []string{
		"nvml.gpu.utilization",
		"nvml.gpu.memory.bytes_used",
		"nvml.processes.lifetime_gpu_utilization",
		"nvml.processes.lifetime_gpu_max_bytes_used",
	})
}

func TestScrapeWithGpuProcessAccountingError(t *testing.T) {
	realNvmlDeviceGetAccountingPids := nvmlDeviceGetAccountingPids
	defer func() { nvmlDeviceGetAccountingPids = realNvmlDeviceGetAccountingPids }()
	nvmlDeviceGetAccountingPids = func(device nvml.Device) ([]int, nvml.Return) {
		return nil, nvml.ERROR_UNKNOWN
	}

	scraper := newNvmlScraper(createDefaultConfig().(*Config), componenttest.NewNopReceiverCreateSettings())
	require.NotNil(t, scraper)

	err := scraper.start(context.Background(), componenttest.NewNopHost())
	require.NoError(t, err)

	testcudakernel.SubmitCudaTestKernel()

	metrics, err := scraper.scrape(context.Background())
	validateScraperResult(t, metrics, []string{
		"nvml.gpu.utilization",
		"nvml.gpu.memory.bytes_used",
	})
}
