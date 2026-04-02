#!/bin/sh
set -eu

prompt=""
resume_mode=0
session_id=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --session-id)
      session_id="$2"
      shift 2
      ;;
    --resume)
      resume_mode=1
      session_id="$2"
      shift 2
      ;;
    *)
      prompt="$1"
      shift
      ;;
  esac
done

if [ -z "$session_id" ]; then
  echo "missing session id" >&2
  exit 2
fi

detect_node_name() {
  node=$(printf '%s\n' "$prompt" | sed -n 's/^Step: //p' | head -n 1)
  if [ -n "$node" ]; then
    printf '%s' "$node"
    return
  fi
  case "$prompt" in
    *"same draft_plan step"*) echo "draft_plan" ;;
    *"Iteration "*" of planning."*) echo "draft_plan" ;;
    *"Iteration "*" of review."*) echo "review_plan" ;;
    *"Iteration "*" of implementation."*) echo "implement" ;;
    *"Iteration "*" of verification."*) echo "verify" ;;
    *)
      echo "unknown" ;;
  esac
}

artifact_dir() {
  dir=$(printf '%s\n' "$prompt" | sed -n 's/^ArtifactDir: //p' | head -n 1)
  if [ -n "$dir" ]; then
    printf '%s' "$dir"
    return
  fi
  dir=$(printf '%s\n' "$prompt" | sed -n 's/.* under \(\/[^ ]*\).*/\1/p' | head -n 1)
  if [ -z "$dir" ]; then
    dir=$(printf '%s\n' "$prompt" | sed -n 's/.*under: \(\/[^ ]*\).*/\1/p' | head -n 1)
  fi
  dir=${dir%.}
  dir=${dir%:}
  printf '%s' "$dir"
}

node_name=$(detect_node_name)
if [ "$node_name" = "unknown" ]; then
  echo "could not detect node from prompt" >&2
  exit 2
fi

state_dir=${FAKE_CLAUDE_STATE_DIR:-.fake-claude-state}
flow=${FAKE_CLAUDE_FLOW:-happy}
mkdir -p "$state_dir"
count_file="$state_dir/${node_name}.count"
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
if [ "$resume_mode" -eq 0 ]; then
  count=$((count + 1))
  printf '%s' "$count" > "$count_file"
fi

artifact_dir=$(artifact_dir)
write_result() {
  artifact_name="$1"
  extra_json="$2"
  mkdir -p "$artifact_dir"
  artifact_path="$artifact_dir/$artifact_name"
  printf '%s %s\n' "$node_name" "$count" > "$artifact_path"
  if [ -n "$extra_json" ]; then
    printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":{"kind":"result","result":{"file_paths":["%s"],%s},"clarification":null}}\n' "$session_id" "$artifact_path" "$extra_json"
  else
    printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":{"kind":"result","result":{"file_paths":["%s"]},"clarification":null}}\n' "$session_id" "$artifact_path"
  fi
}

printf '{"type":"assistant","message":"running %s #%s","session_id":"%s"}\n' "$node_name" "$count" "$session_id"

case "$node_name" in
  draft_plan)
    if [ "$flow" = "clarify-once" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
      printf '{"type":"result","subtype":"success","session_id":"%s","structured_output":{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"Which path should we take?","why_it_matters":"The plan changes based on this choice.","options":[{"label":"A","description":"Option A"},{"label":"B","description":"Option B"}],"multi_select":false}]}}}\n' "$session_id"
      exit 0
    fi
    write_result "plan-${count}.md" ""
    ;;
  review_plan)
    passed=true
    if [ "$flow" = "review-reject-once" ] && [ "$count" -eq 1 ]; then
      passed=false
    fi
    write_result "review-${count}.md" "\"passed\":${passed}"
    ;;
  handle_request)
    write_result "result-${count}.md" ""
    ;;
  implement)
    if [ "$flow" = "implement-fail-once" ] && [ "$count" -eq 1 ]; then
      echo "simulated implement failure" >&2
      exit 1
    fi
    write_result "implementation-${count}.md" ""
    ;;
  verify)
    passed=true
    if [ "$flow" = "verify-fail" ]; then
      passed=false
    elif [ "$flow" = "verify-fail-once" ] && [ "$count" -eq 1 ]; then
      passed=false
    fi
    write_result "verify-${count}.md" "\"passed\":${passed}"
    ;;
  *)
    echo "unexpected node: $node_name" >&2
    exit 2
    ;;
esac
