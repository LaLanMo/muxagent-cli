package relayws

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupSessionSummariesFromOpencodeDB(t *testing.T) {
	if _, err := exec.LookPath(sqlite3Bin); err != nil {
		t.Skip("sqlite3 not installed")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "opencode.db")
	cmd := exec.Command(
		sqlite3Bin,
		dbPath,
		"create table session (id text primary key, directory text not null, title text not null, time_updated integer not null);"+
			"insert into session (id, directory, title, time_updated) values ('sid-1','/tmp/project','Resolved title',1767600000000);",
	)
	require.NoError(t, cmd.Run())

	previousPath := opencodeDBPath
	opencodeDBPath = func() (string, error) { return dbPath, nil }
	defer func() { opencodeDBPath = previousPath }()

	sessions, err := lookupSessionSummariesFromOpencodeDB(context.Background(), []string{"sid-1"})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, "sid-1", sessions[0].SessionID)
	require.Equal(t, "/tmp/project", sessions[0].CWD)
	require.Equal(t, "Resolved title", sessions[0].Title)
}
