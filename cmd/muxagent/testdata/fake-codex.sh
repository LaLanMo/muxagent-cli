#!/bin/sh
set -eu

output=""
resume_mode=0
resume_thread=""
prompt=""
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
      prompt="$1"
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
printf '%s' "$prompt" > "$state_dir/${run_dir}.prompt.txt"
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

if [ "$flow" = "slow-happy" ]; then
  sleep 0.3
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
  draft_plan)
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
    if [ "$flow" = "clarify-late" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
      cat > "$output" <<'JSON'
{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"进入 chat_screen 时，屏幕上显示的具体状态是什么？","why_it_matters":"区分 history.complete 超时、消息事件丢失、还是 RPC 本身卡住，会直接决定后续排查方向。这段说明故意写得比较长，用来验证 clarification 表单在有 artifacts 的 detail 屏里也不会把 footer 或 artifact pane 挤没。","options":[{"label":"显示 'This session cannot be restored on this device yet.'","description":"说明 session.load 超时，history.complete 事件没到达前端，大概率是 activeSession 竞态或 WS 断连。"},{"label":"显示 'Send a message to get started' 或正常 UI 但没有消息","description":"说明 session.load 已经返回，但消息事件在处理链路中被丢弃，或 chatState 没被正确填充。"},{"label":"一直停留在 loading spinner","description":"说明 session.load RPC 自身就卡住了，可能是 Go daemon 无响应、relayws 没连上，或者 runtime 本身卡死。"},{"label":"其他表现","description":"以上都不符合，需要补充更多具体症状。"}],"multi_select":true}]}}
JSON
      exit 0
    fi
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
    if [ "$flow" = "yolo-replan-once" ]; then
      write_result "verify-${count}.md" "\"passed\":${passed},\"summary\":\"wave ${count} complete\""
    else
      write_result "verify-${count}.md" "\"passed\":${passed}"
    fi
    ;;
  evaluate_progress)
    next_node="done"
    reason="Task complete"
    next_focus=""
    if [ "$flow" = "yolo-replan-once" ] && [ "$count" -eq 1 ]; then
      next_node="draft_plan"
      reason="A follow-up planning wave is still required"
      next_focus="Plan the remaining work for the next wave"
    fi
    write_result "evaluate-${count}.md" "\"next_node\":\"${next_node}\",\"reason\":\"${reason}\",\"next_focus\":\"${next_focus}\""
    ;;
  *)
    echo "unexpected node: $node_name" >&2
    exit 2
    ;;
esac
