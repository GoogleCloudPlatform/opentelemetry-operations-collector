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

package googlepolicyprocessor

import (
	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"go.opentelemetry.io/collector/pdata/plog"
)

type compiledLogPolicy struct {
	id       string
	action   policyv1alpha1.Action
	matchers []*compiledLogMatcher
}

type compiledLogMatcher struct {
	target *policyv1alpha1.LogFieldSelector
	pred   matcherPredicate
}

func compileLogPolicy(p *policyv1alpha1.LogFilterPolicy) (*compiledLogPolicy, error) {
	cp := &compiledLogPolicy{
		id:     p.GetId(),
		action: p.GetAction(),
	}
	for _, m := range p.GetMatches() {
		pred, err := compilePredicate(m.GetPredicate(), m.GetNegate())
		if err != nil {
			return nil, err
		}
		cp.matchers = append(cp.matchers, &compiledLogMatcher{
			target: m.GetTarget(),
			pred:   pred,
		})
	}
	return cp, nil
}

// TransformLogs applies active transformation policies in-place across the Resource -> Scope -> Record hierarchy.
// Dropped records are pruned, empty scopes/resources are removed, and batch transformation stats are returned.
func (e *Evaluator) TransformLogs(ld plog.Logs) TransformStats {
	stats := newTransformStats()
	if len(e.logPolicies) == 0 {
		return stats
	}

	ld.ResourceLogs().RemoveIf(func(rl plog.ResourceLogs) bool {
		resource := rl.Resource()
		resourceSchemaURL := rl.SchemaUrl()

		rl.ScopeLogs().RemoveIf(func(sl plog.ScopeLogs) bool {
			scope := sl.Scope()
			scopeSchemaURL := sl.SchemaUrl()

			sl.LogRecords().RemoveIf(func(lr plog.LogRecord) bool {
				ctx := LogContext{
					Record:            lr,
					Resource:          resource,
					Scope:             scope,
					ResourceSchemaURL: resourceSchemaURL,
					ScopeSchemaURL:    scopeSchemaURL,
				}
				res := e.evaluateLog(ctx)
				if res.Drop {
					stats.Dropped++
				} else if len(res.Evaluations) > 0 {
					stats.Kept++
				} else {
					stats.NoMatch++
				}
				for _, ev := range res.Evaluations {
					stats.recordPolicy(ev.PolicyID, ev.Result)
				}
				return res.Drop
			})

			return sl.LogRecords().Len() == 0
		})

		return rl.ScopeLogs().Len() == 0
	})

	return stats
}

type logEvalResult struct {
	Drop        bool
	Evaluations []PolicyEvaluation
}

func (e *Evaluator) evaluateLog(ctx LogContext) logEvalResult {
	if len(e.logPolicies) == 0 {
		return logEvalResult{Drop: false}
	}

	var matchingPolicies []*compiledLogPolicy
	var hasKeep, hasDrop bool
	for _, p := range e.logPolicies {
		if p.matches(ctx) {
			matchingPolicies = append(matchingPolicies, p)
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				hasKeep = true
			} else if p.action == policyv1alpha1.Action_ACTION_DROP {
				hasDrop = true
			}
		}
	}

	var res logEvalResult
	if hasKeep {
		res.Drop = false
		for _, p := range matchingPolicies {
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultKept})
			} else {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultNoMatch})
			}
		}
	} else if hasDrop {
		res.Drop = true
		for _, p := range matchingPolicies {
			res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultDropped})
		}
	} else {
		res.Drop = false
	}

	return res
}

// EvalLog returns true if the log record should be DROPPED, false if it should be KEPT.
func (e *Evaluator) EvalLog(ctx LogContext) bool {
	return e.evaluateLog(ctx).Drop
}

func (p *compiledLogPolicy) matches(ctx LogContext) bool {
	for _, m := range p.matchers {
		val, exists := extractLogTarget(ctx, m.target)
		if !m.pred.evaluate(val, exists) {
			return false
		}
	}
	return true
}

func extractLogTarget(ctx LogContext, target *policyv1alpha1.LogFieldSelector) (any, bool) {
	if target == nil {
		return nil, false
	}
	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_RecordField:
		switch t.RecordField {
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
			s := ctx.Record.Body().AsString()
			return s, s != ""
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
			s := ctx.Record.SeverityText()
			return s, s != ""
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
			n := int64(ctx.Record.SeverityNumber())
			return n, n != 0
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID:
			tid := ctx.Record.TraceID()
			if tid.IsEmpty() {
				return nil, false
			}
			return tid.String(), true
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
			sid := ctx.Record.SpanID()
			if sid.IsEmpty() {
				return nil, false
			}
			return sid.String(), true
		default:
			return nil, false
		}
	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		return lookupPath(ctx.Record.Attributes(), t.LogAttribute.GetPath())
	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		return lookupPath(ctx.Resource.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		return lookupPath(ctx.Scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.LogFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			s := ctx.Scope.Name()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			s := ctx.Scope.Version()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			return ctx.ScopeSchemaURL, ctx.ScopeSchemaURL != ""
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}
