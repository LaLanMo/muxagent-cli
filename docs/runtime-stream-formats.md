# Runtime Stream Formats

This document records the real streamed JSON shapes produced by the two runtimes currently supported by task execution:

- Codex CLI
- Claude Code

The goal is not to mirror either protocol exactly. The goal is to preserve enough real runtime structure to drive task-detail UI rendering without showing raw JSON.

## Why this exists

The task detail screen currently receives opaque progress strings from the executors, so it ends up rendering runtime JSON lines verbatim.

Before introducing a shared abstraction, we captured real streams using the repo's actual invocation style and sufficiently rich prompts so the runs exercised:

- session/thread start
- planning / todo updates
- shell commands
- file reads
- file edits
- file creates
- final structured output
- usage / result termination

## Invocation shape used by this repo

### Codex CLI

Source:

- `internal/taskexecutor/codexexec/codexexec.go`

Current invocation shape:

```sh
codex exec \
  -s danger-full-access \
  --json \
  --output-schema <schema-path> \
  -o <output-path> \
  -C <workdir> \
  --skip-git-repo-check \
  <prompt>
```

Resume shape:

```sh
codex exec \
  -s danger-full-access \
  --json \
  --output-schema <schema-path> \
  -o <output-path> \
  -C <workdir> \
  --skip-git-repo-check \
  resume <session-id> <prompt>
```

### Claude Code

Source:

- `internal/taskexecutor/claudeexec/claudeexec.go`

Current invocation shape:

```sh
claude \
  --print \
  --output-format stream-json \
  --verbose \
  --json-schema '<schema-json>' \
  --setting-sources user,project,local \
  --dangerously-skip-permissions \
  --session-id <node-run-id> \
  <prompt>
```

Resume shape:

```sh
claude \
  --print \
  --output-format stream-json \
  --verbose \
  --json-schema '<schema-json>' \
  --setting-sources user,project,local \
  --dangerously-skip-permissions \
  --resume <session-id> \
  <prompt>
```

## Captured runs

The following live probes were captured and inspected while designing the abstraction:

### Codex samples

- Read-only sample:
  - stream: `/tmp/muxagent-codex-readonly-sMnyoh/stream.jsonl`
  - stderr: `/tmp/muxagent-codex-readonly-sMnyoh/stderr.log`
- Edit sample:
  - stream: `/tmp/muxagent-codex-edit-capture-RnVk0P/stream.jsonl`
  - workdir: `/tmp/muxagent-codex-edit-workdir-XoZraf`

### Claude samples

- Read-only sample:
  - stream: `/tmp/muxagent-claude-bounded-UFtzxz/stream.jsonl`
- Edit sample:
  - stream: `/tmp/muxagent-claude-edit-capture-gzZR1S/stream.jsonl`
  - workdir: `/tmp/muxagent-claude-edit-workdir-wGga8j`

## Codex stream shape

Codex emits JSONL records that are primarily item-oriented.

Top-level event kinds observed:

- `thread.started`
- `turn.started`
- `item.started`
- `item.updated`
- `item.completed`
- `turn.completed`

Observed item subtypes:

- `todo_list`
- `command_execution`
- `file_change`
- `agent_message`

### Representative Codex records

Thread start:

```json
{"type":"thread.started","thread_id":"7b0f6d52-..."}
```

Todo list start:

```json
{"type":"item.started","item":{"id":"item_0","type":"todo_list","items":[{"text":"Inspect the repo state","completed":false},{"text":"Update sample files","completed":false},{"text":"Write summary note","completed":false}],"status":"in_progress"}}
```

Command execution start:

```json
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"cd /tmp/muxagent-codex-edit-workdir-XoZraf && pwd","aggregated_output":"","exit_code":null,"status":"in_progress"}}
```

Command execution complete:

```json
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"cd /tmp/muxagent-codex-edit-workdir-XoZraf && pwd","aggregated_output":"/tmp/muxagent-codex-edit-workdir-XoZraf\n","exit_code":0,"status":"completed"}}
```

Todo update:

```json
{"type":"item.updated","item":{"id":"item_0","type":"todo_list","items":[{"text":"Inspect the repo state","completed":true},{"text":"Update sample files","completed":false},{"text":"Write summary note","completed":false}],"status":"in_progress"}}
```

File change summary:

```json
{"type":"item.completed","item":{"id":"item_4","type":"file_change","changes":[{"path":"/tmp/muxagent-codex-edit-workdir-XoZraf/sample.txt","kind":"update"},{"path":"/tmp/muxagent-codex-edit-workdir-XoZraf/notes.md","kind":"add"}],"status":"completed"}}
```

Final assistant message carrying the structured result envelope:

```json
{"type":"item.completed","item":{"id":"item_7","type":"agent_message","text":"{\"kind\":\"result\",\"result\":{\"summary\":\"Updated sample.txt and wrote notes.md\",\"files\":[\"sample.txt\",\"notes.md\"]}}"}} 
```

Turn completion with usage:

```json
{"type":"turn.completed","usage":{"input_tokens":188100,"cached_input_tokens":166912,"output_tokens":1511}}
```

### Codex observations

- The stream is centered around lifecycle changes to durable items.
- Commands are summarized as whole-item updates, not as incremental stdout chunks.
- File edits are exposed only as coarse file-change summaries in the observed runs.
- Final structured output is embedded inside an `agent_message` item instead of appearing as a dedicated top-level `result` event.
- `thread.started.thread_id` is the stable task session identifier we should capture.
- `turn.completed.usage` is useful for a compact usage footer.

## Claude Code stream shape

Claude emits newline-delimited JSON messages that are primarily conversation-oriented, with tool calls represented as assistant messages and tool results represented as user messages.

Top-level event kinds observed:

- `system`
- `assistant`
- `user`
- `rate_limit_event`
- `result`

Observed assistant content item types:

- `thinking`
- `text`
- `tool_use`

Observed tool names:

- `Bash`
- `Read`
- `Edit`
- `Write`
- `StructuredOutput`

### Representative Claude records

System init:

```json
{"type":"system","subtype":"init","cwd":"/tmp/muxagent-claude-edit-workdir-wGga8j","session_id":"123e4567-e89b-12d3-a456-426614174111","tools":["Bash","Glob","Grep","LS","Read","Edit","Write","NotebookRead","NotebookEdit","WebFetch","TodoWrite","WebSearch"],"model":"claude-opus-4-6[1m]"}
```

Thinking:

```json
{"type":"assistant","message":{"id":"msg_...","role":"assistant","content":[{"type":"thinking","thinking":"I should inspect the files first, then update sample.txt, then write notes.md.","signature":"..."}]}}
```

Bash tool call:

```json
{"type":"assistant","message":{"id":"msg_...","role":"assistant","content":[{"type":"tool_use","id":"toolu_...","name":"Bash","input":{"command":"pwd","description":"Print working directory"},"caller":{"type":"direct"}}]}}
```

Tool result:

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_...","type":"tool_result","content":"/tmp/muxagent-claude-edit-workdir-wGga8j\n","is_error":false}]},"tool_use_result":{"stdout":"/tmp/muxagent-claude-edit-workdir-wGga8j\n","stderr":"","interrupted":false}}
```

Read tool call:

```json
{"type":"assistant","message":{"id":"msg_...","role":"assistant","content":[{"type":"tool_use","id":"toolu_...","name":"Read","input":{"file_path":"/tmp/muxagent-claude-edit-workdir-wGga8j/sample.txt"},"caller":{"type":"direct"}}]}}
```

Edit tool call:

```json
{"type":"assistant","message":{"id":"msg_...","role":"assistant","content":[{"type":"tool_use","id":"toolu_...","name":"Edit","input":{"replace_all":false,"file_path":"/tmp/muxagent-claude-edit-workdir-wGga8j/sample.txt","old_string":"alpha\nbeta\n","new_string":"alpha\nbeta\ngamma\n"},"caller":{"type":"direct"}}]}}
```

Edit tool result with patch metadata:

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_...","type":"tool_result","content":"The file /tmp/muxagent-claude-edit-workdir-wGga8j/sample.txt has been updated. Here's the result of running `cat -n` on a snippet of the edited file:\n     1\talpha\n     2\tbeta\n     3\tgamma\n","is_error":false}]},"tool_use_result":{"filePath":"/tmp/muxagent-claude-edit-workdir-wGga8j/sample.txt","oldString":"alpha\nbeta\n","newString":"alpha\nbeta\ngamma\n","structuredPatch":[{"oldStart":1,"oldLines":2,"newStart":1,"newLines":3,"lines":[" alpha"," beta","+gamma"]}],"userModified":false,"replaceAll":false}}
```

Write tool result:

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_...","type":"tool_result","content":"File created successfully at: /tmp/muxagent-claude-edit-workdir-wGga8j/notes.md","is_error":false}]},"tool_use_result":{"type":"create","filePath":"/tmp/muxagent-claude-edit-workdir-wGga8j/notes.md","content":"- Updated `sample.txt` ...","structuredPatch":[],"originalFile":null}}
```

Final result event:

```json
{"type":"result","subtype":"success","is_error":false,"duration_ms":30787,"duration_api_ms":34058,"num_turns":1,"result":"...","session_id":"123e4567-e89b-12d3-a456-426614174111","structured_output":{"kind":"result","result":{"summary":"Updated sample.txt and wrote notes.md","files":["sample.txt","notes.md"]}}}
```

### Claude observations

- The stream is centered around assistant/user conversational turns.
- Tool calls are explicit and high-fidelity.
- Tool results often carry structured metadata outside the visible text.
- File edits include patch-like metadata that is good enough to build UI diffs.
- The session identifier is stable from the initial `system/init` event and again on the final `result`.
- There may be `rate_limit_event` messages that are useful operationally but not primary task-detail content.

## Direct comparison

### Similarities

- Both runtimes expose a stable session identifier early in the stream.
- Both runtimes expose commands/tool actions as first-class events.
- Both runtimes eventually emit a machine-readable structured result compatible with the task executor envelope.

### Differences

- Codex is item-oriented; Claude is conversation-oriented.
- Codex observed file edits only as changed-file summaries; Claude exposes edit payloads and structured patches.
- Codex usage arrives on `turn.completed`; Claude usage-like completion data arrives on final `result`.
- Claude distinguishes assistant thought, assistant text, tool call, and tool result much more explicitly.

## Abstraction implications

The UI abstraction should be display-oriented, not protocol-oriented.

Recommended event categories:

- session started / updated
- assistant message part
- reasoning part
- tool call lifecycle
- plan update
- file change summary
- usage summary
- final result / terminal failure

Recommended normalized data carried by those events:

- stable event kind
- session id
- node run id
- message id
- part id
- role
- tool call id
- tool name
- tool kind
- tool status
- tool input summary
- output text
- error text
- diff entries
- changed locations or paths
- usage counters
- raw payload for debug only

## Mapping guidance

### Claude native tool mapping

For task-detail UI, Claude native tools should be normalized into the shared display taxonomy conservatively:

| Claude native tool | Shared kind | Notes |
| --- | --- | --- |
| `Bash` | `shell` | Show command text as subject. |
| `Grep` | `search` | Repo discovery, not mutation. |
| `Glob` | `search` | Repo discovery, not mutation. |
| `LS` | `search` | Repo discovery, not mutation. |
| `Read` | `read` | File read. |
| `NotebookRead` | `read` | Notebook read. |
| `Edit` | `edit` | In-place file edit. |
| `NotebookEdit` | `edit` | In-place notebook edit. |
| `Write` | `write` | File create or overwrite. |
| `WebFetch` | `fetch` | URL retrieval. |
| `WebSearch` | `fetch` | Network search or retrieval. |
| `TodoWrite` | native fallback | Keep the native tool name for now instead of forcing it into `write` or `plan`. |

If Claude emits a tool we do not normalize confidently, the UI should show the prettified native tool name instead of `other`.

### Codex to abstraction

- `thread.started` -> session event
- `item.started/completed` with `todo_list` -> plan update
- `item.started/completed` with `command_execution` -> tool call started/completed
- `item.completed` with `file_change` -> file change summary
- `item.completed` with `agent_message` -> assistant message
- `turn.completed.usage` -> usage summary

### Claude to abstraction

- `system/init` -> session event
- `assistant.content[type=thinking]` -> reasoning part
- `assistant.content[type=text]` -> assistant message part
- `assistant.content[type=tool_use]` -> tool call started
- `user.message.content[type=tool_result]` -> tool call completed / failed
- `result[subtype=success]` -> usage summary + final result
- `result[subtype=error_*]` -> terminal failure

## Constraints for implementation

- Keep runtime-specific JSON parsing in executor adapters, not in the TUI.
- Preserve a plain text fallback when a payload cannot be normalized.
- Keep raw payload available for debug, but do not render it by default.
- Prefer ACP naming and shapes where it helps, but avoid importing the whole ACP protocol into task execution.
