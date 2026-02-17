package api

// This file contains the gRPC service definitions.
// In a full build these would be generated from proto/api/v1/service.proto.
// For the MVP we define them manually to avoid requiring protoc.

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatusRequest is the request for GetStatus.
type StatusRequest struct{}

// StatusResponse is the response for GetStatus.
type StatusResponse struct {
	Status    string
	ConfigDir string
}

// DeployRelayRequest is the request for DeployRelay.
type DeployRelayRequest struct {
	Domain string
}

// DeployRelayResponse is the response for DeployRelay.
type DeployRelayResponse struct {
	Message string
}

// TunnelInfo describes a single tunnel.
type TunnelInfo struct {
	Name   string
	Status string
}

// ListTunnelsRequest is the request for ListTunnels.
type ListTunnelsRequest struct{}

// ListTunnelsResponse is the response for ListTunnels.
type ListTunnelsResponse struct {
	Tunnels []TunnelInfo
}

// TunnelWhispererServer is the gRPC service interface.
type TunnelWhispererServer interface {
	GetStatus(ctx context.Context, req *StatusRequest) (*StatusResponse, error)
	DeployRelay(ctx context.Context, req *DeployRelayRequest) (*DeployRelayResponse, error)
	ListTunnels(ctx context.Context, req *ListTunnelsRequest) (*ListTunnelsResponse, error)
}

// RegisterTunnelWhispererServer registers the service with a gRPC server.
// This is a manual registration; in production, protoc-gen-go-grpc generates this.
func RegisterTunnelWhispererServer(s *grpc.Server, srv TunnelWhispererServer) {
	sd := grpc.ServiceDesc{
		ServiceName: "api.v1.TunnelWhisperer",
		HandlerType: (*TunnelWhispererServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "GetStatus",
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					req := new(StatusRequest)
					if err := dec(req); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(TunnelWhispererServer).GetStatus(ctx, req)
					}
					info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/api.v1.TunnelWhisperer/GetStatus"}
					return interceptor(ctx, req, info, func(ctx context.Context, req interface{}) (interface{}, error) {
						return srv.(TunnelWhispererServer).GetStatus(ctx, req.(*StatusRequest))
					})
				},
			},
			{
				MethodName: "DeployRelay",
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					req := new(DeployRelayRequest)
					if err := dec(req); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(TunnelWhispererServer).DeployRelay(ctx, req)
					}
					info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/api.v1.TunnelWhisperer/DeployRelay"}
					return interceptor(ctx, req, info, func(ctx context.Context, req interface{}) (interface{}, error) {
						return srv.(TunnelWhispererServer).DeployRelay(ctx, req.(*DeployRelayRequest))
					})
				},
			},
			{
				MethodName: "ListTunnels",
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					req := new(ListTunnelsRequest)
					if err := dec(req); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(TunnelWhispererServer).ListTunnels(ctx, req)
					}
					info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/api.v1.TunnelWhisperer/ListTunnels"}
					return interceptor(ctx, req, info, func(ctx context.Context, req interface{}) (interface{}, error) {
						return srv.(TunnelWhispererServer).ListTunnels(ctx, req.(*ListTunnelsRequest))
					})
				},
			},
		},
		Streams: []grpc.StreamDesc{},
	}
	s.RegisterService(&sd, srv)
}

// UnimplementedTunnelWhispererServer provides default implementations that return Unimplemented.
type UnimplementedTunnelWhispererServer struct{}

func (UnimplementedTunnelWhispererServer) GetStatus(context.Context, *StatusRequest) (*StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "GetStatus not implemented")
}

func (UnimplementedTunnelWhispererServer) DeployRelay(context.Context, *DeployRelayRequest) (*DeployRelayResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "DeployRelay not implemented")
}

func (UnimplementedTunnelWhispererServer) ListTunnels(context.Context, *ListTunnelsRequest) (*ListTunnelsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "ListTunnels not implemented")
}
