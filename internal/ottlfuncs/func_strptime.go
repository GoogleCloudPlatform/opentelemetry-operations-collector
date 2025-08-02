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

	strptime "github.com/itchyny/timefmt-go"
)

type StrptimeArguments[K any] struct {
	Time   ottl.StringGetter[K]
	Format string
}

func NewStrptimeFactory[K any]() ottl.Factory[K] {
	return ottl.NewFactory("Strptime", &StrptimeArguments[K]{}, createStrptimeFunction[K])
}

func createStrptimeFunction[K any](_ ottl.FunctionContext, oArgs ottl.Arguments) (ottl.ExprFunc[K], error) {
	args, ok := oArgs.(*StrptimeArguments[K])

	if !ok {
		return nil, errors.New("StrptimeFactory args must be of type *StrptimeArguments[K]")
	}

	return Strptime(args.Time, args.Format)
}

func Strptime[K any](inputTime ottl.StringGetter[K], format string) (ottl.ExprFunc[K], error) {
	if format == "" {
		return nil, errors.New("format cannot be nil")
	}

	return func(ctx context.Context, tCtx K) (any, error) {
		t, err := inputTime.Get(ctx, tCtx)
		if err != nil {
			return nil, err
		}
		if t == "" {
			return nil, errors.New("time cannot be nil")
		}

		timestamp, err := strptime.Parse(t, format)
		if err != nil {
			return nil, err
		}

		return timestamp, nil
	}, nil
}
