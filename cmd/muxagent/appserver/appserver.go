package appserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	internalappserver "github.com/LaLanMo/muxagent-cli/internal/appserver"
	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/claudeexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskexecutor/codexexec"
	"github.com/LaLanMo/muxagent-cli/internal/taskruntime"
	"github.com/LaLanMo/muxagent-cli/internal/taskstore"
	cliversion "github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/LaLanMo/muxagent-cli/internal/worktree"
	"github.com/spf13/cobra"
)

func NewCmd() *cobra.Command {
	var workDir string

	cmd := &cobra.Command{
		Use:   "app-server",
		Short: "Run the local task app-server over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedWorkDir, err := resolveAppServerWorkDir(workDir)
			if err != nil {
				return err
			}
			service, err := taskruntime.NewService(
				resolvedWorkDir,
				taskexecutor.NewRouter(codexexec.New(""), claudeexec.New("")),
			)
			if err != nil {
				return err
			}
			server, err := internalappserver.New(internalappserver.Options{
				Service:       service,
				WorkDir:       resolvedWorkDir,
				ServerVersion: cliversion.CLIString(),
				WorktreeAvailable: func(path string) bool {
					_, err := worktree.FindRepoRoot(path)
					return err == nil
				},
				LoadTaskLaunchPreferences: appconfig.LoadTaskLaunchPreferences,
			})
			if err != nil {
				_ = service.Close()
				return err
			}
			return server.Serve(context.Background(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&workDir, "workdir", "", "Workspace directory to serve")
	_ = cmd.MarkFlagRequired("workdir")

	return cmd
}

func resolveAppServerWorkDir(workDir string) (string, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return "", fmt.Errorf("workdir is required")
	}
	if !filepath.IsAbs(workDir) {
		return "", fmt.Errorf("workdir must be an absolute path")
	}
	info, err := os.Stat(workDir)
	if err != nil {
		return "", fmt.Errorf("workdir unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir is not a directory: %s", workDir)
	}
	return taskstore.NormalizeWorkDir(workDir), nil
}
