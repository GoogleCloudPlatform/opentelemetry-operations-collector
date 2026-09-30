// Copyright 2021 Google LLC
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

package confgenerator

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/otel"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/secret"
)

type MetricsReceiverRedis struct {
	ConfigComponent          `yaml:",inline"`
	MetricsReceiverSharedTLS `yaml:",inline"`
	MetricsReceiverShared    `yaml:",inline"`

	// TODO: Add support for ACL Authentication
	Address  string        `yaml:"address" validate:"omitempty,hostname_port|startswith=/"`
	Password secret.String `yaml:"password" validate:"omitempty"`
}

const defaultRedisEndpoint = "localhost:6379"

func (r MetricsReceiverRedis) Type() string {
	return "redis"
}

func (r MetricsReceiverRedis) Pipelines(ctx context.Context) ([]otel.ReceiverPipeline, error) {
	if r.Address == "" {
		r.Address = defaultRedisEndpoint
	}

	var transport string
	if strings.HasPrefix(r.Address, "/") {
		transport = "unix"
	} else {
		transport = "tcp"
	}

	return []otel.ReceiverPipeline{{
		Receiver: otel.Component{
			Type: "redis",
			Config: map[string]interface{}{
				"collection_interval": r.CollectionIntervalString(),
				"endpoint":            r.Address,
				"password":            r.Password.SecretValue(),
				"tls":                 r.TLSConfig(true),
				"transport":           transport,
			},
		},
		Processors: map[string][]otel.Component{"metrics": {
			otel.MetricsFilter(
				"exclude",
				"strict",
				"redis.commands",
				"redis.uptime",
			),
			otel.MetricStartTime(),
			otel.MetricsTransform(
				otel.AddPrefix("workload.googleapis.com"),
			),
			otel.TransformationMetrics(
				otel.SetScopeName("agent.googleapis.com/"+r.Type()),
				otel.SetScopeVersion("1.0"),
			),
			otel.MetricsRemoveServiceAttributes(),
		}},
	}}, nil
}

type LoggingProcessorRedis struct {
	ConfigComponent `yaml:",inline"`
}

func (LoggingProcessorRedis) Type() string {
	return "redis"
}

func (p LoggingProcessorRedis) Processors(ctx context.Context) ([]otel.Component, error) {
	parseRegex := LoggingProcessorParseRegex{
		// Documentation: https://github.com/redis/redis/blob/6.2/src/server.c#L1122
		// Sample line (Redis 3+): 534:M 28 Apr 2020 11:30:29.988 * DB loaded from disk: 0.002 seconds
		// Sample line (Redis <3): [4018] 14 Nov 07:01:22.119 * Background saving terminated with success
		Regex: `^\[?(?<pid>\d+):?(?<roleChar>[A-Z])?\]?\s+(?<time>\d{2}\s+\w+(?:\s+\d{4})?\s+\d{2}:\d{2}:\d{2}.\d{3})\s+(?<level>(\*|#|-|\.))\s+(?<message>.*)$`,
		ParserShared: ParserShared{
			TimeKey:    "time",
			TimeFormat: "%d %b %Y %H:%M:%S.%L",
			Types: map[string]string{
				"pid": "integer",
			},
		},
	}

	instrumentationVal := fmt.Sprintf("agent.googleapis.com/%s", p.Type())
	// Log levels documented: https://github.com/redis/redis/blob/6.2/src/server.c#L1124
	modifyFields := LoggingProcessorModifyFields{
		Fields: map[string]*ModifyField{
			"severity": {
				CopyFrom: "jsonPayload.level",
				MapValues: map[string]string{
					".": "DEBUG",
					"-": "INFO",
					"*": "NOTICE",
					"#": "WARNING",
				},
				MapValuesExclusive: true,
			},
			"jsonPayload.role": {
				CopyFrom: "jsonPayload.roleChar",
				// Role translation documented: https://github.com/redis/redis/blob/6.2/src/server.c#L1149
				MapValues: map[string]string{
					"X": "sentinel",
					"C": "RDB/AOF_writing_child",
					"S": "slave",
					"M": "master",
				},
				MapValuesExclusive: true,
			},
			InstrumentationSourceLabel: {
				StaticValue: &instrumentationVal,
			},
		},
	}

	var out []otel.Component
	for _, step := range []func(context.Context) ([]otel.Component, error){
		parseRegex.Processors,
		modifyFields.Processors,
	} {
		c, err := step(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, c...)
	}
	return out, nil
}

type LoggingReceiverRedis struct {
	ConfigComponent           `yaml:",inline"`
	LoggingReceiverFilesMixin `yaml:",inline"`
}

func (LoggingReceiverRedis) Type() string {
	return "redis"
}

func (r LoggingReceiverRedis) Pipelines(ctx context.Context) ([]otel.ReceiverPipeline, error) {
	rps, err := r.LoggingReceiverFilesMixin.Pipelines(ctx)
	if err != nil {
		return nil, err
	}
	processors, err := (LoggingProcessorRedis{}).Processors(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rps {
		rps[i].Processors["logs"] = append(rps[i].Processors["logs"], processors...)
	}
	return rps, nil
}

func init() {
	MetricsReceiverTypes.RegisterType(func() MetricsReceiver { return &MetricsReceiverRedis{} })
	LoggingProcessorTypes.RegisterType(func() LoggingProcessor { return &LoggingProcessorRedis{} })
	LoggingReceiverTypes.RegisterType(func() LoggingReceiver {
		return &LoggingReceiverRedis{
			LoggingReceiverFilesMixin: LoggingReceiverFilesMixin{
				IncludePaths: []string{
					// Default log path on Ubuntu / Debian
					"/var/log/redis/redis-server.log",
					// Default log path built from src (6379 is the default redis port)
					"/var/log/redis_6379.log",
					// Default log path on CentOS / RHEL
					"/var/log/redis/redis.log",
					// Default log path on SLES
					"/var/log/redis/default.log",
					// Default log path from one click installer (6379 is the default redis port)
					"/var/log/redis/redis_6379.log",
				},
			},
		}
	})
}
