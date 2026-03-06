package worktree

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_NewWithNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktrees.json")
	s := NewStore(path)
	err := s.Load()
	require.NoError(t, err)
	assert.Nil(t, s.Get("anything"))
}

func TestStore_SetAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktrees.json")
	s := NewStore(path)
	require.NoError(t, s.Load())

	m := Mapping{
		WorktreeID:   "abc",
		WorktreePath: "/tmp/wt/abc",
		RepoRoot:     "/tmp/repo",
		BranchName:   "muxagent/abc",
	}
	s.Set("session-1", m)

	got := s.Get("session-1")
	require.NotNil(t, got)
	assert.Equal(t, m, *got)

	assert.Nil(t, s.Get("session-2"))
}

func TestStore_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktrees.json")
	s := NewStore(path)
	require.NoError(t, s.Load())

	m := Mapping{
		WorktreeID:   "abc",
		WorktreePath: "/tmp/wt/abc",
		RepoRoot:     "/tmp/repo",
		BranchName:   "muxagent/abc",
	}
	s.Set("session-1", m)
	require.NoError(t, s.Save())

	s2 := NewStore(path)
	require.NoError(t, s2.Load())

	got := s2.Get("session-1")
	require.NotNil(t, got)
	assert.Equal(t, m, *got)
}

func TestStore_Delete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktrees.json")
	s := NewStore(path)
	require.NoError(t, s.Load())

	s.Set("session-1", Mapping{WorktreeID: "abc"})
	s.Delete("session-1")
	require.NoError(t, s.Save())

	s2 := NewStore(path)
	require.NoError(t, s2.Load())
	assert.Nil(t, s2.Get("session-1"))
}

func TestStore_ConcurrentAccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worktrees.json")
	s := NewStore(path)
	require.NoError(t, s.Load())

	var wg sync.WaitGroup
	n := 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("session-%d", i)
			s.Set(id, Mapping{WorktreeID: fmt.Sprintf("wt-%d", i)})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		id := fmt.Sprintf("session-%d", i)
		got := s.Get(id)
		require.NotNil(t, got, "missing %s", id)
		assert.Equal(t, fmt.Sprintf("wt-%d", i), got.WorktreeID)
	}
}
