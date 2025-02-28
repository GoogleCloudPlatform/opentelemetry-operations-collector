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
	"net"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

var lis *bufconn.Listener

type mockServiceControllerServer struct {
	scpb.UnimplementedServiceControllerServer
	CallCount  int
	returnFunc func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error)
}

func (s *mockServiceControllerServer) Report(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error) {
	if s.returnFunc != nil {
		s.CallCount++
		return s.returnFunc(ctx, req)
	}
	return &scpb.ReportResponse{}, nil
}

func (s *mockServiceControllerServer) SetReturnFunc(f func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error)) {
	s.returnFunc = f
}

func StartMockServer() (*grpc.Server, *mockServiceControllerServer, error) {
	lis = bufconn.Listen(bufSize)
	server := grpc.NewServer()
	scs := &mockServiceControllerServer{
		CallCount: 0,
	}
	scpb.RegisterServiceControllerServer(server, scs)

	go func() {
		if err := server.Serve(lis); err != nil {
			panic(err)
		}
	}()
	return server, scs, nil
}

func BufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}
