#!/usr/bin/env bash
set -euo pipefail
CGO_ENABLED=0 go vet ./... "$@"
python3 scripts/lint_design.py
python3 scripts/lint_plugin_ids.py
if command -v bun >/dev/null 2>&1; then
  bun scripts/sync_plugin_docs.mjs --check
else
  node scripts/sync_plugin_docs.mjs --check
fi
