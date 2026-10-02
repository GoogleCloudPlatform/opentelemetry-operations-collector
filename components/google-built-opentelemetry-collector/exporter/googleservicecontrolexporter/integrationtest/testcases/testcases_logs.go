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

package testcases

var LogsTestCases = []TestCase{
	{
		Name:                 "Apache access log with HTTPRequest",
		ConfigPath:           "testdata/fixtures/configs/basic.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_apache_access.json",
		ExpectFixturePath:    "fixtures/logs/logs_apache_access_expected.json",
	},
	{
		Name:                 "Apache error log with severity",
		ConfigPath:           "testdata/fixtures/configs/basic.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_apache_error.json",
		ExpectFixturePath:    "fixtures/logs/logs_apache_error_expected.json",
	},
	{
		Name:                 "Apache error log (text payload) with severity",
		ConfigPath:           "testdata/fixtures/configs/basic.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_apache_text_error.json",
		ExpectFixturePath:    "fixtures/logs/logs_apache_text_error_expected.json",
	},
	{
		Name:                 "Multi-project logs with servicecontrol.consumer_id",
		ConfigPath:           "testdata/fixtures/configs/basic.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_multi_project.json",
		ExpectFixturePath:    "fixtures/logs/logs_multi_project_expected.json",
	},
	{
		Name:                 "Logs with scope information",
		ConfigPath:           "testdata/fixtures/configs/basic.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_apache_error_scope.json",
		ExpectFixturePath:    "fixtures/logs/logs_apache_error_scope_expected.json",
	},
	{
		Name:                 "Logs with trace/span info",
		ConfigPath:           "testdata/fixtures/configs/basic.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_span_trace_id.json",
		ExpectFixturePath:    "fixtures/logs/logs_span_trace_id_expected.json",
	},
	{
		Name:                 "Logs custom operation name",
		ConfigPath:           "testdata/fixtures/configs/override_operation_name.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_span_trace_id.json",
		ExpectFixturePath:    "fixtures/logs/logs_operation_name_expected.json",
	},
	{
		Name:                 "Logs provide default log name",
		ConfigPath:           "testdata/fixtures/configs/provide_default_log_name.yaml",
		OTLPInputFixturePath: "testdata/fixtures/logs/logs_missing_log_name.json",
		ExpectFixturePath:    "fixtures/logs/logs_missing_log_name_expected.json",
	},
}
