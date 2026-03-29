# MuxAgent CLI

![MuxAgent](og-image.png)

MuxAgent lets you monitor and control Claude Code from your phone.

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
- `muxagent version` - Show the installed CLI version.
- `muxagent update` - Update `muxagent`.
- `muxagent --runtime claude-code` - Launch the task-first TUI with Claude Code for new tasks. Bare `muxagent` still defaults to Codex.

## Built-in Task Configs

The task-first TUI seeds three built-in task configs:

- `default` - safest general-purpose software engineering flow. It plans, reviews, pauses for human approval, implements, and verifies.
- `plan-only` - read-only planning flow. It loops between planning and review, then stops after a reviewed plan.
- `autonomous` - faster software engineering flow. It keeps planning, review, implementation, and verification, but removes the manual approval step.

Built-in configs are different from runtime selection:

- a built-in config chooses the workflow graph, bundled prompts, and product intent
- runtime selection chooses which coding runtime executes agent nodes, for example `codex` or `claude-code`

Built-in configs are stored as task config bundles under `~/.muxagent/taskconfigs`.
They appear first in the config screen. You can clone them, but you cannot rename
or delete the built-in rows themselves.

If you already have a user config named `plan-only` or `autonomous`, MuxAgent
preserves it and installs the built-in config under a fallback alias such as
`builtin-plan-only`. Existing bundle files are never overwritten.

## Experimental Task Config Semantics

The task-first TUI uses `edge` as its only control-flow primitive.

- An `edge` is selected after a node finishes.
- An `iteration` is the 1-based `NodeRun` ordinal for one node within one task.
- A clarification round does not create a new `NodeRun`, so it stays within the current iteration.
- A clarification follow-up resumes the same runtime session or thread for that `NodeRun`; it does not start a fresh session.

If an edge points to a node that has already run earlier in the same task, the
runtime simply attempts to create the next iteration for that node.

`max_iterations` is evaluated per node, per task. It limits how many `NodeRun`
records that node may create within the task.

- `max_iterations: 1` means the node may run once and may not be re-entered.
- `max_iterations: 5` means the node may create up to five `NodeRun` records in that task.
- Other nodes keep their own counters. One node hitting its limit does not consume iterations for any other node.

When the runtime is about to create a new `NodeRun` for a node and doing so
would make that node's iteration exceed `max_iterations`, the runtime must not
create the `NodeRun` and the task fails.

For a node with multiple incoming edges, later selected edges that would create
a higher iteration are always independent. Historical pending inputs are never
merged into a later iteration. If join-style fan-in is supported for a node's first
`NodeRun`, that join only controls when the runtime may create that first
`NodeRun`; it does not change the per-node iteration numbering.

Agent nodes use a shared system-generated output envelope across both task runtimes.

- `node.result_schema` only describes the nested `result` payload.
- If clarification is enabled for a node, the generated envelope requires `kind`, `result`, and `clarification`.
- The inactive branch must be `null`.
- Once a node has exhausted `max_clarification_rounds`, later turns on that same thread must return `kind=result` and `clarification=null`.
- If clarification is disabled, the generated envelope only allows `kind=result` and `result`.
- The Codex transport asks the `codex` CLI to validate against that schema and write `output.json` directly.
- The Claude transport asks the `claude` CLI for `structured_output`, then the executor validates that envelope and writes `output.json` itself.
- Every `NodeRun` artifact directory keeps that machine-readable `output.json`.
- User-facing input is exported alongside it as `input.md`: agent nodes store the prompt they received, human nodes store the submitted payload, and clarification flows extend the same file with the clarification history and answers.
- Clarification runtime state still lives in SQLite `clarifications_json`; `input.md` is the readable audit trail for the same exchanges and answers.

Agent `result_schema` must stay within the current shared Structured Outputs subset used by both Codex and Claude task runs.

- The schema root must be an object.
- Every object must set `additionalProperties: false`.
- Every object property must be listed in `required`.
- Do not use a top-level discriminated union such as `anyOf`/`oneOf`; use `null` unions for optional semantics instead.

## Task TUI E2E

The repository now includes a checked-in end-to-end smoke test for the
task-first TUI. It launches the real `muxagent` binary in a PTY, swaps in a
fake `codex` executable, drives the Bubble Tea UI, and verifies SQLite plus
artifact persistence in the current working directory.

## Task TUI Visual Guidance

The current approved task-first TUI uses a few explicit visual rules.

- Running is the shared orange emphasis color in both the task list and task detail views.
- Awaiting user input uses the amber status treatment; do not reuse it for generic selection or focus.
- Selection is indicated separately from domain status, for example with the task-list arrow marker.

Run just the task TUI E2E:

```bash
go test ./cmd/muxagent -run TestTaskTUIEndToEndScenarios -count=1
```

Run the full suite:

```bash
go test ./...
```
