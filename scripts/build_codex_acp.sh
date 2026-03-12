#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="${1:-$WORKSPACE_ROOT/build}"
VERSION_FILE="$WORKSPACE_ROOT/internal/codexbin/version.go"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

sha256_cmd() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s\n' "sha256sum"
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    printf '%s\n' "shasum -a 256"
    return
  fi
  echo "missing required command: sha256sum or shasum" >&2
  exit 1
}

calc_sha256() {
  local file="$1"
  local cmd="$2"
  if [ "$cmd" = "sha256sum" ]; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$file" | awk '{print $1}'
}

need_cmd curl
need_cmd tar
need_cmd unzip
need_cmd mktemp
need_cmd awk
need_cmd sed
need_cmd chmod
need_cmd find

SHA256_CMD="$(sha256_cmd)"

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

version="$(sed -n 's/^const ACPVersion = "\(.*\)"$/\1/p' "$VERSION_FILE")"
if [ -z "$version" ]; then
  echo "failed to resolve ACPVersion from $VERSION_FILE" >&2
  exit 1
fi

checksum_for() {
  local target="$1"
  sed -n "s/^[[:space:]]*\"${target}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\",$/\\1/p" "$VERSION_FILE"
}

archive_ext_for() {
  local target="$1"
  if [[ "$target" == *windows* ]]; then
    printf '%s\n' ".zip"
    return
  fi
  printf '%s\n' ".tar.gz"
}

binary_name_for() {
  local output="$1"
  if [[ "$output" == *.exe ]]; then
    printf '%s\n' "codex-acp.exe"
    return
  fi
  printf '%s\n' "codex-acp"
}

extract_binary() {
  local archive="$1"
  local output_name="$2"
  local dest="$3"
  local stage="$4"
  local binary_name
  binary_name="$(binary_name_for "$output_name")"

  if [[ "$archive" == *.zip ]]; then
    unzip -q "$archive" -d "$stage"
  else
    tar -xzf "$archive" -C "$stage"
  fi

  local extracted
  extracted="$(find "$stage" -type f \( -name "$binary_name" -o -name "codex-acp" -o -name "codex-acp.exe" \) | head -n 1)"
  if [ -z "$extracted" ]; then
    echo "failed to locate $binary_name in archive for $output_name" >&2
    exit 1
  fi

  mv "$extracted" "$dest"
  chmod 755 "$dest"
}

targets=(
  "codex-acp-darwin-amd64|x86_64-apple-darwin"
  "codex-acp-darwin-arm64|aarch64-apple-darwin"
  "codex-acp-linux-amd64|x86_64-unknown-linux-gnu"
  "codex-acp-linux-amd64-musl|x86_64-unknown-linux-musl"
  "codex-acp-linux-arm64|aarch64-unknown-linux-gnu"
  "codex-acp-linux-arm64-musl|aarch64-unknown-linux-musl"
  "codex-acp-windows-amd64.exe|x86_64-pc-windows-msvc"
  "codex-acp-windows-arm64.exe|aarch64-pc-windows-msvc"
)

for entry in "${targets[@]}"; do
  output_name="${entry%%|*}"
  target="${entry##*|}"
  expected_checksum="$(checksum_for "$target")"
  if [ -z "$expected_checksum" ]; then
    echo "missing checksum for target: $target" >&2
    exit 1
  fi

  ext="$(archive_ext_for "$target")"
  archive_name="codex-acp-${version}-${target}${ext}"
  url="https://github.com/zed-industries/codex-acp/releases/download/v${version}/${archive_name}"

  archive_stage="$(stage_dir)"
  archive_path="$archive_stage/$archive_name"
  extract_stage="$(stage_dir)"

  echo "Downloading $archive_name"
  curl --fail --location --silent --show-error "$url" -o "$archive_path"

  actual_checksum="$(calc_sha256 "$archive_path" "$SHA256_CMD")"
  if [ "$actual_checksum" != "$expected_checksum" ]; then
    echo "checksum mismatch for $archive_name: expected $expected_checksum got $actual_checksum" >&2
    exit 1
  fi

  extract_binary "$archive_path" "$output_name" "$OUT_DIR/$output_name" "$extract_stage"
done
