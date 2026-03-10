package worktree

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

// FindRepoRoot returns the git repo root for a given path.
func FindRepoRoot(cwd string) (string, error) {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", cwd)
	}
	return strings.TrimSpace(string(out)), nil
}

// Create creates a git worktree at ~/.muxagent/worktrees/<repoHash>/<wtID>/
// with branch muxagent/<wtID>, based on HEAD.
func Create(repoRoot string, wtID string) (string, error) {
	h := sha256.Sum256([]byte(repoRoot))
	repoHash := fmt.Sprintf("%x", h[:4]) // first 8 hex chars

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}

	wtPath := filepath.Join(home, ".muxagent", "worktrees", repoHash, wtID)
	if err := privdir.EnsureWithin(filepath.Dir(wtPath), filepath.Join(home, ".muxagent")); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	branchName := "muxagent/" + wtID
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "add", "-b", branchName, wtPath, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return wtPath, nil
}
