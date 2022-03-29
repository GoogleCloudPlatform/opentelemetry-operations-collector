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

package levelchanger

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type LevelChangeCondition func(entry zapcore.Entry, fields []zapcore.Field) bool

type levelChangerCore struct {
	next       zapcore.Core
	fromLevel  zapcore.Level
	toLevel    zapcore.Level
	conditions []LevelChangeCondition
}

// This core is enabled at the requested level if the core it wraps
// is enabled at the requested level.
func (l levelChangerCore) Enabled(level zapcore.Level) bool {
	return l.next.Enabled(level)
}

// This core does not allow adding additional context.
func (l levelChangerCore) With([]zapcore.Field) zapcore.Core { return l }

// This core will always add itself to the checked entry, since the Write
// method will determine whether the entry continues to the next core.
func (l levelChangerCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.AddCore(entry, l)
}

// Check if the log passes any core conditions, and if the log level matches the fromLevel,
// change the log's level to the toLevel.
func (l levelChangerCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Always pass if there are no conditions, otherwise check if any conditions are met.
	changeLevels := len(l.conditions) == 0
	for _, condition := range l.conditions {
		changeLevels = changeLevels || condition(entry, fields)
	}

	if changeLevels && entry.Level == l.fromLevel {
		entry.Level = l.toLevel
	}

	// Check if the next core is enabled at the (potentially) new log level.
	if !l.next.Enabled(entry.Level) {
		return nil
	}
	return l.next.Write(entry, fields)
}

// No special syncing is required for this core.
func (levelChangerCore) Sync() error { return nil }

// Create a zap option that wraps a core with a new levelChangerCore.
func NewLevelChangerOption(from, to zapcore.Level, conditions ...LevelChangeCondition) zap.Option {
	return zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return levelChangerCore{
			next:       core,
			fromLevel:  from,
			toLevel:    to,
			conditions: conditions,
		}
	})
}

// Make a level change condition that passes if the Entry's Caller File contains a
// substring. Can be used to change the level of all logs from some file or package.
func FilePathLevelChangeCondition(pathSubstr string) LevelChangeCondition {
	return func(entry zapcore.Entry, fields []zapcore.Field) bool {
		return strings.Contains(entry.Caller.File, pathSubstr)
	}
}
