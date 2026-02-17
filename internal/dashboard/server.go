package dashboard

import (
	"encoding/json"
	"log"
	"net/http"
)

// Server serves the web dashboard.
type Server struct {
	addr string
	mux  *http.ServeMux
}

func NewServer(addr string) *Server {
	s := &Server{
		addr: addr,
		mux:  http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/", s.handleIndex)
}

// Run starts the HTTP server (blocking).
func (s *Server) Run() error {
	log.Printf("dashboard: listening on %s", s.addr)
	return http.ListenAndServe(s.addr, s.mux)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "running",
		"version": "0.1.0-dev",
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Tunnel Whisperer</title></head>
<body>
  <h1>Tunnel Whisperer Dashboard</h1>
  <p>Web UI coming soon. API available at <a href="/api/status">/api/status</a>.</p>
</body>
</html>`))
}
