// Copyright 2025 Google LLC
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

package ottlfuncs

import (
	"context"
	"errors"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/ottlfuncs"
)

type TryParseTimeArguments[K any] struct {
	Time     ottl.StringGetter[K]
	Formats  []string
	Location ottl.Optional[string]
	Locale   ottl.Optional[string]
}

func NewTryParseTimeFactory[K any]() ottl.Factory[K] {
	return ottl.NewFactory("TryParseTime", &TryParseTimeArguments[K]{}, createTryParseTimeFunction[K])
}

func createTryParseTimeFunction[K any](_ ottl.FunctionContext, oArgs ottl.Arguments) (ottl.ExprFunc[K], error) {
	args, ok := oArgs.(*TryParseTimeArguments[K])

	if !ok {
		return nil, errors.New("TryParseTimeFactory args must be of type *TryParseTimeArguments[K]")
	}

	return TryParseTime(args.Time, args.Formats, args.Location, args.Locale)
}

func TryParseTime[K any](inputTryParseTime ottl.StringGetter[K], formats []string, location, locale ottl.Optional[string]) (ottl.ExprFunc[K], error) {
	if len(formats) == 0 {
		return nil, errors.New("formats cannot be empty")
	}

	var exprFuncSlice []ottl.ExprFunc[K]
	for _, format := range formats {
		exprFunc, exprErr := ottlfuncs.Time(inputTryParseTime, format, location, locale)
		if exprErr != nil {
			return nil, exprErr
		}
		exprFuncSlice = append(exprFuncSlice, exprFunc)
	}

	return func(ctx context.Context, tCtx K) (any, error) {
		var multiErr error
		for _, exprFunc := range exprFuncSlice {
			timestamp, err := exprFunc(ctx, tCtx)
			if err == nil {
				return timestamp, nil
			}
			multiErr = errors.Join(multiErr, err)
		}
		return nil, multiErr
	}, nil
}
