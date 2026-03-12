#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="${1:-$WORKSPACE_ROOT/build}"
OUT_DIR="${2:-$WORKSPACE_ROOT/release}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_file() {
  if [ ! -f "$1" ]; then
    echo "missing required build artifact: $1" >&2
    exit 1
  fi
}

need_cmd tar
need_cmd zip
need_cmd mktemp
need_cmd cp
need_cmd chmod

mkdir -p "$OUT_DIR"

cleanup_paths=()
cleanup() {
  local path
  for path in "${cleanup_paths[@]:-}"; do
    rm -rf "$path"
  done
}
trap cleanup EXIT INT TERM

stage_dir() {
  local dir
  dir="$(mktemp -d)"
  cleanup_paths+=("$dir")
  printf '%s\n' "$dir"
}

package_tar_bundle() {
  local bundle_name="$1"
  local cli_src="$2"
  local claude_src="$3"
  local codex_src="$4"
  local stage

  need_file "$BUILD_DIR/$cli_src"
  need_file "$BUILD_DIR/$claude_src"
  need_file "$BUILD_DIR/$codex_src"

  stage="$(stage_dir)"
  cp "$BUILD_DIR/$cli_src" "$stage/muxagent"
  cp "$BUILD_DIR/$claude_src" "$stage/claude-agent-acp"
  cp "$BUILD_DIR/$codex_src" "$stage/codex-acp"
  chmod 755 "$stage/muxagent" "$stage/claude-agent-acp" "$stage/codex-acp"

  tar -C "$stage" -czf "$OUT_DIR/$bundle_name" muxagent claude-agent-acp codex-acp
}

package_zip_bundle() {
  local bundle_name="$1"
  local cli_src="$2"
  local claude_src="$3"
  local codex_src="$4"
  local stage

  need_file "$BUILD_DIR/$cli_src"
  need_file "$BUILD_DIR/$claude_src"
  need_file "$BUILD_DIR/$codex_src"

  stage="$(stage_dir)"
  cp "$BUILD_DIR/$cli_src" "$stage/muxagent.exe"
  cp "$BUILD_DIR/$claude_src" "$stage/claude-agent-acp.exe"
  cp "$BUILD_DIR/$codex_src" "$stage/codex-acp.exe"
  chmod 755 "$stage/muxagent.exe" "$stage/claude-agent-acp.exe" "$stage/codex-acp.exe"

  (
    cd "$stage"
    zip -q "$OUT_DIR/$bundle_name" muxagent.exe claude-agent-acp.exe codex-acp.exe
  )
}

package_tar_bundle "muxagent-darwin-amd64.tar.gz" "muxagent-darwin-amd64" "claude-agent-acp-darwin-x64" "codex-acp-darwin-amd64"
package_tar_bundle "muxagent-darwin-arm64.tar.gz" "muxagent-darwin-arm64" "claude-agent-acp-darwin-arm64" "codex-acp-darwin-arm64"
package_tar_bundle "muxagent-linux-amd64.tar.gz" "muxagent-linux-amd64" "claude-agent-acp-linux-x64" "codex-acp-linux-amd64"
package_tar_bundle "muxagent-linux-amd64-musl.tar.gz" "muxagent-linux-amd64" "claude-agent-acp-linux-x64-musl" "codex-acp-linux-amd64-musl"
package_tar_bundle "muxagent-linux-arm64.tar.gz" "muxagent-linux-arm64" "claude-agent-acp-linux-arm64" "codex-acp-linux-arm64"
package_tar_bundle "muxagent-linux-arm64-musl.tar.gz" "muxagent-linux-arm64" "claude-agent-acp-linux-arm64-musl" "codex-acp-linux-arm64-musl"
package_zip_bundle "muxagent-windows-amd64.zip" "muxagent-windows-amd64.exe" "claude-agent-acp-windows-x64.exe" "codex-acp-windows-amd64.exe"
package_zip_bundle "muxagent-windows-arm64.zip" "muxagent-windows-arm64.exe" "claude-agent-acp-windows-arm64.exe" "codex-acp-windows-arm64.exe"
