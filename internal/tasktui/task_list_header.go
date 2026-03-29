package tasktui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	appconfig "github.com/LaLanMo/muxagent-cli/internal/config"
)

type versionMeta struct {
	Label    string
	Revision string
	Dev      bool
}

func (m Model) renderTaskListHeader(width int) string {
	if width <= 0 {
		return ""
	}
	meta := parseVersionMeta(m.version)
	cwd := prettyTaskListPath(m.workDir)
	if width < 72 {
		return renderCompactTaskListHeader(width, meta, cwd)
	}
	return renderWideTaskListHeader(width, meta, cwd)
}

func renderWideTaskListHeader(width int, meta versionMeta, cwd string) string {
	hero := renderTaskListWordmark(width)
	version := centerHeaderLine(renderTaskListVersionMeta(meta), width)
	metaBlock := renderTaskListMetadataBlock(width, cwd, true)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		hero,
		version,
		metaBlock,
	)
}

func renderCompactTaskListHeader(width int, meta versionMeta, cwd string) string {
	top := joinHorizontal(
		tuiTheme.Header.Brand.Render("muxagent"),
		renderTaskListVersionMeta(meta),
		width,
	)
	hero := centerHeaderLine(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			tuiTheme.Header.Hero.Render("MUX"),
			tuiTheme.Header.Hero.Render("AGENT"),
		),
		width,
	)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		top,
		hero,
		renderTaskListMetadataBlock(width, cwd, false),
	)
}

func renderTaskListWordmark(width int) string {
	muxRows := renderBlockGlyphRows("MUX")
	agentRows := renderBlockGlyphRows("AGENT")
	lines := make([]string, len(muxRows))
	for i := range muxRows {
		lines[i] = tuiTheme.Header.Hero.Render(muxRows[i]) +
			"    " +
			tuiTheme.Header.Hero.Render(agentRows[i])
	}
	block := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return lipgloss.Place(width, lipgloss.Height(block), lipgloss.Center, lipgloss.Top, block)
}

func renderBlockGlyphRows(text string) []string {
	rows := make([]string, 7)
	for i, ch := range strings.ToUpper(text) {
		glyph, ok := taskListWordmarkGlyphs[ch]
		if !ok {
			continue
		}
		for row := range rows {
			if i > 0 {
				rows[row] += " "
			}
			rows[row] += glyph[row]
		}
	}
	return rows
}

func renderTaskListVersionMeta(meta versionMeta) string {
	if meta.Label == "" {
		return ""
	}
	label := tuiTheme.version.Render(meta.Label)
	if meta.Dev && meta.Revision != "" {
		return label + tuiTheme.Header.MetaLabel.Render(" · ") + tuiTheme.Header.MetaValue.Render(meta.Revision)
	}
	return label
}

func renderTaskListMetadataBlock(width int, cwd string, centered bool) string {
	line := renderTaskListMetaLine("cwd", cwd, false)
	if centered {
		return centerHeaderLine(line, width)
	}
	return fitLine(line, width)
}

func renderTaskListMetaLine(label, value string, strong bool) string {
	if value == "" {
		value = "—"
	}
	valueStyle := tuiTheme.Header.MetaValue
	if strong {
		valueStyle = tuiTheme.Header.MetaStrong
	}
	return tuiTheme.Header.MetaLabel.Render(label+" ") + valueStyle.Render(value)
}

func centerHeaderLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Truncate(line, width, "")
	return lipgloss.Place(width, 1, lipgloss.Center, lipgloss.Top, line)
}

var taskListWordmarkGlyphs = map[rune][7]string{
	'A': {" ███████ ", "███   ███", "███   ███", "█████████", "███   ███", "███   ███", "███   ███"},
	'E': {"█████████", "███      ", "███      ", "███████  ", "███      ", "███      ", "█████████"},
	'G': {" ███████ ", "███      ", "███      ", "███  ████", "███   ███", "███   ███", " ███████ "},
	'M': {"███   ███", "████ ████", "██ ███ ██", "██  █  ██", "██     ██", "██     ██", "██     ██"},
	'N': {"███   ███", "████  ███", "█████ ███", "██ ██████", "███ █████", "███  ████", "███   ███"},
	'T': {"█████████", "   ███   ", "   ███   ", "   ███   ", "   ███   ", "   ███   ", "   ███   "},
	'U': {"███   ███", "███   ███", "███   ███", "███   ███", "███   ███", "███   ███", " ███████ "},
	'X': {"███   ███", "███   ███", " ███ ███ ", "  █████  ", " ███ ███ ", "███   ███", "███   ███"},
}

func parseVersionMeta(raw string) versionMeta {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "muxagent version ")
	clean = strings.TrimPrefix(clean, "version ")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return versionMeta{Label: "v0.1.0"}
	}
	if strings.HasPrefix(clean, "dev") {
		meta := versionMeta{Label: "dev", Dev: true}
		if start := strings.Index(clean, "("); start >= 0 {
			if end := strings.Index(clean[start:], ")"); end > 0 {
				meta.Revision = strings.TrimSpace(clean[start+1 : start+end])
			}
		}
		return meta
	}
	return versionMeta{Label: normalizeVersionLabel(clean)}
}

func prettyTaskListPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "."
	}
	path = filepath.ToSlash(filepath.Clean(path))
	if home, err := os.UserHomeDir(); err == nil {
		home = filepath.ToSlash(filepath.Clean(home))
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			return "~/" + strings.TrimPrefix(path, home+"/")
		}
	}
	return path
}

func runtimeDisplayLabel(id appconfig.RuntimeID) string {
	switch id {
	case appconfig.RuntimeClaudeCode:
		return "Claude Code"
	case appconfig.RuntimeCodex:
		return "Codex"
	default:
		return string(id)
	}
}
