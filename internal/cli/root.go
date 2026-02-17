package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgDir   string
	logLevel string
)

var rootCmd = &cobra.Command{
	Use:   "tw",
	Short: "Tunnel Whisperer — surgical, resilient connectivity",
	Long: `Tunnel Whisperer creates resilient, application-layer bridges for specific
ports across separated private networks. It encapsulates traffic in standard
HTTPS/WebSocket to traverse strict firewalls and DPI.`,
}

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	defaultCfgDir := fmt.Sprintf("%s/.tw", home)

	rootCmd.PersistentFlags().StringVar(&cfgDir, "config-dir", defaultCfgDir, "configuration directory")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
}

func Execute() error {
	return rootCmd.Execute()
}
