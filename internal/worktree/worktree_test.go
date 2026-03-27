package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestNormalizeRepoRelativePath_FromRootAndSubdirectory(t *testing.T) {
	repo := initRepoWithCommit(t)
	sub := filepath.Join(repo, "packages", "app")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	relRoot, err := NormalizeRepoRelativePath(repo, repo)
	require.NoError(t, err)
	assert.Equal(t, ".", relRoot)

	relSub, err := NormalizeRepoRelativePath(repo, sub)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("packages", "app"), relSub)
}

func TestNormalizeRepoRelativePath_CanonicalizesAliasedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is not portable on windows")
	}

	repo := initRepoWithCommit(t)
	sub := filepath.Join(repo, "packages", "app")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	aliasRoot := filepath.Join(t.TempDir(), "repo-alias")
	require.NoError(t, os.Symlink(repo, aliasRoot))

	relPath, err := NormalizeRepoRelativePath(aliasRoot, filepath.Join(aliasRoot, "packages", "app"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("packages", "app"), relPath)
}

func TestResolveWorktreeCWD_ValidatesRelativeSubdirectory(t *testing.T) {
	worktreeRoot := t.TempDir()
	worktreeRoot, err := filepath.EvalSymlinks(worktreeRoot)
	require.NoError(t, err)
	subdir := filepath.Join(worktreeRoot, "packages", "app")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	resolved, err := ResolveWorktreeCWD(worktreeRoot, filepath.Join("packages", "app"))
	require.NoError(t, err)
	assert.Equal(t, subdir, resolved)

	_, err = ResolveWorktreeCWD(worktreeRoot, filepath.Join("packages", "missing"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "saved worktree cwd unavailable")
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

func TestCreate_TightensMuxagentDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not portable on windows")
	}

	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	muxagentDir := filepath.Join(home, ".muxagent")
	worktreesDir := filepath.Join(muxagentDir, "worktrees")
	require.NoError(t, os.MkdirAll(worktreesDir, 0o755))

	wtPath, err := Create(repo, "perm-check")
	require.NoError(t, err)

	assertDirPerm(t, muxagentDir, 0o700)
	assertDirPerm(t, worktreesDir, 0o700)
	assertDirPerm(t, filepath.Dir(wtPath), 0o700)
}

func TestCleanup_RemovesWorktreeAndBranch(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	wtPath, err := Create(repo, "cleanup-check")
	require.NoError(t, err)

	err = Cleanup(repo, wtPath, BranchName("cleanup-check"))
	require.NoError(t, err)

	_, err = os.Stat(wtPath)
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))

	cmd := exec.Command("git", "-C", repo, "branch", "--list", BranchName("cleanup-check"))
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(out)))
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}
