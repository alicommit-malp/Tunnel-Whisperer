package api

import (
	"log/slog"
	"net"

	"github.com/tunnelwhisperer/tw/internal/core"
	"google.golang.org/grpc"
)

// Server wraps a gRPC server and the core service.
type Server struct {
	core *core.Service
	addr string
	gs   *grpc.Server
}

func NewServer(svc *core.Service, addr string) *Server {
	gs := grpc.NewServer()
	s := &Server{
		core: svc,
		addr: addr,
		gs:   gs,
	}
	RegisterTunnelWhispererServer(gs, &handler{core: svc})
	return s
}

// Run starts the gRPC server (blocking).
func (s *Server) Run() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	slog.Info("gRPC server listening", "addr", s.addr)
	return s.gs.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	s.gs.GracefulStop()
}
