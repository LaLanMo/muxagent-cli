# MuxAgent CLI

CLI for managing MuxAgent runtimes.

## Installation

```bash
go install github.com/LaLanMo/muxagent-cli/cmd/muxagent@latest
```

## Commands

### Config Commands

- `muxagent config init` - Create default config file
  - `--project, -p` - Create project-local config (`./.muxagent/config.json`)
  - `--force, -f` - Overwrite existing config file

- `muxagent config show` - Show effective config
  - `--path` - Show config file paths instead of values

### Health Check

- `muxagent health` - Check runtime health and connectivity

### Daemon Management

- `muxagent daemon start` - Start background daemon
- `muxagent daemon start-sync` - Start daemon in foreground
- `muxagent daemon stop` - Stop running daemon
- `muxagent daemon status` - Check daemon status

## Configuration

### Config Files (Priority: High to Low)

1. **Environment variables** (`MUXAGENT_*`) - Highest priority
2. **Project config** (`./.muxagent/config.json`) - Per-project overrides
3. **User config** (`~/.muxagent/config.json`) - User defaults
4. **Built-in defaults** - Fallback values

Each layer overrides the previous. Non-empty values win; empty values don't override.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MUXAGENT_RUNTIMES_OPENCODE_BASE_URL` | OpenCode server URL | `http://127.0.0.1:4096` |
| `MUXAGENT_RUNTIMES_OPENCODE_USERNAME` | Basic auth username | `opencode` |
| `MUXAGENT_RUNTIMES_OPENCODE_PASSWORD` | Basic auth password | (empty) |

### Config File Format

```json
{
  "active_runtime": "opencode",
  "runtimes": {
    "opencode": {
      "base_url": "http://127.0.0.1:4096",
      "auth": {
        "username": "opencode",
        "password": ""
      }
    }
  }
}
```

### Configuration Examples

**User config with custom URL:**
```json
{
  "runtimes": {
    "opencode": {
      "base_url": "http://192.168.1.100:4096"
    }
  }
}
```

**Project config with authentication:**
```json
{
  "runtimes": {
    "opencode": {
      "auth": {
        "password": "project-specific-password"
      }
    }
  }
}
```

**Environment override:**
```bash
export MUXAGENT_RUNTIMES_OPENCODE_BASE_URL="http://production:4096"
export MUXAGENT_RUNTIMES_OPENCODE_PASSWORD="secret"
muxagent health
```

## Quick Start

1. Initialize config:
   ```bash
   muxagent config init
   ```

2. View effective config:
   ```bash
   muxagent config show
   ```

3. Check connectivity:
   ```bash
   muxagent health
   ```

4. Start daemon:
   ```bash
   muxagent daemon start
   ```
