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

## License

By contributing to this repository, you agree that your contributions will be
licensed under the repository's AGPL-3.0 license.
