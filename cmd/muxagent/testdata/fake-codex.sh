#!/bin/sh
set -eu

flow=${FAKE_CODEX_FLOW:-happy}

discover_active_artifact_dir() {
  artifact_dir=""
  roots="$PWD"
  if [ -n "${FAKE_CODEX_STATE_DIR:-}" ]; then
    state_root=$(dirname "$FAKE_CODEX_STATE_DIR")
    if [ "$state_root" != "$PWD" ]; then
      roots="$roots
$state_root"
    fi
  fi
  old_ifs=$IFS
  IFS='
'
  for root in $roots; do
    for dir in "$root"/.muxagent/tasks/*/artifacts/*; do
      [ -d "$dir" ] || continue
      if [ -f "$dir/input.md" ] && [ ! -f "$dir/output.json" ]; then
        artifact_dir=$dir
      fi
    done
  done
  if [ -z "$artifact_dir" ]; then
    for root in $roots; do
      for dir in "$root"/.muxagent/tasks/*/artifacts/*; do
        [ -d "$dir" ] || continue
        artifact_dir=$dir
      done
    done
  fi
  IFS=$old_ifs
  if [ -n "$artifact_dir" ]; then
    run_dir=$(basename "$artifact_dir")
    node_name=${run_dir#*-}
  else
    run_dir=""
    node_name=""
  fi
}

ensure_context() {
  if [ -n "${artifact_dir:-}" ] && [ -n "${node_name:-}" ] && [ -n "${run_dir:-}" ]; then
    return
  fi
  discover_active_artifact_dir
}

load_count() {
  count=0
  count_file=""
  if [ -z "${node_name:-}" ]; then
    return
  fi
  count_file="$state_dir/${node_name}.count"
  if [ -f "$count_file" ]; then
    count=$(cat "$count_file")
  fi
}

capture_prompt_snapshot() {
  if [ -n "${artifact_dir:-}" ] && [ -n "${run_dir:-}" ] && [ -f "$artifact_dir/input.md" ]; then
    cat "$artifact_dir/input.md" > "$state_dir/${run_dir}.prompt.txt"
  fi
}

clarify_once_payload() {
  cat <<'JSON'
{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"Which path should we take?","why_it_matters":"The plan changes based on this choice.","options":[{"label":"A","description":"Option A"},{"label":"B","description":"Option B"}],"multi_select":false}]}}
JSON
}

clarify_multi_payload() {
  cat <<'JSON'
{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"Which path should we take?","why_it_matters":"The plan changes based on this choice.","options":[{"label":"A","description":"Option A"},{"label":"B","description":"Option B"}],"multi_select":false},{"question":"Which reviewer should we use?","why_it_matters":"The review style changes the prompt.","options":[{"label":"Sidekick","description":"Ship quickly"},{"label":"Strict","description":"Push harder"}],"multi_select":false}]}}
JSON
}

clarify_late_payload() {
  cat <<'JSON'
{"kind":"clarification","result":null,"clarification":{"questions":[{"question":"进入 chat_screen 时，屏幕上显示的具体状态是什么？","why_it_matters":"区分 history.complete 超时、消息事件丢失、还是 RPC 本身卡住，会直接决定后续排查方向。这段说明故意写得比较长，用来验证 clarification 表单在有 artifacts 的 detail 屏里也不会把 footer 或 artifact pane 挤没。","options":[{"label":"显示 'This session cannot be restored on this device yet.'","description":"说明 session.load 超时，history.complete 事件没到达前端，大概率是 activeSession 竞态或 WS 断连。"},{"label":"显示 'Send a message to get started' 或正常 UI 但没有消息","description":"说明 session.load 已经返回，但消息事件在处理链路中被丢弃，或 chatState 没被正确填充。"},{"label":"一直停留在 loading spinner","description":"说明 session.load RPC 自身就卡住了，可能是 Go daemon 无响应、relayws 没连上，或者 runtime 本身卡死。"},{"label":"其他表现","description":"以上都不符合，需要补充更多具体症状。"}],"multi_select":true}]}}
JSON
}

write_result_file() {
  artifact_name="$1"
  artifact_path="$artifact_dir/$artifact_name"
  printf '%s %s\n' "$node_name" "$count" > "$artifact_path"
}

write_exec_result() {
  artifact_name="$1"
  extra_json="$2"
  write_result_file "$artifact_name"
  artifact_path="$artifact_dir/$artifact_name"
  if [ -n "$extra_json" ]; then
    printf '{"kind":"result","result":{"file_paths":["%s"],%s},"clarification":null}\n' "$artifact_path" "$extra_json" > "$output"
  else
    printf '{"kind":"result","result":{"file_paths":["%s"]},"clarification":null}\n' "$artifact_path" > "$output"
  fi
}

emit_app_initialized() {
  printf '{"id":1,"result":{"userAgent":"fake","codexHome":"/tmp/.codex","platformFamily":"unix","platformOs":"linux"}}\n'
}

emit_app_thread_started() {
  printf '{"id":2,"result":{"thread":{"id":"%s","status":{"type":"idle"},"cwd":"%s"}}}\n' "$thread_id" "$PWD"
}

emit_app_turn_started() {
  printf '{"id":3,"result":{"turn":{"id":"%s","status":"inProgress","items":[],"error":null}}}\n' "$turn_id"
}

emit_app_web_search() {
  printf '{"method":"item/started","params":{"threadId":"%s","turnId":"%s","item":{"type":"webSearch","id":"ws_123","status":"inProgress","query":"","action":{"type":"other"}}}}\n' "$thread_id" "$turn_id"
  sleep 0.2
  printf '{"method":"item/completed","params":{"threadId":"%s","turnId":"%s","item":{"type":"webSearch","id":"ws_123","status":"completed","query":"latest github release announcement","action":{"type":"search","query":"latest github release announcement","queries":["latest github release announcement"]}}}}\n' "$thread_id" "$turn_id"
  sleep 0.4
}

emit_app_message_result() {
  artifact_name="$1"
  extra_json="$2"
  write_result_file "$artifact_name"
  artifact_path="$artifact_dir/$artifact_name"
  if [ -n "$extra_json" ]; then
    printf '{"method":"item/completed","params":{"threadId":"%s","turnId":"%s","item":{"type":"agentMessage","id":"msg-1","text":"{\\"kind\\":\\"result\\",\\"result\\":{\\"file_paths\\":[\\"%s\\"]%s},\\"clarification\\":null}","phase":"final_answer"}}}\n' "$thread_id" "$turn_id" "$artifact_path" "$extra_json"
  else
    printf '{"method":"item/completed","params":{"threadId":"%s","turnId":"%s","item":{"type":"agentMessage","id":"msg-1","text":"{\\"kind\\":\\"result\\",\\"result\\":{\\"file_paths\\":[\\"%s\\"]},\\"clarification\\":null}","phase":"final_answer"}}}\n' "$thread_id" "$turn_id" "$artifact_path"
  fi
}

emit_app_message_clarification_once() {
  printf '{"method":"item/completed","params":{"threadId":"%s","turnId":"%s","item":{"type":"agentMessage","id":"msg-1","text":"{\\"kind\\":\\"clarification\\",\\"result\\":null,\\"clarification\\":{\\"questions\\":[{\\"question\\":\\"Which path should we take?\\",\\"why_it_matters\\":\\"The plan changes based on this choice.\\",\\"options\\":[{\\"label\\":\\"A\\",\\"description\\":\\"Option A\\"},{\\"label\\":\\"B\\",\\"description\\":\\"Option B\\"}],\\"multi_select\\":false}]}}","phase":"final_answer"}}}\n' "$thread_id" "$turn_id"
}

emit_app_message_clarification_multi() {
  printf '{"method":"item/completed","params":{"threadId":"%s","turnId":"%s","item":{"type":"agentMessage","id":"msg-1","text":"{\\"kind\\":\\"clarification\\",\\"result\\":null,\\"clarification\\":{\\"questions\\":[{\\"question\\":\\"Which path should we take?\\",\\"why_it_matters\\":\\"The plan changes based on this choice.\\",\\"options\\":[{\\"label\\":\\"A\\",\\"description\\":\\"Option A\\"},{\\"label\\":\\"B\\",\\"description\\":\\"Option B\\"}],\\"multi_select\\":false},{\\"question\\":\\"Which reviewer should we use?\\",\\"why_it_matters\\":\\"The review style changes the prompt.\\",\\"options\\":[{\\"label\\":\\"Sidekick\\",\\"description\\":\\"Ship quickly\\"},{\\"label\\":\\"Strict\\",\\"description\\":\\"Push harder\\"}],\\"multi_select\\":false}]}}","phase":"final_answer"}}}\n' "$thread_id" "$turn_id"
}

emit_app_message_clarification_late() {
  cat <<JSON
{"method":"item/completed","params":{"threadId":"$thread_id","turnId":"$turn_id","item":{"type":"agentMessage","id":"msg-1","text":"{\\"kind\\":\\"clarification\\",\\"result\\":null,\\"clarification\\":{\\"questions\\":[{\\"question\\":\\"进入 chat_screen 时，屏幕上显示的具体状态是什么？\\",\\"why_it_matters\\":\\"区分 history.complete 超时、消息事件丢失、还是 RPC 本身卡住，会直接决定后续排查方向。这段说明故意写得比较长，用来验证 clarification 表单在有 artifacts 的 detail 屏里也不会把 footer 或 artifact pane 挤没。\\",\\"options\\":[{\\"label\\":\\"显示 'This session cannot be restored on this device yet.'\\",\\"description\\":\\"说明 session.load 超时，history.complete 事件没到达前端，大概率是 activeSession 竞态或 WS 断连。\\"},{\\"label\\":\\"显示 'Send a message to get started' 或正常 UI 但没有消息\\",\\"description\\":\\"说明 session.load 已经返回，但消息事件在处理链路中被丢弃，或 chatState 没被正确填充。\\"},{\\"label\\":\\"一直停留在 loading spinner\\",\\"description\\":\\"说明 session.load RPC 自身就卡住了，可能是 Go daemon 无响应、relayws 没连上，或者 runtime 本身卡死。\\"},{\\"label\\":\\"其他表现\\",\\"description\\":\\"以上都不符合，需要补充更多具体症状。\\"}],\\"multi_select\\":true}]}}","phase":"final_answer"}}}
JSON
}

emit_app_turn_completed() {
  printf '{"method":"turn/completed","params":{"threadId":"%s","turn":{"id":"%s","status":"completed","items":[],"error":null}}}\n' "$thread_id" "$turn_id"
}

handle_node_exec() {
  case "$node_name" in
    draft_plan)
      if [ "$flow" = "clarify-once" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
        clarify_once_payload > "$output"
        exit 0
      fi
      if [ "$flow" = "clarify-multi" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
        clarify_multi_payload > "$output"
        exit 0
      fi
      if [ "$flow" = "web-search" ] && [ "$resume_mode" -eq 0 ]; then
        echo '{"type":"item.started","item":{"id":"ws_123","type":"web_search","query":"","action":{"type":"other"}}}'
        sleep 0.2
        echo '{"type":"item.completed","item":{"id":"ws_123","type":"web_search","query":"latest github release announcement","action":{"type":"search","query":"latest github release announcement","queries":["latest github release announcement"]}}}'
        sleep 0.4
      fi
      write_exec_result "plan-${count}.md" ""
      ;;
    review_plan)
      passed=true
      if [ "$flow" = "review-reject-once" ] && [ "$count" -eq 1 ]; then
        passed=false
      fi
      write_exec_result "review-${count}.md" "\"passed\":${passed}"
      ;;
    handle_request)
      write_exec_result "result-${count}.md" ""
      ;;
    implement)
      if [ "$flow" = "clarify-late" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
        clarify_late_payload > "$output"
        exit 0
      fi
      if [ "$flow" = "implement-fail-once" ] && [ "$count" -eq 1 ]; then
        echo "simulated implement failure" >&2
        exit 1
      fi
      write_exec_result "implementation-${count}.md" ""
      ;;
    verify)
      passed=true
      if [ "$flow" = "verify-fail" ]; then
        passed=false
      elif [ "$flow" = "verify-fail-once" ] && [ "$count" -eq 1 ]; then
        passed=false
      fi
      if [ "$flow" = "yolo-replan-once" ]; then
        write_exec_result "verify-${count}.md" "\"passed\":${passed},\"summary\":\"wave ${count} complete\""
      else
        write_exec_result "verify-${count}.md" "\"passed\":${passed}"
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
      write_exec_result "evaluate-${count}.md" "\"next_node\":\"${next_node}\",\"reason\":\"${reason}\",\"next_focus\":\"${next_focus}\""
      ;;
    *)
      echo "unexpected node: $node_name" >&2
      exit 2
      ;;
  esac
}

handle_node_appserver() {
  case "$node_name" in
    draft_plan)
      if [ "$flow" = "clarify-once" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
        emit_app_message_clarification_once
      else
        if [ "$flow" = "clarify-multi" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
          emit_app_message_clarification_multi
        else
          if [ "$flow" = "web-search" ] && [ "$resume_mode" -eq 0 ]; then
            emit_app_web_search
          fi
          emit_app_message_result "plan-${count}.md" ""
        fi
      fi
      ;;
    review_plan)
      passed=true
      if [ "$flow" = "review-reject-once" ] && [ "$count" -eq 1 ]; then
        passed=false
      fi
      emit_app_message_result "review-${count}.md" ",\\\"passed\\\":${passed}"
      ;;
    handle_request)
      emit_app_message_result "result-${count}.md" ""
      ;;
    implement)
      if [ "$flow" = "clarify-late" ] && [ "$count" -eq 1 ] && [ "$resume_mode" -eq 0 ]; then
        emit_app_message_clarification_late
      else
        if [ "$flow" = "implement-fail-once" ] && [ "$count" -eq 1 ]; then
          echo "simulated implement failure" >&2
          exit 1
        fi
        emit_app_message_result "implementation-${count}.md" ""
      fi
      ;;
    verify)
      passed=true
      if [ "$flow" = "verify-fail" ]; then
        passed=false
      elif [ "$flow" = "verify-fail-once" ] && [ "$count" -eq 1 ]; then
        passed=false
      fi
      if [ "$flow" = "yolo-replan-once" ]; then
        emit_app_message_result "verify-${count}.md" ",\\\"passed\\\":${passed},\\\"summary\\\":\\\"wave ${count} complete\\\""
      else
        emit_app_message_result "verify-${count}.md" ",\\\"passed\\\":${passed}"
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
      emit_app_message_result "evaluate-${count}.md" ",\\\"next_node\\\":\\\"${next_node}\\\",\\\"reason\\\":\\\"${reason}\\\",\\\"next_focus\\\":\\\"${next_focus}\\\""
      ;;
    *)
      echo "unexpected node: $node_name" >&2
      exit 2
      ;;
  esac
  emit_app_turn_completed
}

exec_mode() {
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

  mkdir -p "$state_dir"
  printf '%s' "$prompt" > "$state_dir/${run_dir}.prompt.txt"
  load_count
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

  handle_node_exec
}

app_server_mode() {
  state_dir=${FAKE_CODEX_STATE_DIR:-"$PWD/.fake-state"}
  mkdir -p "$state_dir"

  artifact_dir=""
  run_dir=""
  node_name=""
  count=0
  count_file=""
  thread_id=""
  turn_id=""
  resume_mode=0

  ensure_context
  load_count

  while IFS= read -r line; do
    case "$line" in
      *'"method":"initialize"'*)
        emit_app_initialized
        ;;
      *'"method":"initialized"'*)
        ;;
      *'"method":"thread/resume"'*)
        ensure_context
        load_count
        resume_mode=1
        thread_id=${line#*\"threadId\":\"}
        thread_id=${thread_id%%\"*}
        if [ "$count" -le 0 ]; then
          echo "resume requested before initial thread start" >&2
          exit 2
        fi
        emit_app_thread_started
        ;;
      *'"method":"thread/start"'*)
        ensure_context
        if [ -z "$node_name" ]; then
          echo "missing artifact context for app-server thread/start" >&2
          exit 2
        fi
        load_count
        count=$((count + 1))
        printf '%s' "$count" > "$count_file"
        thread_id="thread-${node_name}-${count}"
        emit_app_thread_started
        ;;
      *'"method":"turn/start"'*)
        ensure_context
        capture_prompt_snapshot
        if [ -z "$node_name" ]; then
          echo "missing artifact context for app-server turn/start" >&2
          exit 2
        fi
        turn_id="turn-${node_name}-${count}"
        if [ "$flow" = "slow-happy" ]; then
          sleep 0.3
        fi
        emit_app_turn_started
        handle_node_appserver
        exit 0
        ;;
    esac
  done
}

if [ "${1:-}" = "app-server" ]; then
  shift
  app_server_mode "$@"
  exit 0
fi

exec_mode "$@"
