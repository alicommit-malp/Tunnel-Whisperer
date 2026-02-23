package ops

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/tunnelwhisperer/tw/internal/api"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/core"
	twssh "github.com/tunnelwhisperer/tw/internal/ssh"
	twxray "github.com/tunnelwhisperer/tw/internal/xray"
)

// ServerStatus describes the server lifecycle state.
type ServerStatus struct {
	State  ServerState `json:"state"`
	SSH    bool        `json:"ssh"`
	Xray   bool        `json:"xray"`
	Tunnel bool        `json:"tunnel"`
	API    bool        `json:"api"`
	Error  string      `json:"error,omitempty"`
}

// serverManager controls the lifecycle of all server components.
type serverManager struct {
	mu       sync.Mutex
	state    ServerState
	lastErr  string
	sshSrv   *twssh.Server
	xrayInst *twxray.Instance
	tunnel   *twssh.ReverseTunnel
	apiSrv   *api.Server
}

// Start launches all server components (SSH, Xray, reverse tunnel, gRPC API).
func (m *serverManager) Start(o *Ops, progress ProgressFunc) error {
	m.mu.Lock()
	if m.state == StateRunning || m.state == StateStarting {
		m.mu.Unlock()
		return fmt.Errorf("server already %s", m.state)
	}
	m.state = StateStarting
	m.lastErr = ""
	m.mu.Unlock()

	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	cfg := o.Config()

	fail := func(step, total int, label string, err error) error {
		m.mu.Lock()
		m.state = StateError
		m.lastErr = err.Error()
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: label, Status: "failed", Error: err.Error()})
		return err
	}

	total := 4
	if cfg.Xray.RelayHost != "" {
		total = 6
	}

	// Step 1: Ensure keys.
	progress(ProgressEvent{Step: 1, Total: total, Label: "SSH keys", Status: "running"})
	if err := o.EnsureKeys(); err != nil {
		return fail(1, total, "SSH keys", err)
	}
	progress(ProgressEvent{Step: 1, Total: total, Label: "SSH keys", Status: "completed"})

	// Step 2: Initialize core.
	progress(ProgressEvent{Step: 2, Total: total, Label: "Core service", Status: "running"})
	svc := core.New(config.Dir())
	if err := svc.Init(); err != nil {
		return fail(2, total, "Core service", err)
	}
	progress(ProgressEvent{Step: 2, Total: total, Label: "Core service", Status: "completed"})

	// Step 3: Start SSH server.
	progress(ProgressEvent{Step: 3, Total: total, Label: "SSH server", Status: "running"})
	sshServer, err := twssh.NewServer(cfg.Server.SSHPort, config.HostKeyDir(), config.AuthorizedKeysPath())
	if err != nil {
		return fail(3, total, "SSH server", err)
	}
	go func() {
		if err := sshServer.Run(); err != nil {
			slog.Error("SSH server error", "error", err)
		}
	}()
	m.mu.Lock()
	m.sshSrv = sshServer
	m.mu.Unlock()
	progress(ProgressEvent{Step: 3, Total: total, Label: "SSH server", Status: "completed", Message: fmt.Sprintf("listening on :%d", cfg.Server.SSHPort)})

	step := 4

	// Steps 4-5: Xray + reverse tunnel (if relay configured).
	if cfg.Xray.RelayHost != "" {
		if cfg.Xray.UUID == "" {
			cfg.Xray.UUID = uuid.New().String()
			if err := config.Save(cfg); err != nil {
				slog.Warn("could not save generated UUID", "error", err)
			}
		}

		progress(ProgressEvent{Step: step, Total: total, Label: "Xray tunnel", Status: "running"})
		xrayInstance, err := twxray.New(cfg.Xray)
		if err != nil {
			return fail(step, total, "Xray tunnel", err)
		}
		if err := xrayInstance.Start(cfg.Server.SSHPort, cfg.Server.RelaySSHPort); err != nil {
			return fail(step, total, "Xray tunnel", err)
		}
		m.mu.Lock()
		m.xrayInst = xrayInstance
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "Xray tunnel", Status: "completed", Message: fmt.Sprintf("%s:%d%s", cfg.Xray.RelayHost, cfg.Xray.RelayPort, cfg.Xray.Path)})

		step++
		xrayListenPort := cfg.Server.SSHPort + 1
		progress(ProgressEvent{Step: step, Total: total, Label: "Reverse tunnel", Status: "running"})
		privPath := filepath.Join(config.Dir(), "id_ed25519")
		rt := &twssh.ReverseTunnel{
			RemoteAddr: fmt.Sprintf("127.0.0.1:%d", xrayListenPort),
			User:       cfg.Server.RelaySSHUser,
			KeyPath:    privPath,
			RemotePort: cfg.Server.RemotePort,
			LocalAddr:  fmt.Sprintf("127.0.0.1:%d", cfg.Server.SSHPort),
		}
		go func() {
			if err := rt.Run(); err != nil {
				slog.Error("reverse tunnel error", "error", err)
			}
		}()
		m.mu.Lock()
		m.tunnel = rt
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "Reverse tunnel", Status: "completed", Message: fmt.Sprintf("relay :%d → local :%d", cfg.Server.RemotePort, cfg.Server.SSHPort)})

		step++
	}

	// Final step: gRPC API.
	progress(ProgressEvent{Step: step, Total: total, Label: "gRPC API", Status: "running"})
	apiAddr := fmt.Sprintf(":%d", cfg.Server.APIPort)
	apiServer := api.NewServer(svc, apiAddr)
	go func() {
		if err := apiServer.Run(); err != nil {
			slog.Error("gRPC API error", "error", err)
		}
	}()
	m.mu.Lock()
	m.apiSrv = apiServer
	m.mu.Unlock()
	progress(ProgressEvent{Step: step, Total: total, Label: "gRPC API", Status: "completed", Message: apiAddr})

	m.mu.Lock()
	m.state = StateRunning
	m.mu.Unlock()

	return nil
}

// Stop shuts down all server components in reverse order.
func (m *serverManager) Stop(progress ProgressFunc) error {
	m.mu.Lock()
	if m.state != StateRunning && m.state != StateError {
		m.mu.Unlock()
		return fmt.Errorf("server not running (state: %s)", m.state)
	}
	m.state = StateStopping
	m.mu.Unlock()

	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	step := 1
	total := 0
	m.mu.Lock()
	if m.apiSrv != nil {
		total++
	}
	if m.tunnel != nil {
		total++
	}
	if m.xrayInst != nil {
		total++
	}
	if m.sshSrv != nil {
		total++
	}
	m.mu.Unlock()

	if total == 0 {
		total = 1
	}

	m.mu.Lock()
	if m.apiSrv != nil {
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "gRPC API", Status: "running"})
		m.apiSrv.Stop()
		m.mu.Lock()
		m.apiSrv = nil
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "gRPC API", Status: "completed"})
		step++
		m.mu.Lock()
	}

	if m.tunnel != nil {
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "Reverse tunnel", Status: "running"})
		m.tunnel.Stop()
		m.mu.Lock()
		m.tunnel = nil
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "Reverse tunnel", Status: "completed"})
		step++
		m.mu.Lock()
	}

	if m.xrayInst != nil {
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "Xray tunnel", Status: "running"})
		m.xrayInst.Close()
		m.mu.Lock()
		m.xrayInst = nil
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "Xray tunnel", Status: "completed"})
		step++
		m.mu.Lock()
	}

	if m.sshSrv != nil {
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "SSH server", Status: "running"})
		m.sshSrv.Stop()
		m.mu.Lock()
		m.sshSrv = nil
		m.mu.Unlock()
		progress(ProgressEvent{Step: step, Total: total, Label: "SSH server", Status: "completed"})
		m.mu.Lock()
	}
	m.mu.Unlock()

	m.mu.Lock()
	m.state = StateStopped
	m.lastErr = ""
	m.mu.Unlock()

	return nil
}

// Status returns the current server state.
func (m *serverManager) Status() ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ServerStatus{
		State:  m.state,
		SSH:    m.sshSrv != nil,
		Xray:   m.xrayInst != nil,
		Tunnel: m.tunnel != nil,
		API:    m.apiSrv != nil,
		Error:  m.lastErr,
	}
}
