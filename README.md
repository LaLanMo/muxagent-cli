# MuxAgent CLI

![MuxAgent Task TUI](task-tui.png)

MuxAgent is a CLI for running coding agents through graph-based workflows.
Use graph-based workflows to plan, review, approve, implement, and verify code
with Codex or Claude Code.

## What MuxAgent Does

- **Task System** — Run code tasks through graph-based workflows with explicit
  planning, review, approval, implementation, and verification steps. Five
  ready-made configs cover different risk tolerances. Supports Codex and
  Claude Code runtimes.
- **Remote Control** — Monitor and control Claude Code sessions from your
  phone via a paired mobile app.

## Installation

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/LaLanMo/muxagent-cli/main/install.sh | sh
```

The install script puts `muxagent` in `/usr/local/bin` when writable, otherwise
it falls back to `~/.local/bin`.

### Windows

Download the latest `muxagent-windows-*.zip` bundle from
[GitHub Releases](https://github.com/LaLanMo/muxagent-cli/releases), unzip it,
and run `muxagent.exe`.

Official installs include everything needed to run MuxAgent with Claude Code.

## Quick Start

### Task System

```bash
muxagent
```

This opens the workflow CLI. Pick a task config (`default`, `plan-only`,
`single-run`, `autonomous`, or `yolo`), describe the task, and MuxAgent routes
the agent through the workflow for you.

### Remote Control

![MuxAgent Remote Control](og-image.png)

1. Download the MuxAgent mobile app.
   [Google Play](https://play.google.com/store/apps/details?id=ai.soloflux.muxagent) | iOS coming soon.
2. Run:

   ```bash
   muxagent daemon start
   ```

3. Scan the QR code in the app to finish setup.

On a new machine, `muxagent daemon start` begins first-time setup, shows a QR
code, waits for approval in the mobile app, and then starts the daemon.

You can also run `muxagent auth login` manually if you want to pair before
starting the daemon.

## Workflow Graphs

A task config defines a workflow graph — the sequence of nodes and the edges
between them that an AI agent follows. MuxAgent ships five ready-made configs:

**`default`** — When you want human sign-off before code changes land.

```
        ┌─────────────────────────┐
        │  (approval rejected)    │
        ▼                         │
       plan ──▶ review ──▶ approve ──▶ implement ──▶ verify ──▶ done
        ▲         │                      ▲              │
        └─────────┘                      └──────────────┘
     (review rejected)                    (verify failed)
```

**`plan-only`** — When you want a reviewed plan without touching code.

```
       plan ──▶ review ──▶ done
        ▲         │
        └─────────┘
     (review rejected)
```

**`single-run`** — Handle one request once, then stop.

```
   handle_request ──▶ done
```

**`autonomous`** — When you trust the agent and want fast iteration.

```
       plan ──▶ review ──▶ implement ──▶ verify ──▶ done
        ▲         │           ▲              │
        └─────────┘           └──────────────┘
     (review rejected)         (verify failed)
```

**`yolo`** — Fully autonomous multi-wave mode. No approval, no clarification.

```
       ┌──────────────────────────────────────────────────┐
       │                                    (next wave)   │
       ▼                                                  │
      plan ──▶ review ──▶ implement ──▶ verify ──▶ evaluate ──▶ done
       ▲         │           ▲              │
       └─────────┘           └──────────────┘
    (review rejected)         (verify failed)
```

Workflow configs are different from runtime selection:

- a workflow config chooses the graph, bundled prompts, and product intent
- runtime selection chooses which coding runtime executes agent nodes, for example `codex` or `claude-code`

Follow-up tasks inherit the parent task config. If a task starts in
`single-run`, its follow-up tasks also start in `single-run`.

## Customizing Workflows

The included workflow configs are stored as task config bundles under `~/.muxagent/taskconfigs`.
You can clone them and modify the YAML to change the workflow graph, prompts,
runtime, iteration limits, or clarification settings.

If you already have a user config named `plan-only`, `single-run`,
`autonomous`, or `yolo`, MuxAgent preserves it and installs the built-in
config under a fallback alias such as `builtin-plan-only`. Existing bundle
files are never overwritten.

See [Task Config Semantics](docs/task-config-semantics.md) for the full edge,
iteration, and schema specification.

## Commands

**Task TUI**

- `muxagent` — Launch the interactive workflow CLI.

**Daemon**

- `muxagent daemon start` — Start first-time setup or start the daemon.
- `muxagent daemon status` — Show daemon status.
- `muxagent daemon stop` — Stop the daemon.

**Auth**

- `muxagent auth status` — Show pairing status.

**General**

- `muxagent version` — Show the installed CLI version.
- `muxagent update` — Update `muxagent`.
