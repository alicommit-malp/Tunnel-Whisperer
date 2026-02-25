package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/api"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/dashboard"
	"github.com/tunnelwhisperer/tw/internal/ops"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard",
	RunE:  runDashboard,
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 0, "dashboard listen port (overrides config)")
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	o, err := ops.New()
	if err != nil {
		return fmt.Errorf("initializing ops: %w", err)
	}

	// Start gRPC API so CLI commands can talk to this daemon.
	apiAddr := fmt.Sprintf(":%d", cfg.Server.APIPort)
	apiSrv := api.NewServer(o, apiAddr)
	go func() {
		slog.Info("gRPC API listening", "addr", apiAddr)
		if err := apiSrv.Run(); err != nil {
			slog.Error("gRPC API error", "error", err)
		}
	}()

	port := cfg.Server.DashboardPort
	if dashboardPort != 0 {
		port = dashboardPort
	}

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Starting dashboard on http://localhost%s\n", addr)
	srv := dashboard.NewServer(addr, o)
	return srv.Run()
}
