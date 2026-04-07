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
		Short: "Run the local task app-server",
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := newServer(stateDir)
			if err != nil {
				return err
			}
			return server.Serve(context.Background(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	cmd.PersistentFlags().StringVar(&stateDir, "state-dir", "", "State directory for the global app-server")
	_ = cmd.PersistentFlags().MarkHidden("state-dir")

	cmd.AddCommand(newEnsureCmd(&stateDir))
	cmd.AddCommand(newServeDaemonCmd(&stateDir))

	return cmd
}

func newServer(stateDir string) (*internalappserver.Server, error) {
	return internalappserver.New(internalappserver.Options{
		StateDir:      stateDir,
		ServerVersion: cliversion.CLIString(),
		WorktreeAvailable: func(path string) bool {
			_, err := worktree.FindRepoRoot(path)
			return err == nil
		},
	})
}
