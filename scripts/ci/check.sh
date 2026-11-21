#!/usr/bin/env bash
# CI 与自动修复共用的检查：设计 lint、三平台 vet/build、go test。
set -euo pipefail

out_dir="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/ci-bins"
mkdir -p "$out_dir"

python3 scripts/lint_design.py &
pid_lint=$!

echo "==> Checking linux/amd64"
go vet ./...
go build -o "$out_dir/" ./cmd/...

echo "==> Checking windows/amd64, darwin/arm64 & darwin/amd64 in parallel"
(
  GOOS=windows GOARCH=amd64 go vet ./...
  GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/block
) &
pid_win=$!
(
  GOOS=darwin GOARCH=arm64 go vet ./...
  GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/block
  GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/block
) &
pid_mac=$!

wait "$pid_win"
wait "$pid_mac"
wait "$pid_lint"
echo "==> Running go test ./..."
go test ./...
