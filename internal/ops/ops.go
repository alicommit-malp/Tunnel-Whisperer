package ops

import (
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/tunnelwhisperer/tw/internal/config"
)

// ProgressEvent describes one step in a long-running operation.
type ProgressEvent struct {
	Step    int    `json:"step"`
	Total   int    `json:"total"`
	Label   string `json:"label"`
	Status  string `json:"status"` // "running", "completed", "failed"
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// ProgressFunc is a callback for reporting progress. CLI callers print to
// stdout; dashboard callers forward events to an SSE channel.
type ProgressFunc func(ProgressEvent)

// ServerState represents the lifecycle state of a manager.
type ServerState string

const (
	StateStopped  ServerState = "stopped"
	StateStarting ServerState = "starting"
	StateRunning  ServerState = "running"
	StateStopping ServerState = "stopping"
	StateError    ServerState = "error"
)

// Ops centralises business logic shared by the CLI and the web dashboard.
type Ops struct {
	cfg *config.Config
	mu  sync.Mutex // serialises relay + user operations
	srv serverManager
	cli clientManager

	onlineMu      sync.RWMutex
	onlineCache   map[string]bool
	onlinePoll    time.Time
	onlineRefresh sync.Mutex // prevents concurrent refreshes
	trafficReset  bool       // true after first traffic stats reset
}

// New loads the configuration and returns a ready Ops instance.
func New() (*Ops, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &Ops{
		cfg: cfg,
		srv: serverManager{state: StateStopped},
		cli: clientManager{state: StateStopped},
	}, nil
}

// Config returns the current configuration (read-only snapshot).
func (o *Ops) Config() *config.Config {
	o.mu.Lock()
	defer o.mu.Unlock()
	c := *o.cfg
	return &c
}

// ReloadConfig re-reads the config file from disk.
func (o *Ops) ReloadConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	o.mu.Lock()
	o.cfg = cfg
	o.mu.Unlock()
	return nil
}

// Mode returns the current operating mode ("server", "client", or "").
func (o *Ops) Mode() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cfg.Mode
}

// SetMode persists the operating mode to config.
func (o *Ops) SetMode(mode string) error {
	if mode != "server" && mode != "client" {
		return fmt.Errorf("invalid mode: %q (must be \"server\" or \"client\")", mode)
	}
	o.mu.Lock()
	o.cfg.Mode = mode
	cfg := o.cfg
	o.mu.Unlock()
	return config.Save(cfg)
}

// SetProxy validates and persists the proxy URL to config.
// An empty string clears the proxy. Takes effect on next server/client start.
func (o *Ops) SetProxy(proxyURL string) error {
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		if u.Scheme != "socks5" && u.Scheme != "http" {
			return fmt.Errorf("unsupported proxy scheme %q (use socks5:// or http://)", u.Scheme)
		}
		if u.Hostname() == "" {
			return fmt.Errorf("proxy URL must include a host")
		}
	}
	o.mu.Lock()
	o.cfg.Proxy = proxyURL
	cfg := o.cfg
	o.mu.Unlock()
	return config.Save(cfg)
}

// StartServer starts all server components.
func (o *Ops) StartServer(progress ProgressFunc) error {
	return o.srv.Start(o, progress)
}

// StopServer stops all server components.
func (o *Ops) StopServer(progress ProgressFunc) error {
	return o.srv.Stop(progress)
}

// ServerStatus returns the server lifecycle state.
func (o *Ops) ServerStatus() ServerStatus {
	return o.srv.Status()
}

// StartClient starts the client connection.
func (o *Ops) StartClient(progress ProgressFunc) error {
	return o.cli.Start(o, progress)
}

// StopClient stops the client connection.
func (o *Ops) StopClient(progress ProgressFunc) error {
	return o.cli.Stop(progress)
}

// ClientStatus returns the client lifecycle state.
func (o *Ops) ClientStatus() ClientStatus {
	return o.cli.Status()
}
