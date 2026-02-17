package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/api"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/core"
	twssh "github.com/tunnelwhisperer/tw/internal/ssh"
	twxray "github.com/tunnelwhisperer/tw/internal/xray"
)

var asServer bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Tunnel Whisperer",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&asServer, "as-server", false, "install and start as a system service")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	if !asServer {
		return fmt.Errorf("please specify --as-server to initialize in server mode")
	}

	fmt.Println("Initializing Tunnel Whisperer server...")

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	fmt.Printf("Config: %s\n", config.FilePath())

	// Ensure config directory exists.
	if err := os.MkdirAll(config.Dir(), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Generate SSH client key pair (used to connect to relay).
	privPath := filepath.Join(config.Dir(), "id_ed25519")
	pubPath := filepath.Join(config.Dir(), "id_ed25519.pub")

	if _, err := os.Stat(privPath); err == nil {
		fmt.Println("SSH client key pair already exists, skipping generation.")
	} else {
		fmt.Println("Generating ed25519 SSH client key pair...")
		privPEM, pubAuthorized, err := twssh.GenerateKeyPair()
		if err != nil {
			return fmt.Errorf("generating SSH key pair: %w", err)
		}
		if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
			return fmt.Errorf("writing private key: %w", err)
		}
		if err := os.WriteFile(pubPath, pubAuthorized, 0644); err != nil {
			return fmt.Errorf("writing public key: %w", err)
		}
		fmt.Printf("Client keys written to %s\n", config.Dir())

		// Seed authorized_keys with the generated public key so the
		// server's own client key is trusted by the embedded SSH server.
		akPath := cfg.SSH.AuthorizedKeys
		if _, err := os.Stat(akPath); os.IsNotExist(err) {
			if err := os.WriteFile(akPath, pubAuthorized, 0600); err != nil {
				return fmt.Errorf("writing authorized_keys: %w", err)
			}
			fmt.Printf("authorized_keys seeded at %s\n", akPath)
		}
	}

	// Save default config if none exists.
	if _, err := os.Stat(config.FilePath()); os.IsNotExist(err) {
		if err := config.Save(cfg); err != nil {
			fmt.Printf("Warning: could not save default config: %v\n", err)
		} else {
			fmt.Printf("Default config written to %s\n", config.FilePath())
		}
	}

	// Initialize core service.
	svc := core.New(config.Dir())
	if err := svc.Init(); err != nil {
		return fmt.Errorf("initializing core service: %w", err)
	}

	// Print service install instructions.
	switch runtime.GOOS {
	case "linux":
		fmt.Println("\nTo install as a systemd service:")
		fmt.Println("  sudo cp tw /usr/local/bin/tw")
		fmt.Println("  sudo systemctl enable --now tw.service")
	case "windows":
		fmt.Println("\nTo install as a Windows service:")
		fmt.Println("  sc.exe create TunnelWhisperer binPath= \"C:\\tw\\tw.exe run\"")
		fmt.Println("  sc.exe config TunnelWhisperer start= auto")
		fmt.Println("  sc.exe start TunnelWhisperer")
	default:
		fmt.Printf("\nService installation for %s is not yet supported.\n", runtime.GOOS)
	}

	// Start SSH server.
	sshServer, err := twssh.NewServer(cfg.SSH.Port, cfg.SSH.HostKeyDir, cfg.SSH.AuthorizedKeys)
	if err != nil {
		return fmt.Errorf("initializing SSH server: %w", err)
	}
	go func() {
		fmt.Printf("\nStarting SSH server on :%d...\n", cfg.SSH.Port)
		if err := sshServer.Run(); err != nil {
			fmt.Printf("SSH server error: %v\n", err)
		}
	}()

	// Start Xray tunnel if enabled.
	if cfg.Xray.Enabled {
		if cfg.Xray.UUID == "" {
			cfg.Xray.UUID = uuid.New().String()
			if err := config.Save(cfg); err != nil {
				fmt.Printf("Warning: could not save generated UUID: %v\n", err)
			} else {
				fmt.Printf("Generated Xray UUID: %s\n", cfg.Xray.UUID)
			}
		}

		xrayInstance, err := twxray.New(cfg.Xray, cfg.SSH.Port)
		if err != nil {
			return fmt.Errorf("initializing Xray: %w", err)
		}
		if err := xrayInstance.Start(cfg.SSH.Port); err != nil {
			return fmt.Errorf("starting Xray: %w", err)
		}
		defer xrayInstance.Close()
		fmt.Printf("Xray tunnel active → %s:%d%s\n", cfg.Xray.RelayHost, cfg.Xray.RelayPort, cfg.Xray.Path)

		// Determine Xray local listen port for the SSH reverse tunnel.
		xrayListenPort := cfg.Xray.ListenPort
		if xrayListenPort == 0 {
			xrayListenPort = cfg.SSH.Port + 1
		}

		// Start SSH reverse tunnel through Xray to the relay.
		privPath := filepath.Join(config.Dir(), "id_ed25519")
		rt := &twssh.ReverseTunnel{
			RemoteAddr: fmt.Sprintf("127.0.0.1:%d", xrayListenPort),
			User:       cfg.Xray.RelaySSHUser,
			KeyPath:    privPath,
			RemotePort: cfg.Xray.RemotePort,
			LocalAddr:  fmt.Sprintf("127.0.0.1:%d", cfg.SSH.Port),
		}
		go func() {
			fmt.Printf("Reverse tunnel: relay :%d → local :%d (via Xray :%d)\n",
				cfg.Xray.RemotePort, cfg.SSH.Port, xrayListenPort)
			if err := rt.Run(); err != nil {
				fmt.Printf("Reverse tunnel error: %v\n", err)
			}
		}()
		defer rt.Stop()
	}

	// Start gRPC API server (blocking).
	apiAddr := fmt.Sprintf(":%d", cfg.API.Port)
	fmt.Printf("Starting gRPC API server on %s...\n", apiAddr)
	apiServer := api.NewServer(svc, apiAddr)
	return apiServer.Run()
}
