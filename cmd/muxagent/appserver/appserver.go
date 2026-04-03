package appserver

import (
	"context"

	internalappserver "github.com/LaLanMo/muxagent-cli/internal/appserver"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/spf13/cobra"
)

func NewCmd() *cobra.Command {
	var stateDir string

	cmd := &cobra.Command{
		Use:   "app-server",
		Short: "Run the local task app-server over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := internalappserver.New(internalappserver.Options{
				StateDir:      stateDir,
				ServerVersion: cliversion.CLIString(),
				WorktreeAvailable: func(path string) bool {
					_, err := worktree.FindRepoRoot(path)
					return err == nil
				},
			})
			if err != nil {
				return err
			}
			return server.Serve(context.Background(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&stateDir, "state-dir", "", "State directory for the global app-server")
	_ = cmd.Flags().MarkHidden("state-dir")

	return cmd
}
