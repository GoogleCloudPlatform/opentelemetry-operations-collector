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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/otel"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/otel/ottl"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/platform"
)

type LoggingProcessorIisAccess struct {
	ConfigComponent `yaml:",inline"`
}

func (LoggingProcessorIisAccess) Type() string {
	return "iis_access"
}

type LoggingReceiverIisAccess struct {
	ConfigComponent           `yaml:",inline"`
	LoggingReceiverFilesMixin `yaml:",inline"`
}

func (LoggingReceiverIisAccess) Type() string {
	return "iis_access"
}

func (r LoggingReceiverIisAccess) Pipelines(ctx context.Context) ([]otel.ReceiverPipeline, error) {
	rps, err := r.LoggingReceiverFilesMixin.Pipelines(ctx)
	if err != nil {
		return nil, err
	}
	processors, err := (LoggingProcessorIisAccess{}).Processors(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rps {
		rps[i].Processors["logs"] = append(rps[i].Processors["logs"], processors...)
	}
	return rps, nil
}

func iisConcatFieldsProcessors(ctx context.Context) ([]otel.Component, error) {
	// Required OTTL fields
	bodyHttpRequestServerIp := ottl.LValue{"body", "http_request_serverIp"}
	bodySPort := ottl.LValue{"body", "s_port"}
	bodyUriQuery := ottl.LValue{"body", "cs_uri_query"}
	bodyUriStem := ottl.LValue{"body", "cs_uri_stem"}

	modifyFields := LoggingProcessorModifyFields{
		Fields: map[string]*ModifyField{
			// Omit fields equal to "-"
			"jsonPayload.cs_uri_query": {
				OmitIf: `jsonPayload.cs_uri_query = "-"`,
			},
			"jsonPayload.http_request_referer": {
				OmitIf: `jsonPayload.http_request_referer = "-"`,
			},
			"jsonPayload.user": {
				OmitIf: `jsonPayload.user = "-"`,
			},
			// Concatenate serverIp with port
			"jsonPayload.http_request_serverIp": {
				CopyFrom: "jsonPayload.http_request_serverIp",
				CustomConvertFunc: func(v ottl.LValue) ottl.Statements {
					return ottl.NewStatements(
						v.Set(ottl.Concat([]ottl.Value{bodyHttpRequestServerIp, bodySPort}, ":")),
						bodySPort.Delete(),
					)
				},
			},
			// Build requestUrl from stem and query
			"jsonPayload.http_request_requestUrl": {
				CopyFrom: "jsonPayload.cs_uri_stem",
				CustomConvertFunc: func(v ottl.LValue) ottl.Statements {
					return ottl.NewStatements(
						// Set requestUrl to stem when query is empty/"-"
						v.SetIf(
							bodyUriStem,
							ottl.And(
								bodyUriStem.IsPresent(),
								ottl.Or(
									ottl.Not(ottl.IsNotNil(bodyUriQuery)),
									ottl.Equals(bodyUriQuery, ottl.StringLiteral("")),
									ottl.Equals(bodyUriQuery, ottl.StringLiteral("-")),
								),
							),
						),
						// Set requestUrl to stem + "?" + query when query has content
						v.SetIf(
							ottl.Concat([]ottl.Value{bodyUriStem, bodyUriQuery}, "?"),
							ottl.And(bodyUriStem.IsPresent(), bodyUriQuery.IsPresent()),
						),
						// Clean up intermediate fields
						bodyUriQuery.Delete(),
						bodyUriStem.Delete(),
					)
				},
			},
		},
	}

	return modifyFields.Processors(ctx)
}

func (p LoggingProcessorIisAccess) Processors(ctx context.Context) ([]otel.Component, error) {
	parseRegex := LoggingProcessorParseRegex{
		// Microsoft updated the default format in Feb 2026.
		// The new format now has fields sc_bytes and cs_bytes added right before time_taken
		// To ensure backwards compatibility, we added an optional capture group right before time_taken
		// Documentation:
		// https://docs.microsoft.com/en-us/windows/win32/http/w3c-logging
		// sample line old format: 2022-03-10 17:26:30 ::1 GET /iisstart.png - 80 - ::1 Mozilla/5.0+(Windows+NT+10.0;+WOW64;+Trident/7.0;+rv:11.0)+like+Gecko http://localhost/ 200 0 0 18
		// sample line old format: 2022-03-10 17:26:30 ::1 GET / - 80 - ::1 Mozilla/5.0+(Windows+NT+10.0;+WOW64;+Trident/7.0;+rv:11.0)+like+Gecko - 200 0 0 352
		// sample line old format: 2022-03-10 17:26:32 ::1 GET /favicon.ico - 80 - ::1 Mozilla/5.0+(Windows+NT+10.0;+WOW64;+Trident/7.0;+rv:11.0)+like+Gecko - 404 0 2 49
		// sample line new format: 2026-02-19 10:28:49 ::1 GET /forbidden something=something 80 - ::1 Mozilla/5.0+(Windows+NT;+Windows+NT+10.0;+en-US)+WindowsPowerShell/5.1.26100.32370 - 404 0 2 5035 184 189
		Regex: `^(?<timestamp>\d{4}-\d{2}-\d{2}\s\d{2}:\d{2}:\d{2})\s(?<http_request_serverIp>[^\s]+)\s(?<http_request_requestMethod>[^\s]+)\s(?<cs_uri_stem>\/[^\s]*)\s(?<cs_uri_query>[^\s]*)\s(?<s_port>\d*)\s(?<user>[^\s]+)\s(?<http_request_remoteIp>[^\s]+)\s(?<http_request_userAgent>[^\s]+)\s(?<http_request_referer>[^\s]+)\s(?<http_request_status>\d{3})\s(?<sc_substatus>\d+)\s(?<sc_win32_status>\d+)(?:\s(?<sc_bytes>\d+)\s(?<cs_bytes>\d+))?\s(?<time_taken>\d+)$`,
		ParserShared: ParserShared{
			TimeKey:    "timestamp",
			TimeFormat: "%Y-%m-%d %H:%M:%S",
			Types: map[string]string{
				"http_request_status": "integer",
			},
		},
	}

	// This is used to exclude the header lines above the logs
	// EXAMPLE LINES:
	// #Software: Microsoft Internet Information Services 10.0
	// #Version: 1.0
	// #Date: 2022-04-11 12:53:50
	excludeLogs := LoggingProcessorExcludeLogs{
		MatchAny: []string{`jsonPayload.message=~"^#(?:Fields|Date|Version|Software):"`},
	}

	instrumentationVal := fmt.Sprintf("agent.googleapis.com/%s", p.Type())
	fields := map[string]*ModifyField{
		InstrumentationSourceLabel: {
			StaticValue: &instrumentationVal,
		},
	}

	// Generate the httpRequest structure field moves
	for _, field := range []string{
		"serverIp",
		"remoteIp",
		"requestMethod",
		"requestUrl",
		"status",
		"referer",
		"userAgent",
	} {
		fields[fmt.Sprintf("httpRequest.%s", field)] = &ModifyField{
			MoveFrom: fmt.Sprintf("jsonPayload.http_request_%s", field),
		}
	}

	modifyFields := LoggingProcessorModifyFields{
		Fields: fields,
	}

	var out []otel.Component
	for _, step := range []func(context.Context) ([]otel.Component, error){
		parseRegex.Processors,
		iisConcatFieldsProcessors,
		excludeLogs.Processors,
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

func init() {
	LoggingProcessorTypes.RegisterType(func() LoggingProcessor {
		return &LoggingProcessorIisAccess{}
	}, platform.Windows)
	LoggingReceiverTypes.RegisterType(func() LoggingReceiver {
		return &LoggingReceiverIisAccess{
			LoggingReceiverFilesMixin: LoggingReceiverFilesMixin{
				IncludePaths: []string{
					`C:\inetpub\logs\LogFiles\W3SVC1\u_ex*`,
				},
			},
		}
	}, platform.Windows)
}
