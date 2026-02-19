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

	if cfg.Xray.RelayHost == "" {
		return fmt.Errorf("xray.relay_host must be set in config to connect")
	}

	if len(cfg.Client.Tunnels) == 0 {
		return fmt.Errorf("no tunnels defined in client.tunnels config")
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
	xrayInstance, err := twxray.NewClient(cfg.Xray)
	if err != nil {
		return fmt.Errorf("initializing Xray: %w", err)
	}
	if err := xrayInstance.StartClient(cfg.Client); err != nil {
		return fmt.Errorf("starting Xray: %w", err)
	}
	defer xrayInstance.Close()
	fmt.Printf("Xray tunnel active → %s:%d%s\n", cfg.Xray.RelayHost, cfg.Xray.RelayPort, cfg.Xray.Path)

	// Build mappings from config.
	mappings := make([]twssh.Mapping, len(cfg.Client.Tunnels))
	for i, t := range cfg.Client.Tunnels {
		mappings[i] = twssh.Mapping{
			LocalPort:  t.LocalPort,
			RemoteHost: t.RemoteHost,
			RemotePort: t.RemotePort,
		}
		fmt.Printf("Forwarding localhost:%d → server %s:%d\n",
			t.LocalPort, t.RemoteHost, t.RemotePort)
	}

	// Single SSH session handles all port mappings.
	privPath := filepath.Join(config.Dir(), "id_ed25519")
	ft := &twssh.ForwardTunnel{
		RemoteAddr: fmt.Sprintf("127.0.0.1:%d", twxray.ClientListenPort),
		User:       cfg.Client.SSHUser,
		KeyPath:    privPath,
		Mappings:   mappings,
	}

	// Run blocks until stopped (Ctrl-C).
	return ft.Run()
}
