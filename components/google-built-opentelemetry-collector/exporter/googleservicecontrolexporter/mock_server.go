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
	"sync"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type mockServiceControllerServer struct {
	scpb.UnimplementedServiceControllerServer

	Requests []*scpb.ReportRequest
	Address  string
	DialFunc func(context.Context, string) (net.Conn, error)

	returnFunc func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error)
	lis        net.Listener
	server     *grpc.Server
	// mutex lock to prevent concurrent appending to requests
	mutex sync.Mutex
}

func (s *mockServiceControllerServer) Report(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error) {
	s.mutex.Lock()
	s.Requests = append(s.Requests, req)
	s.mutex.Unlock()
	if s.returnFunc != nil {
		return s.returnFunc(ctx, req)
	}
	return &scpb.ReportResponse{}, nil
}

func (s *mockServiceControllerServer) CallCount() int {
	return len(s.Requests)
}

func (s *mockServiceControllerServer) SetReturnFunc(f func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error)) {
	s.returnFunc = f
}

// StartInMemoryMockServer starts a mock server with a bufconn (in-memory channel)
// listener. Mainly used for unit tests.
func StartInMemoryMockServer() (*mockServiceControllerServer, error) {
	lis := bufconn.Listen(bufSize)
	scs := &mockServiceControllerServer{
		DialFunc: func(ctx context.Context, s string) (net.Conn, error) { return lis.Dial() },
	}
	return startMockServer(lis, scs)
}

// StartNetworkMockServer starts a mock server with a network listener (binding to a port)
// Mainly used for integration tests.
func StartNetworkMockServer() (*mockServiceControllerServer, error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}
	scs := &mockServiceControllerServer{
		Address: lis.Addr().String(),
	}
	return startMockServer(lis, scs)
}

func startMockServer(lis net.Listener, scs *mockServiceControllerServer) (*mockServiceControllerServer, error) {
	server := grpc.NewServer()
	scs.Requests = make([]*scpb.ReportRequest, 0)
	scpb.RegisterServiceControllerServer(server, scs)

	go func() {
		if err := server.Serve(lis); err != nil {
			panic(err)
		}
	}()
	scs.lis = lis
	scs.server = server
	return scs, nil
}

func StopMockServer(scs *mockServiceControllerServer) {
	scs.server.GracefulStop()
	scs.lis.Close()
}
