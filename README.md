# MuxAgent CLI

MuxAgent lets you monitor and control Claude Code from your phone.

## Installation

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/LaLanMo/muxagent-cli/main/install.sh | sh
```

The install script puts `muxagent` in `/usr/local/bin` when writable, otherwise
it falls back to `~/.local/bin`.

### Windows

Download the latest `muxagent` binary from
[GitHub Releases](https://github.com/LaLanMo/muxagent-cli/releases).

Official installs include everything needed to run MuxAgent with Claude Code.

## Quick Start

1. Install `muxagent`.
2. Download the MuxAgent mobile app.
   Public download is coming soon.
3. Run:

   ```bash
   muxagent daemon start
   ```

4. Scan the QR code in the app to finish setup.

On a new machine, `muxagent daemon start` begins first-time setup, shows a QR
code, waits for approval in the mobile app, and then starts the daemon.

You can also run `muxagent auth login` manually if you want to pair before
starting the daemon.

## Essential Commands

- `muxagent daemon start` - Start first-time setup or start the daemon.
- `muxagent daemon status` - Show daemon status.
- `muxagent daemon stop` - Stop the daemon.
- `muxagent auth status` - Show pairing status.
- `muxagent update` - Update `muxagent`.
