package cli

import (
	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/logging"
)

var logLevel string

var rootCmd = &cobra.Command{
	Use:   "tw",
	Short: "Tunnel Whisperer — surgical, resilient connectivity",
	Long: `Tunnel Whisperer creates resilient, application-layer bridges for specific
ports across separated private networks. It encapsulates traffic in standard
HTTPS/WebSocket to traverse strict firewalls and DPI.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logging.Setup(logLevel)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
}

func Execute() error {
	return rootCmd.Execute()
}
