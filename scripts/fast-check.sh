#!/usr/bin/env bash
# Stay on Cargo's default dev profile. Do not pass --profile fast.
# `cargo check` skips code generation, so the fast opt-level does not apply.
# fast inherits release (panic=abort, debug-assertions off) and its check units
# would be a separate target/fast metadata compile, not the install build.
set -euo pipefail
cargo check --quiet --message-format=short "$@"
