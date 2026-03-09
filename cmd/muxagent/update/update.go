package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/LaLanMo/muxagent-cli/internal/acpbin"
	"github.com/LaLanMo/muxagent-cli/internal/config"
	"github.com/LaLanMo/muxagent-cli/internal/version"
	"github.com/spf13/cobra"
)

const cliRepo = "LaLanMo/muxagent-cli"

func NewCmd() *cobra.Command {
	var checkOnly bool
	var ensureRuntimeOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update muxagent to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if ensureRuntimeOnly {
				return ensureRuntime()
			}
			return run(checkOnly)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates, don't install")
	cmd.Flags().BoolVar(&ensureRuntimeOnly, "ensure-runtime", false, "Only ensure the agent runtime binary is downloaded")
	cmd.Flags().MarkHidden("ensure-runtime")

	return cmd
}

func run(checkOnly bool) error {
	current := version.Version
	if current == "dev" {
		fmt.Println("Running development build. Skipping update.")
		return nil
	}

	latest, err := latestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestClean := strings.TrimPrefix(latest, "v")
	currentClean := strings.TrimPrefix(current, "v")

	if checkOnly {
		if latestClean == currentClean {
			fmt.Printf("muxagent is up to date (v%s)\n", currentClean)
		} else {
			fmt.Printf("Update available: v%s → v%s\nRun \"muxagent update\" to install.\n", currentClean, latestClean)
		}
		return nil
	}

	if latestClean == currentClean {
		fmt.Printf("muxagent is up to date (v%s)\n", currentClean)
		// Still ensure runtime is resolved
		return ensureRuntime()
	}

	fmt.Printf("Updating muxagent... (v%s → v%s)\n", currentClean, latestClean)

	if err := downloadCLI(latest); err != nil {
		return err
	}

	// Re-exec the new binary to resolve the runtime with its embedded ACPVersion.
	// This ensures CLI + runtime are both updated in one shot.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("updated CLI but failed to resolve executable: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("updated CLI but failed to resolve executable: %w", err)
	}
	return syscall.Exec(exe, []string{exe, "update", "--ensure-runtime"}, os.Environ())
}

func ensureRuntime() error {
	cfg, err := config.LoadEffective()
	if err != nil {
		return err
	}

	if cfg.ActiveRuntime != config.RuntimeClaudeCode {
		fmt.Printf("Updated muxagent to v%s\n", strings.TrimPrefix(version.Version, "v"))
		return nil
	}

	_, err = acpbin.Resolve(cfg, func(ev acpbin.ProgressEvent) {
		if ev.Phase == "downloading" && ev.TotalBytes > 0 {
			pct := float64(ev.BytesRead) / float64(ev.TotalBytes) * 100
			fmt.Printf("\rDownloading agent runtime... %.0f%%", pct)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to set up agent runtime: %w", err)
	}
	fmt.Println()
	fmt.Printf("Updated muxagent to v%s\n", strings.TrimPrefix(version.Version, "v"))
	return nil
}

func latestRelease() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", cliRepo)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

func downloadCLI(tag string) error {
	arch := runtime.GOARCH
	goos := runtime.GOOS

	assetName := fmt.Sprintf("muxagent-%s-%s", goos, arch)
	if goos == "windows" {
		assetName += ".exe"
	}

	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", cliRepo, tag, assetName)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download update (HTTP %d)", resp.StatusCode)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	tmpPath := exe + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("cannot write update: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to download update: %w", err)
	}
	f.Close()

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, exe); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}
