# Task Config Semantics

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

## Output Envelope

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

## Result Schema Constraints

Agent `result_schema` must stay within the current shared Structured Outputs subset used by both Codex and Claude task runs.

- The schema root must be an object.
- Every object must set `additionalProperties: false`.
- Every object property must be listed in `required`.
- Do not use a top-level discriminated union such as `anyOf`/`oneOf`; use `null` unions for optional semantics instead.
