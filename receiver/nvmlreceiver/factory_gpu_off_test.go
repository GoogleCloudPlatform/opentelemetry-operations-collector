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

//go:build !gpu
// +build !gpu

package nvmlreceiver

import (
	"context"
	"errors"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver/internal/metadata"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

func TestCreateMetricsReceiverWithGPUSupportOff(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	receiver, err := factory.CreateMetrics(
		context.Background(),
		receivertest.NewNopSettings(metadata.Type),
		cfg,
		consumertest.NewNop())

	require.True(t, errors.Is(err, ErrGPUSupportDisabled))
	require.Nil(t, receiver)
}
