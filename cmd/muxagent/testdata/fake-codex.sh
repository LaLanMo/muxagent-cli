#!/bin/sh
set -eu

output=""
resume_mode=0
resume_thread=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    resume)
      resume_mode=1
      resume_thread="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

if [ -z "$output" ]; then
  echo "missing -o" >&2
  exit 2
fi

artifact_dir=$(dirname "$output")
run_dir=$(basename "$artifact_dir")
node_name=${run_dir#*-}
state_dir=${FAKE_CODEX_STATE_DIR:-$(dirname "$artifact_dir")/.fake-state}
flow=${FAKE_CODEX_FLOW:-happy}

mkdir -p "$state_dir"
count_file="$state_dir/${node_name}.count"
count=0
if [ -f "$count_file" ]; then
  count=$(cat "$count_file")
fi
if [ "$resume_mode" -eq 1 ]; then
  if [ "$count" -le 0 ]; then
    echo "resume requested before initial thread start" >&2
    exit 2
  fi
  thread_id="$resume_thread"
  echo "{\"type\":\"item.completed\",\"message\":\"resumed ${node_name} #${count}\"}"
else
  count=$((count + 1))
  printf '%s' "$count" > "$count_file"
  thread_id="thread-${node_name}-${count}"
  echo "{\"type\":\"thread.started\",\"thread_id\":\"$thread_id\"}"
  echo "{\"type\":\"item.completed\",\"message\":\"running ${node_name} #${count}\"}"
fi

write_result() {
  artifact_name="$1"
  extra_json="$2"
  artifact_path="$artifact_dir/$artifact_name"
  printf '%s %s\n' "$node_name" "$count" > "$artifact_path"
  if [ -n "$extra_json" ]; then
    printf '{"kind":"result","result":{"file_paths":["%s"],%s},"clarification":null}\n' "$artifact_path" "$extra_json" > "$output"
  else
    printf '{"kind":"result","result":{"file_paths":["%s"]},"clarification":null}\n' "$artifact_path" > "$output"
  fi
}

case "$node_name" in
  upsert_plan)
    if [ "$flow" = "clarify-once" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
      cat > "$output" <<'JSON'
{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"Which path should we take?","why_it_matters":"The plan changes based on this choice.","options":[{"label":"A","description":"Option A"},{"label":"B","description":"Option B"}],"multi_select":false}]}}
JSON
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
    fi
    write_result "verify-${count}.md" "\"passed\":${passed}"
    ;;
  *)
    echo "unexpected node: $node_name" >&2
    exit 2
    ;;
esac
