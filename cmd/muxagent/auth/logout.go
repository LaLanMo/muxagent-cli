package auth

import (
	"fmt"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		Long: `Removes the stored authentication credentials from this machine.

After logging out, you will need to re-authenticate using 'muxagent auth login'
before the daemon can connect to the relay server.`,
		RunE: runLogout,
	}

	return cmd
}

func runLogout(cmd *cobra.Command, args []string) error {
	if !auth.HasCredentials() {
		fmt.Println("Not currently authenticated.")
		return nil
	}

	if err := auth.ClearCredentials(); err != nil {
		return fmt.Errorf("failed to clear credentials: %w", err)
	}

	fmt.Println("Credentials cleared. You are now logged out.")
	return nil
}
