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

import "fmt"

func (r MetricsReceiver) mssqlReceiver(escapedID string) (string, map[string]any) {
	if r.ReceiverVersion == "2" {
		return fmt.Sprintf("sqlserver/%s", escapedID), map[string]any{
			"collection_interval": r.collectionInterval(),
		}
	}

	return fmt.Sprintf("windowsperfcounters/%s", escapedID), map[string]any{
		"collection_interval": r.collectionInterval(),
		"perfcounters": []map[string]any{
			{
				"object":    "SQLServer:General Statistics",
				"instances": []string{"_Total"},
				"counters":  []map[string]string{{"name": "User Connections"}},
			},
			{
				"object":    "SQLServer:Databases",
				"instances": []string{"_Total"},
				"counters": []map[string]string{
					{"name": "Transactions/sec"},
					{"name": "Write Transactions/sec"},
				},
			},
		},
	}
}

func (r MetricsReceiver) mssqlProcessors(escapedID string) []namedProcessor {
	if r.ReceiverVersion == "2" {
		return []namedProcessor{
			{
				id: fmt.Sprintf("metricstransform/%s_0", escapedID),
				config: metricsTransformProcessor(
					renameMetric(
						"sqlserver.transaction_log.usage",
						"sqlserver.transaction_log.percent_used",
					),
					addMetricPrefix("workload.googleapis.com"),
				),
			},
			{
				id: fmt.Sprintf("transform/%s_1", escapedID),
				config: flattenResourceAndSetScopeProcessor(
					"agent.googleapis.com/mssql",
					"2.0",
					`set(attributes["database"], resource.attributes["sqlserver.database.name"])`,
				),
			},
			{
				id:     fmt.Sprintf("transform/%s_2", escapedID),
				config: removeServiceAttributesProcessor(),
			},
			{
				id:     fmt.Sprintf("normalizesums/%s_3", escapedID),
				config: map[string]any{},
			},
		}
	}

	return []namedProcessor{
		{
			id: fmt.Sprintf("metricstransform/%s_0", escapedID),
			config: metricsTransformProcessor(
				renameMetric(
					`\SQLServer:General Statistics(_Total)\User Connections`,
					"mssql/connections/user",
				),
				renameMetric(
					`\SQLServer:Databases(_Total)\Transactions/sec`,
					"mssql/transaction_rate",
				),
				renameMetric(
					`\SQLServer:Databases(_Total)\Write Transactions/sec`,
					"mssql/write_transaction_rate",
				),
				addMetricPrefix("agent.googleapis.com"),
			),
		},
		{
			id:     fmt.Sprintf("transform/%s_1", escapedID),
			config: setInstrumentationScopeProcessor("agent.googleapis.com/mssql", "1.0"),
		},
		{
			id:     fmt.Sprintf("transform/%s_2", escapedID),
			config: removeInstrumentationScopeProcessor(),
		},
		{
			id:     fmt.Sprintf("transform/%s_3", escapedID),
			config: removeServiceAttributesProcessor(),
		},
	}
}
