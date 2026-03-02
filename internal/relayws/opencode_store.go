package relayws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/domain"
)

var resolveSessionSummaries = lookupSessionSummariesFromOpencodeDB

func lookupSessionSummariesFromOpencodeDB(
	ctx context.Context,
	sessionIDs []string,
) ([]domain.SessionSummary, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}

	dbPath, err := opencodeDBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("stat opencode db: %w", err)
	}

	quoted := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if sessionID == "" {
			continue
		}
		quoted = append(quoted, sqliteQuote(sessionID))
	}
	if len(quoted) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(
		"select id as sessionId, directory as cwd, title, time_updated as timeUpdated from session where id in (%s)",
		strings.Join(quoted, ","),
	)
	cmd := exec.CommandContext(ctx, sqlite3Bin, "-json", dbPath, query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("query opencode db: %w: %s", err, strings.TrimSpace(string(output)))
	}

	var rows []struct {
		SessionID   string `json:"sessionId"`
		CWD         string `json:"cwd"`
		Title       string `json:"title"`
		TimeUpdated int64  `json:"timeUpdated"`
	}
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("parse opencode db result: %w", err)
	}

	result := make([]domain.SessionSummary, 0, len(rows))
	for _, row := range rows {
		updatedAt := time.UnixMilli(row.TimeUpdated).UTC()
		result = append(result, domain.SessionSummary{
			SessionID: row.SessionID,
			CWD:       row.CWD,
			Title:     row.Title,
			UpdatedAt: updatedAt,
		})
	}
	return result, nil
}

var sqlite3Bin = "sqlite3"

var opencodeDBPath = func() (string, error) {
	if path := os.Getenv("MUXAGENT_OPENCODE_DB_PATH"); path != "" {
		return path, nil
	}
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "opencode", "opencode.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db"), nil
}

func sqliteQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
