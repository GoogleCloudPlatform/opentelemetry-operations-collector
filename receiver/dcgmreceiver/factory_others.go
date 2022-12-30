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

//go:build !windows
// +build !windows

package dcgmreceiver

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/scraperhelper"
)

var dcgmIDToName map[dcgm.Short]string
var dcgmNameToMetricName map[string]string
var metricNameToDcgmName map[string]string

func init() {
	rand.Seed(time.Now().UnixMicro())

	dcgmIDToName = make(map[dcgm.Short]string, len(dcgm.DCGM_FI))
	for fieldName, fieldID := range dcgm.DCGM_FI {
		dcgmIDToName[fieldID] = fieldName
	}

	dcgmNameToMetricName = map[string]string{
		"DCGM_FI_DEV_GPU_UTIL":            "dcgm.gpu.utilization",
		"DCGM_FI_DEV_FB_USED":             "dcgm.gpu.memory.bytes_used",
		"DCGM_FI_DEV_FB_FREE":             "dcgm.gpu.memory.bytes_free",
		"DCGM_FI_PROF_SM_ACTIVE":          "dcgm.gpu.profiling.sm_utilization",
		"DCGM_FI_PROF_SM_OCCUPANCY":       "dcgm.gpu.profiling.sm_occupancy",
		"DCGM_FI_PROF_PIPE_TENSOR_ACTIVE": "dcgm.gpu.profiling.tensor_utilization",
		"DCGM_FI_PROF_DRAM_ACTIVE":        "dcgm.gpu.profiling.dram_utilization",
		"DCGM_FI_PROF_PIPE_FP64_ACTIVE":   "dcgm.gpu.profiling.fp64_utilization",
		"DCGM_FI_PROF_PIPE_FP32_ACTIVE":   "dcgm.gpu.profiling.fp32_utilization",
		"DCGM_FI_PROF_PIPE_FP16_ACTIVE":   "dcgm.gpu.profiling.fp16_utilization",
		"DCGM_FI_PROF_PCIE_TX_BYTES":      "dcgm.gpu.profiling.pcie_sent_bytes",
		"DCGM_FI_PROF_PCIE_RX_BYTES":      "dcgm.gpu.profiling.pcie_received_bytes",
		"DCGM_FI_PROF_NVLINK_TX_BYTES":    "dcgm.gpu.profiling.nvlink_sent_bytes",
		"DCGM_FI_PROF_NVLINK_RX_BYTES":    "dcgm.gpu.profiling.nvlink_received_bytes",
	}

	metricNameToDcgmName = make(map[string]string, len(dcgmNameToMetricName))
	for dcgmName, metricName := range dcgmNameToMetricName {
		metricNameToDcgmName[metricName] = dcgmName
	}
}

func createMetricsReceiver(
	_ context.Context,
	params receiver.CreateSettings,
	rConf component.Config,
	consumer consumer.Metrics,
) (receiver.Metrics, error) {
	cfg, ok := rConf.(*Config)
	if !ok {
		return nil, fmt.Errorf("Unable to cast receiver configuration to dcgm.Config")
	}

	ns, err := newDcgmScraper(cfg, params)
	if err != nil {
		return nil, err
	}

	scraper, err := scraperhelper.NewScraper(
		typeStr,
		ns.scrape,
		scraperhelper.WithStart(ns.start),
		scraperhelper.WithShutdown(ns.stop))
	if err != nil {
		return nil, err
	}

	return scraperhelper.NewScraperControllerReceiver(
		&cfg.ScraperControllerSettings, params, consumer,
		scraperhelper.AddScraper(scraper),
	)
}
