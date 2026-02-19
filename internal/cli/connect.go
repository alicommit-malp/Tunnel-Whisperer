package cli

import (
	"fmt"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/config"
	twssh "github.com/tunnelwhisperer/tw/internal/ssh"
	twxray "github.com/tunnelwhisperer/tw/internal/xray"
)

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to a relay as a client",
	RunE:  runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	fmt.Println("Connecting to relay...")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	fmt.Printf("Config: %s\n", config.FilePath())

	// Ensure keys exist.
	if err := ensureKeys(cfg); err != nil {
		return err
	}

	if !cfg.Xray.Enabled {
		return fmt.Errorf("xray must be enabled in config to connect")
	}

	// Auto-generate UUID if missing.
	if cfg.Xray.UUID == "" {
		cfg.Xray.UUID = uuid.New().String()
		if err := config.Save(cfg); err != nil {
			fmt.Printf("Warning: could not save generated UUID: %v\n", err)
		} else {
			fmt.Printf("Generated Xray UUID: %s\n", cfg.Xray.UUID)
		}
	}

	// Start Xray in client mode.
	xrayInstance, err := twxray.NewClient(cfg.Xray, cfg.Client)
	if err != nil {
		return fmt.Errorf("initializing Xray: %w", err)
	}
	if err := xrayInstance.StartClient(cfg.Client); err != nil {
		return fmt.Errorf("starting Xray: %w", err)
	}
	defer xrayInstance.Close()
	fmt.Printf("Xray tunnel active → %s:%d%s\n", cfg.Xray.RelayHost, cfg.Xray.RelayPort, cfg.Xray.Path)

	// Start SSH forward tunnel through Xray to the server.
	privPath := filepath.Join(config.Dir(), "id_ed25519")
	ft := &twssh.ForwardTunnel{
		RemoteAddr: fmt.Sprintf("127.0.0.1:%d", cfg.Client.XrayListenPort),
		User:       cfg.Client.SSHUser,
		KeyPath:    privPath,
		LocalPort:  cfg.Client.LocalPort,
		RemoteHost: cfg.Client.RemoteHost,
		RemotePort: cfg.Client.RemotePort,
	}

	fmt.Printf("Forwarding localhost:%d → server %s:%d\n",
		cfg.Client.LocalPort, cfg.Client.RemoteHost, cfg.Client.RemotePort)

	// Run blocks until stopped (Ctrl-C).
	return ft.Run()
}
