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
	"fmt"
	"maps"
	"slices"
	"sort"
)

const instrumentationSourceLabel = `labels."logging.googleapis.com/instrumentation_source"`

// ModifyField describes a field modification operation in the modify_fields logging processor.
type ModifyField struct {
	MoveFrom           string                          `yaml:"move_from,omitempty"`
	CopyFrom           string                          `yaml:"copy_from,omitempty"`
	StaticValue        *string                         `yaml:"static_value,omitempty"`
	DefaultValue       *string                         `yaml:"default_value,omitempty"`
	Type               string                          `yaml:"type,omitempty"`
	CustomConvertFunc  func(ottlLValue) ottlStatements `yaml:"-"`
	OmitIf             string                          `yaml:"omit_if,omitempty"`
	MapValues          map[string]string               `yaml:"map_values,omitempty"`
	MapValuesExclusive bool                            `yaml:"map_values_exclusive,omitempty"`
}

var legacyBuiltinProcessors = map[string]LoggingProcessor{
	"lib:apache": {
		Type:       "parse_regex",
		Regex:      `^(?<host>[^ ]*) [^ ]* (?<user>[^ ]*) \[(?<time>[^\]]*)\] "(?<method>\S+)(?: +(?<path>[^\"]*?)(?: +\S*)?)?" (?<code>[^ ]*) (?<size>[^ ]*)(?: "(?<referer>[^\"]*)" "(?<agent>[^\"]*)")?$`,
		TimeKey:    "time",
		TimeFormat: "%d/%b/%Y:%H:%M:%S %z",
	},
	"lib:apache2": {
		Type:       "parse_regex",
		Regex:      `^(?<host>[^ ]*) [^ ]* (?<user>[^ ]*) \[(?<time>[^\]]*)\] "(?<method>\S+)(?: +(?<path>[^ ]*) +\S*)?" (?<code>[^ ]*) (?<size>[^ ]*)(?: "(?<referer>[^\"]*)" "(?<agent>.*)")?$`,
		TimeKey:    "time",
		TimeFormat: "%d/%b/%Y:%H:%M:%S %z",
	},
	"lib:apache_error": {
		Type:  "parse_regex",
		Regex: `^\[[^ ]* (?<time>[^\]]*)\] \[(?<level>[^\]]*)\](?: \[pid (?<pid>[^\]]*)\])?( \[client (?<client>[^\]]*)\])? (?<message>.*)$`,
	},
	"lib:mongodb": {
		Type:       "parse_regex",
		Regex:      `^(?<time>[^ ]*)\s+(?<severity>\w)\s+(?<component>[^ ]+)\s+\[(?<context>[^\]]+)]\s+(?<message>.*?) *(?<ms>(\d+))?(:?ms)?$`,
		TimeKey:    "time",
		TimeFormat: "%Y-%m-%dT%H:%M:%S.%L",
	},
	"lib:nginx": {
		Type:       "parse_regex",
		Regex:      `^(?<remote>[^ ]*) (?<host>[^ ]*) (?<user>[^ ]*) \[(?<time>[^\]]*)\] "(?<method>\S+)(?: +(?<path>[^\"]*?)(?: +\S*)?)?" (?<code>[^ ]*) (?<size>[^ ]*)(?: "(?<referer>[^\"]*)" "(?<agent>[^\"]*)")`,
		TimeKey:    "time",
		TimeFormat: "%d/%b/%Y:%H:%M:%S %z",
	},
	"lib:syslog-rfc5424": {
		Type:       "parse_regex",
		Regex:      `^\<(?<pri>[0-9]{1,5})\>1 (?<time>[^ ]+) (?<host>[^ ]+) (?<ident>[^ ]+) (?<pid>[-0-9]+) (?<msgid>[^ ]+) (?<extradata>(\[(.*?)\]|-)) (?<message>.+)$`,
		TimeKey:    "time",
		TimeFormat: "%Y-%m-%dT%H:%M:%S.%L%Z",
	},
	"lib:syslog-rfc3164": {
		Type:       "parse_regex",
		Regex:      `/^\<(?<pri>[0-9]+)\>(?<time>[^ ]* {1,2}[^ ]* [^ ]*) (?<host>[^ ]*) (?<ident>[a-zA-Z0-9_\/\.\-]*)(?:\[(?<pid>[0-9]+)\])?(?:[^\:]*\:)? *(?<message>.*)$/`,
		TimeKey:    "time",
		TimeFormat: "%b %d %H:%M:%S",
	},
}

func (p LoggingProcessor) validateNoParserFields(id string) error {
	if p.TimeKey != "" {
		return fmt.Errorf("logging processor %q with type %q does not support time_key", id, p.Type)
	}
	if p.TimeFormat != "" {
		return fmt.Errorf("logging processor %q with type %q does not support time_format", id, p.Type)
	}
	if p.Field != "" {
		return fmt.Errorf("logging processor %q with type %q does not support field", id, p.Type)
	}
	return nil
}

func (p LoggingProcessor) validateNoRegexFields(id string) error {
	if p.Regex != "" {
		return fmt.Errorf("logging processor %q with type %q does not support regex", id, p.Type)
	}
	return nil
}

func (p LoggingProcessor) validateNoExcludeLogsFields(id string) error {
	if len(p.MatchAny) > 0 {
		return fmt.Errorf("logging processor %q with type %q does not support match_any", id, p.Type)
	}
	return nil
}

func (p LoggingProcessor) validateNoModifyFields(id string) error {
	if len(p.Fields) > 0 {
		return fmt.Errorf("logging processor %q with type %q does not support fields", id, p.Type)
	}
	return nil
}

func (p LoggingProcessor) validateParserShared(id string) error {
	if (p.TimeKey == "") != (p.TimeFormat == "") {
		return fmt.Errorf("logging processor %q with type %q requires both time_key and time_format when either is set", id, p.Type)
	}
	if p.TimeKey != "" {
		if _, err := newLogMemberLegacy(p.TimeKey); err != nil {
			return fmt.Errorf("logging processor %q has invalid time_key %q: %w", id, p.TimeKey, err)
		}
	}
	if p.Field != "" {
		if _, err := newLogMemberLegacy(p.Field); err != nil {
			return fmt.Errorf("logging processor %q has invalid field %q: %w", id, p.Field, err)
		}
	}
	return nil
}

func (p LoggingProcessor) validate(id string) error {
	switch p.Type {
	case "parse_json":
		if err := p.validateNoRegexFields(id); err != nil {
			return err
		}
		if err := p.validateNoExcludeLogsFields(id); err != nil {
			return err
		}
		if err := p.validateNoModifyFields(id); err != nil {
			return err
		}
		return p.validateParserShared(id)
	case "parse_regex":
		if err := p.validateNoExcludeLogsFields(id); err != nil {
			return err
		}
		if err := p.validateNoModifyFields(id); err != nil {
			return err
		}
		if err := p.validateParserShared(id); err != nil {
			return err
		}
		if p.Regex == "" {
			return fmt.Errorf("logging processor %q with type %q requires regex", id, p.Type)
		}
		return nil
	case "exclude_logs":
		if err := p.validateNoParserFields(id); err != nil {
			return err
		}
		if err := p.validateNoRegexFields(id); err != nil {
			return err
		}
		if err := p.validateNoModifyFields(id); err != nil {
			return err
		}
		if len(p.MatchAny) == 0 {
			return fmt.Errorf("logging processor %q with type %q requires non-empty match_any", id, p.Type)
		}
		for _, cond := range p.MatchAny {
			if _, err := newLogFilter(cond); err != nil {
				return fmt.Errorf("logging processor %q has invalid match_any filter %q: %w", id, cond, err)
			}
		}
		return nil
	case "modify_fields":
		if err := p.validateNoParserFields(id); err != nil {
			return err
		}
		if err := p.validateNoRegexFields(id); err != nil {
			return err
		}
		if err := p.validateNoExcludeLogsFields(id); err != nil {
			return err
		}
		nonWritableMember, _ := newLogMember(instrumentationSourceLabel)
		var nonWritableKey string
		if nonWritableMember != nil {
			nonWritableKey, _ = nonWritableMember.canonicalPathKey()
		}
		seenPaths := make(map[string]bool, len(p.Fields))
		for _, dest := range slices.Sorted(maps.Keys(p.Fields)) {
			destMember, err := newLogMember(dest)
			if err != nil {
				return fmt.Errorf("logging processor %q has invalid field %q: %w", id, dest, err)
			}
			destKey, err := destMember.canonicalPathKey()
			if err != nil {
				return fmt.Errorf("logging processor %q has invalid field %q: %w", id, dest, err)
			}
			if nonWritableKey != "" && destKey == nonWritableKey {
				return fmt.Errorf("logging processor %q field %q is not a writable field", id, dest)
			}
			if seenPaths[destKey] {
				return fmt.Errorf("logging processor %q field %q is specified multiple times", id, dest)
			}
			seenPaths[destKey] = true

			mf := p.Fields[dest]
			if mf == nil {
				continue
			}
			sources := 0
			if mf.MoveFrom != "" {
				sources++
			}
			if mf.CopyFrom != "" {
				sources++
			}
			if mf.StaticValue != nil {
				sources++
			}
			if sources > 1 {
				return fmt.Errorf("logging processor %q field %q cannot set more than one of [move_from copy_from static_value]", id, dest)
			}
			if mf.DefaultValue != nil && mf.StaticValue != nil {
				return fmt.Errorf("logging processor %q field %q cannot set default_value when static_value is set", id, dest)
			}
			if mf.MoveFrom != "" {
				if _, err := newLogMember(mf.MoveFrom); err != nil {
					return fmt.Errorf("logging processor %q field %q has invalid move_from %q: %w", id, dest, mf.MoveFrom, err)
				}
			}
			if mf.CopyFrom != "" {
				if _, err := newLogMember(mf.CopyFrom); err != nil {
					return fmt.Errorf("logging processor %q field %q has invalid copy_from %q: %w", id, dest, mf.CopyFrom, err)
				}
			}
			if mf.Type != "" {
				switch mf.Type {
				case "integer", "float":
				default:
					return fmt.Errorf("logging processor %q field %q has invalid type %q: must be one of [integer float]", id, dest, mf.Type)
				}
			}
			if mf.OmitIf != "" {
				if _, err := newLogFilter(mf.OmitIf); err != nil {
					return fmt.Errorf("logging processor %q field %q has invalid omit_if %q: %w", id, dest, mf.OmitIf, err)
				}
			}
			if mf.MapValuesExclusive && len(mf.MapValues) == 0 {
				return fmt.Errorf("logging processor %q field %q cannot set map_values_exclusive without map_values", id, dest)
			}
		}
		return nil
	default:
		return fmt.Errorf("logging processor %q with type %q is not supported", id, p.Type)
	}
}

func (p LoggingProcessor) targetFieldAccessor() (ottlLValue, error) {
	from := p.Field
	if from == "" {
		from = "jsonPayload.message"
	}
	m, err := newLogMemberLegacy(from)
	if err != nil {
		return nil, err
	}
	return m.OTTLAccessor()
}

func (p LoggingProcessor) timestampStatements() (ottlStatements, error) {
	if p.TimeKey == "" {
		return nil, nil
	}
	from, err := newLogMemberLegacy(p.TimeKey)
	if err != nil {
		return nil, err
	}
	fromAccessor, err := from.OTTLAccessor()
	if err != nil {
		return nil, err
	}
	flag := ottlLValue{"cache", "__time_valid"}
	return newOTTLStatements(
		flag.Set(ottlFalse()),
		flag.SetIf(ottlTrue(), ottlAnd(
			fromAccessor.IsPresent(),
			ottlIsNotNil(ottlToTime(fromAccessor, p.TimeFormat)),
		)),
		ottlLValue{"time"}.SetIf(ottlToTime(fromAccessor, p.TimeFormat), ottlEquals(flag, ottlTrue())),
		fromAccessor.DeleteIf(ottlEquals(flag, ottlTrue())),
	), nil
}

func (p LoggingProcessor) specialFieldsStatements() (ottlStatements, error) {
	labels := ottlLValue{"body", "logging.googleapis.com/labels"}
	statements := newOTTLStatements(
		ottlLValue{"attributes"}.MergeMaps(labels, "upsert"),
		labels.Delete(),
	)
	for _, f := range slices.Sorted(maps.Keys(specialFieldsMap)) {
		dest := specialFieldsMap[f]
		if dest == "labels" {
			continue
		}
		s, err := (LoggingProcessor{
			Type: "modify_fields",
			Fields: map[string]*ModifyField{
				dest: {
					MoveFrom: fmt.Sprintf(`jsonPayload.%q`, f),
				},
			},
		}).modifyFieldsStatements()
		if err != nil {
			return nil, err
		}
		statements = statements.Append(s)
	}

	funcOld := ottlLValue{"attributes", "gcp.source_location", "function"}
	funcNew := ottlLValue{"attributes", "gcp.source_location", "func"}
	statements = statements.Append(newOTTLStatements(
		funcNew.SetIf(funcOld, funcOld.IsPresent()),
		funcOld.DeleteIf(funcNew.IsPresent()),
	))

	return statements, nil
}

func (p LoggingProcessor) postParseStatements() (ottlStatements, error) {
	ts, err := p.timestampStatements()
	if err != nil {
		return nil, err
	}
	sf, err := p.specialFieldsStatements()
	if err != nil {
		return nil, err
	}
	return ts.Append(sf), nil
}

func (p LoggingProcessor) parseJSONStatements() (ottlStatements, error) {
	fromAccessor, err := p.targetFieldAccessor()
	if err != nil {
		return nil, err
	}

	cachedJSON := ottlLValue{"cache", "__parsed_json"}
	statements := newOTTLStatements(
		cachedJSON.SetIf(ottlParseJSON(fromAccessor), fromAccessor.IsPresent()),
		fromAccessor.DeleteIf(cachedJSON.IsPresent()),
		ottlLValue{"body"}.MergeMapsIf(cachedJSON, "upsert", cachedJSON.IsPresent()),
		cachedJSON.Delete(),
	)

	post, err := p.postParseStatements()
	if err != nil {
		return nil, err
	}
	return statements.Append(post), nil
}

func (p LoggingProcessor) parseRegexStatements() (ottlStatements, error) {
	fromAccessor, err := p.targetFieldAccessor()
	if err != nil {
		return nil, err
	}

	cachedParsedRegex := ottlLValue{"cache", "__parsed_regex"}
	statements := newOTTLStatements(
		cachedParsedRegex.SetIf(ottlExtractPatternsRubyRegex(fromAccessor, p.Regex, true), ottlAnd(
			fromAccessor.IsPresent(),
			ottlIsMatchRubyRegex(fromAccessor, p.Regex),
		)),
		fromAccessor.DeleteIf(cachedParsedRegex.IsPresent()),
		ottlLValue{"body"}.MergeMapsIf(cachedParsedRegex, "upsert", cachedParsedRegex.IsPresent()),
		cachedParsedRegex.Delete(),
	)

	post, err := p.postParseStatements()
	if err != nil {
		return nil, err
	}
	return statements.Append(post), nil
}

func (p LoggingProcessor) modifyFieldsStatements() (ottlStatements, error) {
	var statements ottlStatements

	var dests []string
	for dest, field := range p.Fields {
		if field == nil {
			continue
		}
		dests = append(dests, dest)
	}
	sort.Strings(dests)

	fieldMappings := map[string]ottlLValue{}
	var moveFromFields []ottlLValue
	var omitFilters []*logFilter
	sourceValues := make(map[string]ottlLValue, len(dests))
	omitVars := make(map[string]string, len(dests))

	for i, dest := range dests {
		field := *p.Fields[dest]
		if field.MoveFrom == "" && field.CopyFrom == "" && field.StaticValue == nil {
			field.CopyFrom = dest
		}
		for j, name := range []string{field.MoveFrom, field.CopyFrom} {
			if name == "" {
				continue
			}
			m, err := newLogMember(name)
			if err != nil {
				return nil, fmt.Errorf("failed to parse field %q: %w", name, err)
			}
			accessor, err := m.OTTLAccessor()
			if err != nil {
				return nil, fmt.Errorf("failed to convert field %q to OTTL: %w", name, err)
			}
			key := accessor.String()
			if _, ok := fieldMappings[key]; !ok {
				cached := ottlLValue{"cache", fmt.Sprintf("__field_%d", i)}
				fieldMappings[key] = cached
				statements = statements.Append(
					cached.Delete(),
					cached.SetIf(accessor, accessor.IsPresent()),
				)
			}
			sourceValues[dest] = fieldMappings[key]
			if j == 0 {
				moveFromFields = append(moveFromFields, accessor)
			}
		}
		if field.OmitIf != "" {
			f, err := newLogFilter(field.OmitIf)
			if err != nil {
				return nil, fmt.Errorf("failed to parse filter %q: %w", field.OmitIf, err)
			}
			omitVars[dest] = fmt.Sprintf("__omit_%d", len(omitFilters))
			omitFilters = append(omitFilters, f)
		}
	}

	for i, f := range omitFilters {
		name := fmt.Sprintf("__omit_%d", i)
		expr, err := f.OTTLExpression()
		if err != nil {
			return nil, fmt.Errorf("failed to parse omit_if condition %q: %w", f, err)
		}
		statements = statements.Append(
			ottlLValue{"cache", name}.Set(ottlFalse()),
			ottlLValue{"cache", name}.SetIf(ottlTrue(), expr),
		)
	}

	sort.Slice(moveFromFields, func(i, j int) bool {
		return moveFromFields[i].String() < moveFromFields[j].String()
	})
	var last ottlLValue
	for _, v := range moveFromFields {
		if !slices.Equal(last, v) {
			statements = statements.Append(v.Delete())
		}
		last = v
	}

	if p.EmptyBody {
		statements = statements.Append(
			ottlLValue{"cache", "body"}.Set(ottlLValue{"body"}),
			ottlLValue{"body"}.KeepKeys(),
		)
	}

	for _, dest := range dests {
		field := p.Fields[dest]
		outM, err := newLogMember(dest)
		if err != nil {
			return nil, fmt.Errorf("failed to parse output field %q: %w", dest, err)
		}

		value := ottlLValue{"cache", "value"}
		statements = statements.Append(value.Delete())
		if field.StaticValue != nil {
			statements = statements.Append(value.Set(ottlStringLiteral(*field.StaticValue)))
		} else if srcVal, ok := sourceValues[dest]; ok {
			statements = statements.Append(value.SetIf(srcVal, srcVal.IsPresent()))
		}
		if field.DefaultValue != nil {
			statements = statements.Append(value.SetIfNil(ottlStringLiteral(*field.DefaultValue)))
		}

		if len(field.MapValues) > 0 {
			mappedValue := ottlLValue{"cache", "mapped_value"}
			statements = statements.Append(mappedValue.Delete())
			if !field.MapValuesExclusive {
				statements = statements.Append(mappedValue.SetIf(value, value.IsPresent()))
			}
			for _, k := range slices.Sorted(maps.Keys(field.MapValues)) {
				statements = statements.Append(
					mappedValue.SetIf(ottlStringLiteral(field.MapValues[k]), ottlEquals(value, ottlStringLiteral(k))),
				)
			}
			value = mappedValue
		}

		switch field.Type {
		case "integer":
			statements = statements.Append(value.SetIf(ottlToInt(value), ottlAnd(value.IsPresent(), ottlIsNotNil(ottlToInt(value)))))
		case "float":
			statements = statements.Append(value.SetIf(ottlToFloat(value), ottlAnd(value.IsPresent(), ottlIsNotNil(ottlToFloat(value)))))
		}

		if field.CustomConvertFunc != nil {
			statements = statements.Append(field.CustomConvertFunc(value))
		}

		ra, err := outM.OTTLAccessor()
		if err != nil {
			return nil, fmt.Errorf("failed to convert %v to OTTL accessor: %w", outM, err)
		}
		statements = statements.Append(ra.SetIf(value, value.IsPresent()))

		if omitVar := omitVars[dest]; omitVar != "" {
			statements = statements.Append(ra.DeleteIf(ottlEquals(ottlLValue{"cache", omitVar}, ottlTrue())))
		}
	}

	return statements, nil
}

type otelProcessorSpec struct {
	componentType string
	config        map[string]any
}

func (p LoggingProcessor) buildProcessors() ([]otelProcessorSpec, error) {
	switch p.Type {
	case "parse_json":
		stmts, err := p.parseJSONStatements()
		if err != nil {
			return nil, err
		}
		return []otelProcessorSpec{{
			componentType: "transform",
			config:        logTransformIgnoreProcessor(stmts...),
		}}, nil
	case "parse_regex":
		stmts, err := p.parseRegexStatements()
		if err != nil {
			return nil, err
		}
		return []otelProcessorSpec{{
			componentType: "transform",
			config:        logTransformIgnoreProcessor(stmts...),
		}}, nil
	case "exclude_logs":
		expressions := make([]string, 0, len(p.MatchAny))
		for _, cond := range p.MatchAny {
			f, err := newLogFilter(cond)
			if err != nil {
				return nil, err
			}
			expr, err := f.OTTLExpression()
			if err != nil {
				return nil, err
			}
			expressions = append(expressions, expr.String())
		}
		return []otelProcessorSpec{{
			componentType: "filter",
			config:        logFilterProcessor(expressions...),
		}}, nil
	case "modify_fields":
		stmts, err := p.modifyFieldsStatements()
		if err != nil {
			return nil, err
		}
		if len(stmts) == 0 {
			return nil, nil
		}
		return []otelProcessorSpec{{
			componentType: "transform",
			config:        logTransformIgnoreProcessor(stmts...),
		}}, nil
	default:
		return nil, fmt.Errorf("unsupported logging processor type %q", p.Type)
	}
}
