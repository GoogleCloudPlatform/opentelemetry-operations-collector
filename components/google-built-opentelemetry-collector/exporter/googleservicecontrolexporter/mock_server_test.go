// Copyright 2025 Google LLC
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

package googleservicecontrolexporter

import (
	"context"
	"testing"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestHeaderLoggingInterceptor(t *testing.T) {
	mockServer, err := StartInMemoryMockServer()
	defer StopMockServer(mockServer)
	require.NoError(t, err)

	mockServer.SetReturnFunc(func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error) {
		md := metadata.Pairs(debugHeaderKey, "This is debug encrypted response value.")
		grpc.SendHeader(ctx, md)
		return &scpb.ReportResponse{}, nil
	})

	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	interceptor := NewHeaderLoggingInterceptor(logger)

	conn, err := grpc.DialContext(
		context.Background(),
		"bufconn",
		grpc.WithInsecure(),
		grpc.WithContextDialer(mockServer.DialFunc),
		grpc.WithUnaryInterceptor(interceptor.UnaryInterceptor),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := scpb.NewServiceControllerClient(conn)

	req := &scpb.ReportRequest{
		ServiceConfigId: testServiceConfigID,
	}

	_, err = client.Report(context.Background(), req)
	require.NoError(t, err, "Report call should succeed")

	expectedLogMessage := "Method: /google.api.servicecontrol.v1.ServiceController/Report, Received response headers: map[content-type:[application/grpc] x-return-encrypted-headers:[This is debug encrypted response value.]]"
	logEntries := logs.All()
	require.Len(t, logEntries, 1, "Expected one log entry for response headers")

	require.Equal(t, expectedLogMessage, logEntries[0].Message, "Log message does not match expected")
}
