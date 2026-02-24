package xray

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/logging"
	"github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/distro/all"
)

// ClientListenPort is the fixed local port for the client-side Xray dokodemo-door.
const ClientListenPort = 54001

// Instance wraps a running xray-core instance.
type Instance struct {
	instance *core.Instance
	cfg      config.XrayConfig
}

// xrayConfig mirrors the Xray JSON configuration structure.
type xrayConfig struct {
	Log       xrayLog       `json:"log"`
	Inbounds  []interface{} `json:"inbounds"`
	Outbounds []interface{} `json:"outbounds"`
	Routing   *xrayRouting  `json:"routing,omitempty"`
}

type xrayRouting struct {
	Rules []map[string]interface{} `json:"rules"`
}

type xrayLog struct {
	LogLevel string `json:"loglevel"`
}

// vlessOutbound returns the VLESS outbound config block (shared by server and client).
func vlessOutbound(cfg config.XrayConfig) map[string]interface{} {
	return map[string]interface{}{
		"tag":      "to-relay",
		"protocol": "vless",
		"settings": map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": cfg.RelayHost,
					"port":    cfg.RelayPort,
					"users": []map[string]interface{}{
						{
							"id":         cfg.UUID,
							"encryption": "none",
						},
					},
				},
			},
		},
		"streamSettings": map[string]interface{}{
			"network":  "splithttp",
			"security": "tls",
			"tlsSettings": map[string]interface{}{
				"serverName": cfg.RelayHost,
			},
			"splithttpSettings": map[string]interface{}{
				"path": cfg.Path,
			},
		},
	}
}

// buildServerConfig generates the server-side Xray JSON config.
// dokodemo-door listens on sshPort+1 and forwards to the relay's SSH port.
func buildServerConfig(cfg config.XrayConfig, sshPort, relaySSHPort int) ([]byte, error) {
	listenPort := sshPort + 1

	xc := xrayConfig{
		Log: xrayLog{LogLevel: logging.XrayLevel},
		Inbounds: []interface{}{
			map[string]interface{}{
				"tag":      "ssh-in",
				"listen":   "127.0.0.1",
				"port":     listenPort,
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{
					"network": "tcp",
					"address": "127.0.0.1",
					"port":    relaySSHPort,
				},
			},
		},
		Outbounds: []interface{}{vlessOutbound(cfg)},
	}

	return json.MarshalIndent(xc, "", "  ")
}

// buildClientConfig generates the client-side Xray JSON config.
// dokodemo-door listens on ClientListenPort and forwards to the server's SSH
// port on the relay (exposed via reverse tunnel).
func buildClientConfig(cfg config.XrayConfig, clientCfg config.ClientConfig) ([]byte, error) {
	xc := xrayConfig{
		Log: xrayLog{LogLevel: logging.XrayLevel},
		Inbounds: []interface{}{
			map[string]interface{}{
				"tag":      "ssh-local",
				"listen":   "127.0.0.1",
				"port":     ClientListenPort,
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{
					"network": "tcp",
					"address": "127.0.0.1",
					"port":    clientCfg.ServerSSHPort,
				},
			},
		},
		Outbounds: []interface{}{vlessOutbound(cfg)},
		Routing: &xrayRouting{
			Rules: []map[string]interface{}{
				{
					"type":        "field",
					"inboundTag":  []string{"ssh-local"},
					"outboundTag": "to-relay",
				},
			},
		},
	}

	return json.MarshalIndent(xc, "", "  ")
}

// New creates a new Xray instance for server mode.
func New(cfg config.XrayConfig) (*Instance, error) {
	if cfg.UUID == "" {
		return nil, fmt.Errorf("xray: UUID is required")
	}
	if cfg.RelayHost == "" {
		return nil, fmt.Errorf("xray: relay_host is required")
	}

	return &Instance{cfg: cfg}, nil
}

// Start builds the server JSON config and starts the xray-core instance.
func (x *Instance) Start(sshPort, relaySSHPort int) error {
	configBytes, err := buildServerConfig(x.cfg, sshPort, relaySSHPort)
	if err != nil {
		return fmt.Errorf("xray: building config: %w", err)
	}

	slog.Info("Xray starting", "relay", fmt.Sprintf("%s:%d", x.cfg.RelayHost, x.cfg.RelayPort), "path", x.cfg.Path, "xray_log_level", logging.XrayLevel)

	instance, err := core.StartInstance("json", configBytes)
	if err != nil {
		return fmt.Errorf("xray: starting instance: %w", err)
	}

	x.instance = instance
	slog.Info("Xray instance started")
	return nil
}

// NewClient creates a new Xray instance for client mode.
func NewClient(cfg config.XrayConfig) (*Instance, error) {
	if cfg.UUID == "" {
		return nil, fmt.Errorf("xray: UUID is required")
	}
	if cfg.RelayHost == "" {
		return nil, fmt.Errorf("xray: relay_host is required")
	}

	return &Instance{cfg: cfg}, nil
}

// StartClient builds the client JSON config and starts the xray-core instance.
func (x *Instance) StartClient(clientCfg config.ClientConfig) error {
	configBytes, err := buildClientConfig(x.cfg, clientCfg)
	if err != nil {
		return fmt.Errorf("xray: building client config: %w", err)
	}

	slog.Info("Xray client starting", "relay", fmt.Sprintf("%s:%d", x.cfg.RelayHost, x.cfg.RelayPort), "path", x.cfg.Path, "xray_log_level", logging.XrayLevel)

	instance, err := core.StartInstance("json", configBytes)
	if err != nil {
		return fmt.Errorf("xray: starting client instance: %w", err)
	}

	x.instance = instance
	slog.Info("Xray client instance started")
	return nil
}

// Running reports whether the xray-core instance is started.
func (x *Instance) Running() bool {
	return x != nil && x.instance != nil
}

// Close shuts down the xray-core instance.
func (x *Instance) Close() error {
	if x.instance != nil {
		err := x.instance.Close()
		x.instance = nil
		return err
	}
	return nil
}
