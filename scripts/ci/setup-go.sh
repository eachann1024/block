#!/usr/bin/env bash
set -euo pipefail

ver="${1:-}"
if [ -z "$ver" ]; then
  ver="$(awk '/^go[[:space:]]+[0-9]+/ { print $2; exit }' go.mod)"
fi
if [ -z "$ver" ]; then
  echo "Unable to determine Go version from go.mod" >&2
  exit 1
fi

version_ge() {
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

if command -v go >/dev/null 2>&1; then
  cur="$(GOTOOLCHAIN=local go env GOVERSION 2>/dev/null | sed 's/^go//' || true)"
  if [ -n "$cur" ] && version_ge "$cur" "$ver"; then
    echo "Using preinstalled Go ${cur} (requires >= ${ver})"
    if [ -n "${GITHUB_ENV:-}" ]; then
      echo "GOTOOLCHAIN=local" >> "$GITHUB_ENV"
      if [ -n "${RUNNER_TEMP:-}" ]; then
        echo "GOMODCACHE=${RUNNER_TEMP}/go-mod" >> "$GITHUB_ENV"
        echo "GOCACHE=${RUNNER_TEMP}/go-build" >> "$GITHUB_ENV"
      fi
    fi
    exit 0
  fi
fi

# Resolve 2-part version (e.g. 1.27) to latest patch release from go.dev.
if [[ "$ver" =~ ^[0-9]+\.[0-9]+$ ]]; then
  resolved="$(curl -fsSL --retry 3 'https://go.dev/dl/?mode=json' | python3 -c '
import json, sys
prefix = "go" + sys.argv[1] + "."
for item in json.load(sys.stdin):
    v = item.get("version", "")
    if v.startswith(prefix):
        print(v[2:])
        break
' "$ver")"
  if [ -z "$resolved" ]; then
    echo "Failed to resolve latest Go ${ver}.x release" >&2
    exit 1
  fi
  ver="$resolved"
fi

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) echo "Unsupported OS for setup-go.sh: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "Unsupported arch for setup-go.sh: $(uname -m)" >&2; exit 1 ;;
esac

dest="${RUNNER_TEMP:-/tmp}/go-toolchain-${ver}"
if [ ! -x "$dest/go/bin/go" ]; then
  rm -rf "$dest"
  mkdir -p "$dest"
  url="https://dl.google.com/go/go${ver}.${os}-${arch}.tar.gz"
  echo "Streaming ${url} -> ${dest}"
  curl -fsSL --retry 3 --retry-delay 1 "$url" | tar -xz -C "$dest"
fi

if [ -n "${GITHUB_PATH:-}" ]; then
  echo "$dest/go/bin" >> "$GITHUB_PATH"
fi
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "GOTOOLCHAIN=local" >> "$GITHUB_ENV"
  if [ -n "${RUNNER_TEMP:-}" ]; then
    echo "GOMODCACHE=${RUNNER_TEMP}/go-mod" >> "$GITHUB_ENV"
    echo "GOCACHE=${RUNNER_TEMP}/go-build" >> "$GITHUB_ENV"
  fi
fi
export PATH="$dest/go/bin:$PATH"
export GOTOOLCHAIN=local
go version
