# MuxAgent CLI - Agent Guidelines

## Testing Patterns

### Table-Driven Tests with Callbacks
Tests use `setup`/`verify` or `before`/`after` callback pattern:

```go
tests := []struct {
    name   string
    setup  func(t *testing.T)
    verify func(t *testing.T, result Type, err error)
}{...}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        t.Setenv("HOME", t.TempDir())  // isolate HOME
        tt.setup(t)
        result, err := FunctionUnderTest()
        tt.verify(t, result, err)
    })
}
```

### Testify Conventions
- `require.*` - fails test immediately (use for setup/preconditions)
- `assert.*` - continues test (use for actual assertions)

### Environment Isolation
- Always use `t.Setenv()` (not `os.Setenv()`) - auto-restores after test
- Always use `t.TempDir()` for temp files - auto-cleans after test

## Code Conventions

### File Permissions
- `0o600` for files containing secrets (config, state, tokens)
- `0o755` for directories

### JSON Fields
- Use `snake_case` in JSON tags (e.g., `json:"start_time"`)

### CLI Output
- Write to `cmd.OutOrStdout()` (not `fmt.Print`) for testability
