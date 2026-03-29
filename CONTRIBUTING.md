# Contributing

Thanks for contributing to MuxAgent CLI.

This project is still moving quickly, so the most helpful contributions are
small, focused changes with a clear user-facing reason.

## Before You Start

- Open an issue before large changes, new features, or behavior changes.
- For small fixes, docs updates, or targeted tests, a pull request is usually
  fine without prior discussion.
- Keep pull requests narrow. Separate refactors from behavior changes.

## Development Setup

Requirements:

- Go
- A POSIX shell for helper scripts

Useful commands:

```bash
go test ./...
go build ./cmd/muxagent
```

The release helper scripts under `scripts/` are for maintainers. Most
contributors do not need them for normal development.

## Pull Request Expectations

Please include:

- What problem the change solves.
- Why the chosen approach is correct.
- Tests for behavior changes when practical.
- README or docs updates if the user workflow changed.

If your change affects CLI output, auth, update behavior, daemon lifecycle, or
runtime resolution, include enough detail for reviewers to validate the impact.

## Testing Guidelines

- Run `go test ./...` before opening a pull request.
- Prefer table-driven tests for branching behavior.
- Use `t.Setenv()` and `t.TempDir()` in tests instead of mutating shared state.
- Write CLI output through `cmd.OutOrStdout()` where possible so it remains
  testable.

## Commit Style

Conventional Commits are preferred, for example:

- `feat: add release installer`
- `fix: harden update manifest verification`
- `docs: clarify first-time pairing flow`

You do not need perfect commit history in a pull request, but clear commit
messages make review easier.

## Security and Privacy

- Do not commit secrets, tokens, private keys, or the contents of
  `~/.muxagent/`.
- Do not commit local editor state, personal file paths, or generated binaries.
- If you find a security issue, follow [SECURITY.md](SECURITY.md) instead of
  opening a public issue with exploit details.

## Task TUI E2E

The repository includes a checked-in end-to-end smoke test for the
task-first TUI. It launches the real `muxagent` binary in a PTY, swaps in a
fake `codex` executable, drives the Bubble Tea UI, and verifies SQLite plus
artifact persistence in the current working directory.

Run just the task TUI E2E:

```bash
go test ./cmd/muxagent -run TestTaskTUIEndToEndScenarios -count=1
```

## Task TUI Visual Guidance

The current approved task-first TUI uses a few explicit visual rules.

- Running is the shared orange emphasis color in both the task list and task detail views.
- Awaiting user input uses the amber status treatment; do not reuse it for generic selection or focus.
- Selection is indicated separately from domain status, for example with the task-list arrow marker.

## License

By contributing to this repository, you agree that your contributions will be
licensed under the repository's AGPL-3.0 license.
