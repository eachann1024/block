#!/usr/bin/env bash
set -euo pipefail

min_major="${1:-22}"

node_major() {
  "$1" -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0
}

if command -v node >/dev/null 2>&1; then
  cur="$(node_major "$(command -v node)")"
  if [ "$cur" -ge "$min_major" ]; then
    echo "Using system Node $(node -v)"
    exit 0
  fi
fi

if [ -n "${RUNNER_TOOL_CACHE:-}" ] && [ -d "$RUNNER_TOOL_CACHE/node" ]; then
  cand="$(ls -d "$RUNNER_TOOL_CACHE"/node/"${min_major}".*/*/bin/node 2>/dev/null | sort -V | tail -n1 || true)"
  if [ -n "$cand" ] && [ -x "$cand" ]; then
    bin_dir="$(dirname "$cand")"
    if [ -n "${GITHUB_PATH:-}" ]; then
      echo "$bin_dir" >> "$GITHUB_PATH"
    fi
    export PATH="$bin_dir:$PATH"
    echo "Using cached Node $("$cand" -v) at $bin_dir"
    exit 0
  fi
fi

if command -v node >/dev/null 2>&1; then
  cur="$(node_major "$(command -v node)")"
  if [ "$cur" -ge 20 ]; then
    echo "Using fallback Node $(node -v)"
    exit 0
  fi
fi

echo "Node.js >= 20 not found on runner" >&2
exit 1
