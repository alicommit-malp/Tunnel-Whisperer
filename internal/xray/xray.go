package xray

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/distro/all"
)

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
}

type xrayLog struct {
	LogLevel string `json:"loglevel"`
}

// buildConfig generates the Xray JSON config matching
// docs/architecture/ssh-over-xray/server.json structure.
func buildConfig(cfg config.XrayConfig, sshPort int) ([]byte, error) {
	listenPort := cfg.ListenPort
	if listenPort == 0 {
		listenPort = sshPort + 1
	}

	xc := xrayConfig{
		Log: xrayLog{LogLevel: "warning"},
		Inbounds: []interface{}{
			map[string]interface{}{
				"tag":      "ssh-in",
				"listen":   "127.0.0.1",
				"port":     listenPort,
				"protocol": "dokodemo-door",
				"settings": map[string]interface{}{
					"network": "tcp",
					"address": "127.0.0.1",
					"port":    cfg.RelaySSHPort,
				},
			},
		},
		Outbounds: []interface{}{
			map[string]interface{}{
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
			},
		},
	}

	return json.MarshalIndent(xc, "", "  ")
}

// New creates a new Xray instance from the given config.
// It does not start the instance — call Start() for that.
func New(cfg config.XrayConfig, sshPort int) (*Instance, error) {
	if cfg.UUID == "" {
		return nil, fmt.Errorf("xray: UUID is required")
	}
	if cfg.RelayHost == "" {
		return nil, fmt.Errorf("xray: relay_host is required")
	}

	return &Instance{cfg: cfg}, nil
}

// Start builds the JSON config and starts the xray-core instance.
func (x *Instance) Start(sshPort int) error {
	configBytes, err := buildConfig(x.cfg, sshPort)
	if err != nil {
		return fmt.Errorf("xray: building config: %w", err)
	}

	log.Printf("xray: starting instance (relay=%s:%d, path=%s)", x.cfg.RelayHost, x.cfg.RelayPort, x.cfg.Path)

	instance, err := core.StartInstance("json", configBytes)
	if err != nil {
		return fmt.Errorf("xray: starting instance: %w", err)
	}

	x.instance = instance
	log.Println("xray: instance started successfully")
	return nil
}

// Close shuts down the xray-core instance.
func (x *Instance) Close() error {
	if x.instance != nil {
		return x.instance.Close()
	}
	return nil
}
