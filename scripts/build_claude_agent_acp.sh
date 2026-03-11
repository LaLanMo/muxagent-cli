#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="${1:-$WORKSPACE_ROOT/build}"
VERSION_FILE="$WORKSPACE_ROOT/internal/acpbin/version.go"

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
  local platform="$1"
  sed -n "s/^[[:space:]]*\"${platform}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\",$/\\1/p" "$VERSION_FILE"
}

platforms=(
  "darwin-arm64"
  "darwin-x64"
  "linux-x64"
  "linux-arm64"
  "linux-x64-musl"
  "linux-arm64-musl"
  "windows-x64"
  "windows-arm64"
)

if [ -n "${DOWNLOAD_PLATFORMS:-}" ]; then
  read -r -a platforms <<<"$DOWNLOAD_PLATFORMS"
fi

archive_ext_for() {
  local platform="$1"
  if [[ "$platform" == linux-* ]]; then
    printf '%s\n' ".tar.gz"
    return
  fi
  printf '%s\n' ".zip"
}

output_name_for() {
  local platform="$1"
  local name="claude-agent-acp-$platform"
  if [[ "$platform" == windows-* ]]; then
    name="${name}.exe"
  fi
  printf '%s\n' "$name"
}

extract_binary() {
  local archive="$1"
  local platform="$2"
  local dest="$3"
  local stage="$4"
  local binary_name="claude-agent-acp"

  if [[ "$platform" == windows-* ]]; then
    binary_name="${binary_name}.exe"
    unzip -q "$archive" -d "$stage"
  else
    tar -xzf "$archive" -C "$stage"
  fi

  local extracted
  extracted="$(find "$stage" -type f \( -name "$binary_name" -o -name "claude-agent-acp" -o -name "claude-agent-acp.exe" \) | head -n 1)"
  if [ -z "$extracted" ]; then
    echo "failed to locate $binary_name in archive for $platform" >&2
    exit 1
  fi

  mv "$extracted" "$dest"
  chmod 755 "$dest"
}

for platform in "${platforms[@]}"; do
  expected_checksum="$(checksum_for "$platform")"
  if [ -z "$expected_checksum" ]; then
    echo "missing checksum for platform: $platform" >&2
    exit 1
  fi

  ext="$(archive_ext_for "$platform")"
  archive_name="claude-agent-acp-${platform}${ext}"
  output_name="$(output_name_for "$platform")"
  url="https://github.com/zed-industries/claude-agent-acp/releases/download/v${version}/${archive_name}"

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

  extract_binary "$archive_path" "$platform" "$OUT_DIR/$output_name" "$extract_stage"
done
