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

package googlepolicy

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc/grpclog"
)

// zapDepthLogger bridges google.golang.org/grpc/grpclog.DepthLoggerV2 to a
// Collector *zap.Logger so that all xdsclient PrefixLogger output is emitted
// through the Collector's structured logger.
type zapDepthLogger struct {
	logger *zap.Logger
}

var _ grpclog.DepthLoggerV2 = (*zapDepthLogger)(nil)

func newZapDepthLogger(logger *zap.Logger) grpclog.DepthLoggerV2 {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &zapDepthLogger{logger: logger.WithOptions(zap.AddCallerSkip(2))}
}

func formatArgs(args ...any) string {
	return strings.TrimSuffix(fmt.Sprint(args...), "\n")
}

func formatArgsln(args ...any) string {
	return strings.TrimSuffix(fmt.Sprintln(args...), "\n")
}

func (l *zapDepthLogger) Info(args ...any) {
	l.logger.Debug(formatArgs(args...))
}

func (l *zapDepthLogger) Infoln(args ...any) {
	l.logger.Debug(formatArgsln(args...))
}

func (l *zapDepthLogger) Infof(format string, args ...any) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}

func (l *zapDepthLogger) InfoDepth(_ int, args ...any) {
	l.logger.Debug(formatArgs(args...))
}

func (l *zapDepthLogger) Warning(args ...any) {
	l.logger.Warn(formatArgs(args...))
}

func (l *zapDepthLogger) Warningln(args ...any) {
	l.logger.Warn(formatArgsln(args...))
}

func (l *zapDepthLogger) Warningf(format string, args ...any) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

func (l *zapDepthLogger) WarningDepth(_ int, args ...any) {
	l.logger.Warn(formatArgs(args...))
}

func (l *zapDepthLogger) Error(args ...any) {
	l.logger.Error(formatArgs(args...))
}

func (l *zapDepthLogger) Errorln(args ...any) {
	l.logger.Error(formatArgsln(args...))
}

func (l *zapDepthLogger) Errorf(format string, args ...any) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

func (l *zapDepthLogger) ErrorDepth(_ int, args ...any) {
	l.logger.Error(formatArgs(args...))
}

func (l *zapDepthLogger) Fatal(args ...any) {
	l.logger.Fatal(formatArgs(args...))
}

func (l *zapDepthLogger) Fatalln(args ...any) {
	l.logger.Fatal(formatArgsln(args...))
}

func (l *zapDepthLogger) Fatalf(format string, args ...any) {
	l.logger.Fatal(fmt.Sprintf(format, args...))
}

func (l *zapDepthLogger) FatalDepth(_ int, args ...any) {
	l.logger.Fatal(formatArgs(args...))
}

func (l *zapDepthLogger) V(_ int) bool {
	return true
}
