// Copyright 2022 Google LLC
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

package levelchanger_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/levelchanger"
)

type baseTestCase struct {
	name              string
	loggerLevel       zapcore.Level
	from              zapcore.Level
	to                zapcore.Level
	logWriteFunc      func(logger *zap.Logger)
	expectedLogLevels []zapcore.Level
}

func TestLevelChangerCoreNoConditions(t *testing.T) {
	t.Parallel()

	testCases := []baseTestCase{
		{
			name:        "changes level",
			loggerLevel: zapcore.DebugLevel,
			from:        zapcore.ErrorLevel,
			to:          zapcore.DebugLevel,
			logWriteFunc: func(logger *zap.Logger) {
				logger.Error("this should be debug")
			},
			expectedLogLevels: []zapcore.Level{zapcore.DebugLevel},
		},
		{
			name:        "does not output the log when it changes to level the logger doesn't allow",
			loggerLevel: zapcore.ErrorLevel,
			from:        zapcore.ErrorLevel,
			to:          zapcore.DebugLevel,
			logWriteFunc: func(logger *zap.Logger) {
				logger.Error("this should not get logged")
			},
			expectedLogLevels: []zapcore.Level{},
		},
		{
			name:        "only changes level of logs it's supposed to",
			loggerLevel: zapcore.DebugLevel,
			from:        zapcore.ErrorLevel,
			to:          zapcore.DebugLevel,
			logWriteFunc: func(logger *zap.Logger) {
				logger.Error("this should become debug")
				logger.Info("this should stay info")
			},
			expectedLogLevels: []zapcore.Level{zapcore.DebugLevel, zapcore.InfoLevel},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			observedCore, observedLogs := observer.New(testCase.loggerLevel)
			logger := zap.New(
				observedCore,
				levelchanger.NewLevelChangerOption(testCase.from, testCase.to))

			testCase.logWriteFunc(logger)

			allLogs := observedLogs.All()
			assert.Equal(t, len(testCase.expectedLogLevels), len(allLogs))
			for i, expectedLevel := range testCase.expectedLogLevels {
				assert.Equal(t, expectedLevel, allLogs[i].Level)
			}
		})
	}
}

func TestLevelChangerCoreFilePathCondition(t *testing.T) {
	t.Parallel()

	filename := "levelchanger_test.go"

	observedCore, observedLogs := observer.New(zapcore.DebugLevel)
	observedCoreOption := zap.WrapCore(func(zapcore.Core) zapcore.Core {
		return observedCore
	})

	from := zapcore.ErrorLevel
	to := zapcore.DebugLevel
	levelChangeOption := levelchanger.NewLevelChangerOption(
		from,
		to,
		levelchanger.FilePathLevelChangeCondition(filename))

	// Using a development logger will log at debug level and will populate the entry
	// with the calling file so we can test our condition with it.
	logger, _ := zap.NewDevelopment(observedCoreOption, levelChangeOption)

	logger.Error("should be debug")
	assert.Len(t, observedLogs.All(), 1)
	log := observedLogs.All()[0]
	assert.Equal(t, zapcore.DebugLevel, log.Level)
}
