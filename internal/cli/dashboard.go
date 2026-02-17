package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/dashboard"
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

	port := cfg.Dashboard.Port
	if dashboardPort != 0 {
		port = dashboardPort
	}

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Starting dashboard on http://localhost%s\n", addr)
	srv := dashboard.NewServer(addr)
	return srv.Run()
}
