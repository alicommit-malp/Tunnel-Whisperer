package cli

import (
	"fmt"

	"github.com/spf13/cobra"
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
	fmt.Println("Client connection is not yet implemented.")
	fmt.Println("This will connect to a relay via Xray and establish an SSH tunnel.")
	return nil
}
