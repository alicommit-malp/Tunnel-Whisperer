package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/dashboard"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard",
	RunE:  runDashboard,
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8080, "dashboard listen port")
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	addr := fmt.Sprintf(":%d", dashboardPort)
	fmt.Printf("Starting dashboard on http://localhost%s\n", addr)
	srv := dashboard.NewServer(addr)
	return srv.Run()
}
