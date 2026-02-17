package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/api"
	"github.com/tunnelwhisperer/tw/internal/core"
	twssh "github.com/tunnelwhisperer/tw/internal/ssh"
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

	// Ensure config directory exists.
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Generate SSH key pair.
	privPath := filepath.Join(cfgDir, "id_ed25519")
	pubPath := filepath.Join(cfgDir, "id_ed25519.pub")

	if _, err := os.Stat(privPath); err == nil {
		fmt.Println("SSH key pair already exists, skipping generation.")
	} else {
		fmt.Println("Generating ed25519 SSH key pair...")
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
		fmt.Printf("Keys written to %s\n", cfgDir)
	}

	// Initialize core service.
	svc := core.New(cfgDir)
	if err := svc.Init(); err != nil {
		return fmt.Errorf("initializing core service: %w", err)
	}

	// Print service install instructions (actual install is platform-specific and deferred).
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

	// Start gRPC API server.
	fmt.Println("\nStarting gRPC API server on :50051...")
	apiServer := api.NewServer(svc, ":50051")
	return apiServer.Run()
}
