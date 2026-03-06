package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Resolve symlinks (macOS: /var → /private/var) to match git output.
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	return dir
}

func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}

func TestFindRepoRoot_FromRoot(t *testing.T) {
	repo := initRepoWithCommit(t)

	root, err := FindRepoRoot(repo)
	require.NoError(t, err)
	assert.Equal(t, repo, root)
}

func TestFindRepoRoot_FromSubdirectory(t *testing.T) {
	repo := initRepoWithCommit(t)
	sub := filepath.Join(repo, "src", "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	root, err := FindRepoRoot(sub)
	require.NoError(t, err)
	assert.Equal(t, repo, root)
}

func TestFindRepoRoot_NonGitDir(t *testing.T) {
	dir := t.TempDir()

	_, err := FindRepoRoot(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestCreate_FromRepoRoot(t *testing.T) {
	repo := initRepoWithCommit(t)

	// Override HOME so worktrees go to temp dir
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	wtPath, err := Create(repo, "abc12345")
	require.NoError(t, err)

	// Verify worktree directory exists
	info, err := os.Stat(wtPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify .git file exists (worktree has a .git file, not directory)
	gitPath := filepath.Join(wtPath, ".git")
	gitInfo, err := os.Stat(gitPath)
	require.NoError(t, err)
	assert.False(t, gitInfo.IsDir(), ".git should be a file in a worktree, not a directory")

	// Verify branch exists
	cmd := exec.Command("git", "-C", repo, "branch", "--list", "muxagent/abc12345")
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Contains(t, strings.TrimSpace(string(out)), "muxagent/abc12345")

	// Verify HEAD matches
	headOrig := gitHead(t, repo)
	headWT := gitHead(t, wtPath)
	assert.Equal(t, headOrig, headWT)
}

func TestCreate_EmptyRepo(t *testing.T) {
	repo := initRepo(t) // no commit

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	_, err := Create(repo, "abc12345")
	require.Error(t, err, "should fail on empty repo (no HEAD)")
}

func TestCreate_TwoWorktrees(t *testing.T) {
	repo := initRepoWithCommit(t)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	wtA, err := Create(repo, "wt-aaa")
	require.NoError(t, err)

	wtB, err := Create(repo, "wt-bbb")
	require.NoError(t, err)

	// Both directories exist
	_, err = os.Stat(wtA)
	require.NoError(t, err)
	_, err = os.Stat(wtB)
	require.NoError(t, err)

	// Both branches exist
	cmd := exec.Command("git", "-C", repo, "branch", "--list", "muxagent/*")
	out, err := cmd.Output()
	require.NoError(t, err)
	branches := string(out)
	assert.Contains(t, branches, "muxagent/wt-aaa")
	assert.Contains(t, branches, "muxagent/wt-bbb")
}

func TestCreate_DuplicateID(t *testing.T) {
	repo := initRepoWithCommit(t)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	_, err := Create(repo, "abc12345")
	require.NoError(t, err)

	_, err = Create(repo, "abc12345")
	require.Error(t, err, "second call with same ID should fail (branch already exists)")
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
