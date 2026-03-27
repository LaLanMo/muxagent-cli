package worktree

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

func canonicalExistingPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

// FindRepoRoot returns the git repo root for a given path.
func FindRepoRoot(cwd string) (string, error) {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", cwd)
	}
	return strings.TrimSpace(string(out)), nil
}

func BranchName(wtID string) string {
	return "muxagent/" + wtID
}

func NormalizeRepoRelativePath(repoRoot, cwd string) (string, error) {
	canonicalRepoRoot, err := canonicalExistingPath(repoRoot)
	if err != nil {
		return "", fmt.Errorf("canonicalize repo root: %w", err)
	}
	canonicalCWD, err := canonicalExistingPath(cwd)
	if err != nil {
		return "", fmt.Errorf("canonicalize cwd: %w", err)
	}
	relPath, err := filepath.Rel(canonicalRepoRoot, canonicalCWD)
	if err != nil {
		return "", err
	}
	relPath = filepath.Clean(relPath)
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("cwd %q escapes repo root %q", canonicalCWD, canonicalRepoRoot)
	}
	return relPath, nil
}

func WorktreeCWDPath(worktreePath, relativeCWD string) (string, error) {
	relPath, err := cleanRelativeCWD(relativeCWD)
	if err != nil {
		return "", err
	}
	if relPath == "." {
		return filepath.Clean(worktreePath), nil
	}
	return filepath.Join(filepath.Clean(worktreePath), relPath), nil
}

func ResolveWorktreeCWD(worktreePath, relativeCWD string) (string, error) {
	canonicalRoot, err := canonicalExistingPath(worktreePath)
	if err != nil {
		return "", fmt.Errorf("saved worktree path unavailable: %w", err)
	}

	relPath, err := cleanRelativeCWD(relativeCWD)
	if err != nil {
		return "", err
	}
	switch relPath {
	case ".":
		info, err := os.Stat(canonicalRoot)
		if err != nil {
			return "", fmt.Errorf("saved worktree cwd unavailable: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("saved worktree cwd is not a directory: %s", canonicalRoot)
		}
		return canonicalRoot, nil
	}
	target := filepath.Join(canonicalRoot, relPath)
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("saved worktree cwd unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("saved worktree cwd is not a directory: %s", target)
	}
	return target, nil
}

func ResolveMappedCWD(mapping *Mapping) (string, error) {
	if mapping == nil {
		return "", errors.New("saved worktree mapping unavailable")
	}
	return ResolveWorktreeCWD(mapping.WorktreePath, mapping.RelativeCWD)
}

func Cleanup(repoRoot, worktreePath, branchName string) error {
	var cleanupErr error

	if strings.TrimSpace(worktreePath) != "" {
		cmd := exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", worktreePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err))
		}
	}
	if strings.TrimSpace(branchName) != "" {
		cmd := exec.Command("git", "-C", repoRoot, "branch", "-D", branchName)
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("git branch -D: %s: %w", strings.TrimSpace(string(out)), err))
		}
	}
	return cleanupErr
}

func cleanRelativeCWD(relativeCWD string) (string, error) {
	relPath := filepath.Clean(strings.TrimSpace(relativeCWD))
	switch relPath {
	case "", ".":
		return ".", nil
	}
	if filepath.IsAbs(relPath) || relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("saved worktree cwd is invalid: %s", relativeCWD)
	}
	return relPath, nil
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

	branchName := BranchName(wtID)
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "add", "-b", branchName, wtPath, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return wtPath, nil
}
