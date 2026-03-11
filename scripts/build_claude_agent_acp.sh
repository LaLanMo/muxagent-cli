#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ACP_DIR="$WORKSPACE_ROOT/claude-agent-acp"
OUT_DIR="${1:-$WORKSPACE_ROOT/release}"

if ! command -v bun >/dev/null 2>&1; then
  echo "bun is required to build claude-agent-acp release assets" >&2
  exit 1
fi

if [ ! -d "$ACP_DIR" ]; then
  echo "claude-agent-acp source directory not found: $ACP_DIR" >&2
  exit 1
fi

if [ ! -d "$ACP_DIR/node_modules" ]; then
  echo "missing dependencies in $ACP_DIR; run npm ci or bun install first" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

build_target() {
  local bun_target="$1"
  local asset_name="$2"

  echo "building $asset_name ($bun_target)"
  bun build --compile --minify \
    --no-compile-autoload-dotenv \
    --no-compile-autoload-bunfig \
    --target="$bun_target" \
    "$ACP_DIR/src/index.ts" \
    --outfile "$OUT_DIR/$asset_name"
}

build_target "bun-darwin-arm64" "claude-agent-acp-darwin-arm64"
build_target "bun-darwin-x64" "claude-agent-acp-darwin-x64"
build_target "bun-linux-x64" "claude-agent-acp-linux-x64"
build_target "bun-linux-arm64" "claude-agent-acp-linux-arm64"
build_target "bun-linux-x64-musl" "claude-agent-acp-linux-x64-musl"
build_target "bun-linux-arm64-musl" "claude-agent-acp-linux-arm64-musl"
build_target "bun-windows-x64" "claude-agent-acp-windows-x64.exe"
build_target "bun-windows-arm64" "claude-agent-acp-windows-arm64.exe"
