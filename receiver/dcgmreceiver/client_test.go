// Copyright 2023 Google LLC
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

//go:build gpu && !has_gpu
// +build gpu,!has_gpu

package dcgmreceiver

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestNewDcgmClientOnInitializationError(t *testing.T) {
	config := createDefaultConfig().(*Config)
	client, err := newClient(config, zaptest.NewLogger(t))
	assert.True(t, errors.Is(err, ErrDcgmInitialization))
	assert.Regexp(t, ".*cannot initialize a DCGM client; DCGM is not installed.*", err)
	assert.Nil(t, client)
}
