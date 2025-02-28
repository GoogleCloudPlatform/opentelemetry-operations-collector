// Package client implements client interface for service control API
package googleservicecontrolexporter

import (
	"context"

	servicecontrol "cloud.google.com/go/servicecontrol/apiv1"
	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"go.uber.org/zap"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type HeaderLoggingInterceptor struct {
	logger *zap.SugaredLogger
}

// ServiceControlClient defines a interface of client for service control API
type ServiceControlClient interface {
	Report(ctx context.Context, request *scpb.ReportRequest) (*scpb.ReportResponse, error)
	Close() error
}

type serviceControlClientRaw struct {
	service scpb.ServiceControllerClient
}

type serviceControlClientLibrary struct {
	service *servicecontrol.ServiceControllerClient
}

// New is constructor for service control client
func New(endpoint string, useRawServicecontrolClient bool, enableDebugHeaders bool, logger *zap.Logger, opts ...grpc.DialOption) (ServiceControlClient, error) {
	ctx := context.Background()
	// Use client library. Ignore grpc dial options.
	if !useRawServicecontrolClient {
		// Enable gRPC response interceptor for debug header
		var clientOpts []option.ClientOption
		if enableDebugHeaders {
			interceptor := NewHeaderLoggingInterceptor(logger)
			clientOpts = append(clientOpts, option.WithGRPCDialOption(grpc.WithUnaryInterceptor(interceptor.UnaryInterceptor)))
		}
		clientOpts = append(clientOpts, option.WithEndpoint(endpoint))

		c, err := servicecontrol.NewServiceControllerClient(ctx, clientOpts...)
		if err != nil {
			return nil, err
		}

		return &serviceControlClientLibrary{
			service: c,
		}, nil
	}

	// Use raw client.
	conn, err := grpc.DialContext(ctx, endpoint, opts...)
	if err != nil {
		return nil, err
	}

	return &serviceControlClientRaw{
		service: scpb.NewServiceControllerClient(conn),
	}, nil
}

func (c *serviceControlClientRaw) Report(ctx context.Context, request *scpb.ReportRequest) (*scpb.ReportResponse, error) {
	return c.service.Report(ctx, request)
}
func (c *serviceControlClientRaw) Close() error {
	// There is no Close function in basic version.
	return nil
}
func (c *serviceControlClientLibrary) Report(ctx context.Context, request *scpb.ReportRequest) (*scpb.ReportResponse, error) {
	return c.service.Report(ctx, request)
}
func (c *serviceControlClientLibrary) Close() error {
	return c.service.Close()
}

func NewHeaderLoggingInterceptor(logger *zap.Logger) *HeaderLoggingInterceptor {
	return &HeaderLoggingInterceptor{
		logger: logger.Sugar(),
	}
}

// UnaryInterceptor implements grpc.UnaryClientInterceptor interface
func (h *HeaderLoggingInterceptor) UnaryInterceptor(
	ctx context.Context,
	method string,
	req interface{},
	reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	var respHeaders metadata.MD
	opts = append(opts, grpc.Header(&respHeaders))
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err != nil {
		h.logger.Infof("Request failed for method %s, debug response headers:%v", method, respHeaders)
		return err
	}
	h.logger.Infof("Method: %s, Received response headers: %v", method, respHeaders)
	return nil
}
