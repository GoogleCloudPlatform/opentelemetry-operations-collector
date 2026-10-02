// Copyright 2026 Google LLC
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

package opsagentconfprovider

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	_ "github.com/prometheus/prometheus/discovery/install" // Register Prometheus service discovery implementations.
	"go.yaml.in/yaml/v3"
)

const minScrapeInterval = model.Duration(10 * time.Second)

func validatePrometheusConfig(rawConfig map[string]any) error {
	if len(rawConfig) == 0 {
		return errors.New("no Prometheus scrape_configs")
	}

	promCfg, err := loadPrometheusConfig(rawConfig)
	if err != nil {
		return err
	}

	if len(promCfg.ScrapeConfigs) == 0 {
		return errors.New("no Prometheus scrape_configs")
	}

	var unsupportedFeatures []string
	if len(promCfg.RemoteWriteConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "remote_write")
	}
	if len(promCfg.RemoteReadConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "remote_read")
	}
	if len(promCfg.RuleFiles) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "rule_files")
	}
	if len(promCfg.AlertingConfig.AlertRelabelConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "alert_config.relabel_configs")
	}
	if len(promCfg.AlertingConfig.AlertmanagerConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "alert_config.alertmanagers")
	}
	if len(unsupportedFeatures) != 0 {
		sort.Strings(unsupportedFeatures)
		return fmt.Errorf("unsupported features:\n\t%s", strings.Join(unsupportedFeatures, "\n\t"))
	}

	for _, sc := range promCfg.ScrapeConfigs {
		if sc.HonorLabels {
			return fmt.Errorf("error validating scrape_config for job %v: honor_labels is not supported", sc.JobName)
		}
		for _, rc := range sc.RelabelConfigs {
			if rc.TargetLabel == "location" || rc.TargetLabel == "namespace" || rc.TargetLabel == "cluster" {
				return fmt.Errorf("error validating scrape_config for job %v: relabel_configs cannot rename location, namespace or cluster", sc.JobName)
			}
		}
		for _, rc := range sc.MetricRelabelConfigs {
			if rc.TargetLabel == "__name__" {
				return fmt.Errorf("error validating scrape_config for job %v: metric_relabel_configs cannot rename __name__", sc.JobName)
			}
			if rc.TargetLabel == "location" || rc.TargetLabel == "namespace" || rc.TargetLabel == "cluster" {
				return fmt.Errorf("error validating scrape_config for job %v: metric_relabel_configs cannot rename location, namespace or cluster", sc.JobName)
			}
		}
		if sc.HTTPClientConfig.Authorization != nil {
			if err := checkPrometheusFile(sc.HTTPClientConfig.Authorization.CredentialsFile); err != nil {
				return fmt.Errorf("error checking authorization credentials file %q: %w", sc.HTTPClientConfig.Authorization.CredentialsFile, err)
			}
		}
		if err := checkPrometheusTLSConfig(sc.HTTPClientConfig.TLSConfig); err != nil {
			return err
		}
		for _, c := range sc.ServiceDiscoveryConfigs {
			switch c := c.(type) {
			case discovery.StaticConfig:
			default:
				return fmt.Errorf("unsupported service discovery config %T", c)
			}
		}
	}

	return nil
}

func loadPrometheusConfig(rawConfig map[string]any) (*promconfig.Config, error) {
	yamlBytes, err := yaml.Marshal(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Prometheus config: %w", err)
	}
	return promconfig.Load(string(yamlBytes), slog.New(slog.DiscardHandler))
}

func checkPrometheusFile(fn string) error {
	if fn == "" {
		return nil
	}
	if _, err := os.Stat(fn); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %q does not exist", fn)
		}
		return fmt.Errorf("error checking file %q", fn)
	}
	return nil
}

func checkPrometheusTLSConfig(tlsConfig commonconfig.TLSConfig) error {
	if err := checkPrometheusFile(tlsConfig.CertFile); err != nil {
		return fmt.Errorf("error checking client cert file %q: %w", tlsConfig.CertFile, err)
	}
	if err := checkPrometheusFile(tlsConfig.KeyFile); err != nil {
		return fmt.Errorf("error checking client key file %q: %w", tlsConfig.KeyFile, err)
	}
	return nil
}

func prometheusReceiver(rawConfig map[string]any) map[string]any {
	yamlBytes, err := yaml.Marshal(rawConfig)
	if err != nil {
		return map[string]any{"config": rawConfig}
	}
	var cloned map[string]any
	if err := yaml.Unmarshal(yamlBytes, &cloned); err != nil {
		return map[string]any{"config": rawConfig}
	}

	if promCfg, err := promconfig.Load(string(yamlBytes), slog.New(slog.DiscardHandler)); err == nil {
		if scList, ok := cloned["scrape_configs"].([]any); ok {
			for i, sc := range promCfg.ScrapeConfigs {
				if sc.ScrapeInterval < minScrapeInterval && i < len(scList) {
					if scMap, ok := scList[i].(map[string]any); ok {
						scMap["scrape_interval"] = minScrapeInterval.String()
					}
				}
			}
		}
	}

	return map[string]any{
		"config": cloned,
	}
}
