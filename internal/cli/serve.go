package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/dashboard"
	"github.com/tunnelwhisperer/tw/internal/ops"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Tunnel Whisperer server",
	RunE:  runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	fmt.Println("Starting Tunnel Whisperer server...")

	o, err := ops.New()
	if err != nil {
		return fmt.Errorf("initializing ops: %w", err)
	}

	cfg := o.Config()
	fmt.Printf("Config: %s\n", config.FilePath())

	// Start dashboard if configured (before server so user can see progress).
	if cfg.Server.DashboardPort > 0 {
		dashAddr := fmt.Sprintf(":%d", cfg.Server.DashboardPort)
		dashSrv := dashboard.NewServer(dashAddr, o)
		go func() {
			fmt.Printf("Dashboard on http://localhost%s\n", dashAddr)
			if err := dashSrv.Run(); err != nil {
				fmt.Printf("Dashboard error: %v\n", err)
			}
		}()
	}

	// Start all server components via the manager.
	if err := o.StartServer(cliProgress); err != nil {
		return err
	}

	fmt.Println("Server running. Press Ctrl-C to stop.")

	// Block until signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Println("\nShutting down...")
	o.StopServer(nil)
	return nil
}
