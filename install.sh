#!/usr/bin/env sh
set -eu

REPO="${MUXAGENT_REPO:-LaLanMo/muxagent-cli}"
BASE_URL="${MUXAGENT_RELEASE_BASE_URL:-https://github.com/$REPO/releases/download}"
LATEST_BASE_URL="${MUXAGENT_RELEASE_LATEST_BASE_URL:-https://github.com/$REPO/releases/latest/download}"
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

normalize_version() {
  raw="$1"
  case "$raw" in
    latest) echo "latest" ;;
    v*) echo "$raw" ;;
    *) echo "v$raw" ;;
  esac
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
need_cmd tar

TAG="$(normalize_version "$VERSION")"
GOOS="$(detect_goos)"
GOARCH="$(detect_goarch)"
ASSET_SUFFIX=""
if [ "$GOOS" = "linux" ] && is_musl; then
  ASSET_SUFFIX="-musl"
fi
BUNDLE_ASSET="muxagent-$GOOS-$GOARCH$ASSET_SUFFIX.tar.gz"
TARGET_DIR="$(choose_install_dir)"
TMP_DIR="$(mktemp -d)"
DOWNLOAD_BASE="$BASE_URL/$TAG"
DISPLAY_VERSION="$TAG"

if [ "$TAG" = "latest" ]; then
  DOWNLOAD_BASE="$LATEST_BASE_URL"
  DISPLAY_VERSION="latest"
fi

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$TARGET_DIR"

echo "Installing muxagent $DISPLAY_VERSION to $TARGET_DIR"

BUNDLE_TMP="$TMP_DIR/muxagent.tar.gz"
if ! download_asset "$DOWNLOAD_BASE/$BUNDLE_ASSET" "$BUNDLE_TMP"; then
  echo "failed to download $BUNDLE_ASSET from $DOWNLOAD_BASE/" >&2
  exit 1
fi

EXTRACT_DIR="$TMP_DIR/extract"
mkdir -p "$EXTRACT_DIR"
if ! tar -xzf "$BUNDLE_TMP" -C "$EXTRACT_DIR"; then
  echo "failed to extract $BUNDLE_ASSET" >&2
  exit 1
fi

if [ ! -f "$EXTRACT_DIR/muxagent" ]; then
  echo "bundle missing muxagent executable" >&2
  exit 1
fi
if [ ! -f "$EXTRACT_DIR/claude-agent-acp" ]; then
  echo "bundle missing claude-agent-acp executable" >&2
  exit 1
fi
if [ ! -f "$EXTRACT_DIR/codex-acp" ]; then
  echo "bundle missing codex-acp executable" >&2
  exit 1
fi

chmod 755 "$EXTRACT_DIR/muxagent" "$EXTRACT_DIR/claude-agent-acp" "$EXTRACT_DIR/codex-acp"
mv "$EXTRACT_DIR/muxagent" "$TARGET_DIR/muxagent"
mv "$EXTRACT_DIR/claude-agent-acp" "$TARGET_DIR/claude-agent-acp"
mv "$EXTRACT_DIR/codex-acp" "$TARGET_DIR/codex-acp"

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
