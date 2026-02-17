package api

import (
	"context"

	"github.com/tunnelwhisperer/tw/internal/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type handler struct {
	UnimplementedTunnelWhispererServer
	core *core.Service
}

func (h *handler) GetStatus(ctx context.Context, req *StatusRequest) (*StatusResponse, error) {
	s := h.core.Status()
	return &StatusResponse{
		Status:    s["status"],
		ConfigDir: s["configDir"],
	}, nil
}

func (h *handler) DeployRelay(ctx context.Context, req *DeployRelayRequest) (*DeployRelayResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "DeployRelay: not yet implemented")
}

func (h *handler) ListTunnels(ctx context.Context, req *ListTunnelsRequest) (*ListTunnelsResponse, error) {
	return &ListTunnelsResponse{Tunnels: nil}, nil
}
