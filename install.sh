#!/usr/bin/env sh
set -eu

REPO="${MUXAGENT_REPO:-LaLanMo/muxagent-cli}"
API_URL="${MUXAGENT_RELEASE_API_URL:-https://api.github.com/repos/$REPO/releases}"
BASE_URL="${MUXAGENT_RELEASE_BASE_URL:-https://github.com/$REPO/releases/download}"
VERSION="${MUXAGENT_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

detect_goos() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    *)
      echo "unsupported operating system: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_goarch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

detect_runtime_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "x64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

is_musl() {
  if [ "$(detect_goos)" != "linux" ]; then
    return 1
  fi

  if command -v ldd >/dev/null 2>&1 && ldd --version 2>&1 | grep -qi musl; then
    return 0
  fi

  for path in /lib/ld-musl-*; do
    if [ -e "$path" ]; then
      return 0
    fi
  done

  return 1
}

runtime_platform() {
  os="$(detect_goos)"
  arch="$(detect_runtime_arch)"

  case "$os" in
    darwin)
      echo "$os-$arch"
      ;;
    linux)
      if is_musl; then
        echo "$os-$arch-musl"
      else
        echo "$os-$arch"
      fi
      ;;
  esac
}

normalize_version() {
  raw="$1"
  case "$raw" in
    latest) echo "latest" ;;
    v*) echo "$raw" ;;
    *) echo "v$raw" ;;
  esac
}

fetch_latest_tag() {
  need_cmd curl
  body="$(curl -fsSL "$API_URL/latest")"
  tag="$(printf '%s' "$body" | tr -d '\n' | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
  if [ -z "$tag" ]; then
    echo "failed to resolve latest release tag" >&2
    exit 1
  fi
  printf '%s\n' "$tag"
}

choose_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    printf '%s\n' "$INSTALL_DIR"
    return
  fi

  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    printf '%s\n' /usr/local/bin
    return
  fi

  printf '%s\n' "$HOME/.local/bin"
}

download_asset() {
  url="$1"
  dest="$2"
  if ! curl -fsSL --retry 3 --connect-timeout 15 "$url" -o "$dest"; then
    return 1
  fi
}

need_cmd uname
need_cmd mktemp
need_cmd chmod
need_cmd mv
need_cmd mkdir
need_cmd grep
need_cmd sed
need_cmd tr

TAG="$(normalize_version "$VERSION")"
if [ "$TAG" = "latest" ]; then
  TAG="$(fetch_latest_tag)"
fi

GOOS="$(detect_goos)"
GOARCH="$(detect_goarch)"
CLI_ASSET="muxagent-$GOOS-$GOARCH"
RUNTIME_ASSET="claude-agent-acp-$(runtime_platform)"
TARGET_DIR="$(choose_install_dir)"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$TARGET_DIR"

echo "Installing muxagent $TAG to $TARGET_DIR"

CLI_TMP="$TMP_DIR/muxagent"
if ! download_asset "$BASE_URL/$TAG/$CLI_ASSET" "$CLI_TMP"; then
  echo "failed to download $CLI_ASSET from $BASE_URL/$TAG/" >&2
  exit 1
fi
chmod 755 "$CLI_TMP"
mv "$CLI_TMP" "$TARGET_DIR/muxagent"

RUNTIME_TMP="$TMP_DIR/claude-agent-acp"
if download_asset "$BASE_URL/$TAG/$RUNTIME_ASSET" "$RUNTIME_TMP"; then
  chmod 755 "$RUNTIME_TMP"
  mv "$RUNTIME_TMP" "$TARGET_DIR/claude-agent-acp"
else
  echo "warning: bundled Claude runtime not found for $RUNTIME_ASSET; muxagent will prepare it when needed" >&2
fi

case ":$PATH:" in
  *":$TARGET_DIR:"*) ;;
  *)
    echo "note: $TARGET_DIR is not in PATH" >&2
    ;;
esac

echo "Installed muxagent to $TARGET_DIR/muxagent"
echo "Next:"
echo "  1. Download the MuxAgent mobile app"
echo "  2. Run: muxagent daemon start"
