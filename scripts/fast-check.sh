#!/usr/bin/env bash
set -euo pipefail
CGO_ENABLED=0 go vet ./... "$@"
python3 scripts/lint_design.py
