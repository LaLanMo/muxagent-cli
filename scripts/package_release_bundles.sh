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
  local runtime_src="$3"
  local stage

  need_file "$BUILD_DIR/$cli_src"
  need_file "$BUILD_DIR/$runtime_src"

  stage="$(stage_dir)"
  cp "$BUILD_DIR/$cli_src" "$stage/muxagent"
  cp "$BUILD_DIR/$runtime_src" "$stage/claude-agent-acp"
  chmod 755 "$stage/muxagent" "$stage/claude-agent-acp"

  tar -C "$stage" -czf "$OUT_DIR/$bundle_name" muxagent claude-agent-acp
}

package_zip_bundle() {
  local bundle_name="$1"
  local cli_src="$2"
  local runtime_src="$3"
  local stage

  need_file "$BUILD_DIR/$cli_src"
  need_file "$BUILD_DIR/$runtime_src"

  stage="$(stage_dir)"
  cp "$BUILD_DIR/$cli_src" "$stage/muxagent.exe"
  cp "$BUILD_DIR/$runtime_src" "$stage/claude-agent-acp.exe"
  chmod 755 "$stage/muxagent.exe" "$stage/claude-agent-acp.exe"

  (
    cd "$stage"
    zip -q "$OUT_DIR/$bundle_name" muxagent.exe claude-agent-acp.exe
  )
}

package_tar_bundle "muxagent-darwin-amd64.tar.gz" "muxagent-darwin-amd64" "claude-agent-acp-darwin-x64"
package_tar_bundle "muxagent-darwin-arm64.tar.gz" "muxagent-darwin-arm64" "claude-agent-acp-darwin-arm64"
package_tar_bundle "muxagent-linux-amd64.tar.gz" "muxagent-linux-amd64" "claude-agent-acp-linux-x64"
package_tar_bundle "muxagent-linux-amd64-musl.tar.gz" "muxagent-linux-amd64" "claude-agent-acp-linux-x64-musl"
package_tar_bundle "muxagent-linux-arm64.tar.gz" "muxagent-linux-arm64" "claude-agent-acp-linux-arm64"
package_tar_bundle "muxagent-linux-arm64-musl.tar.gz" "muxagent-linux-arm64" "claude-agent-acp-linux-arm64-musl"
package_zip_bundle "muxagent-windows-amd64.zip" "muxagent-windows-amd64.exe" "claude-agent-acp-windows-x64.exe"
package_zip_bundle "muxagent-windows-arm64.zip" "muxagent-windows-arm64.exe" "claude-agent-acp-windows-arm64.exe"
