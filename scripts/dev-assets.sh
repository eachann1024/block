#!/usr/bin/env bash
# Launch the already-built local binary. Does not call Cargo.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
target_dir="${CARGO_TARGET_DIR:-$root/target}"
if [[ "$target_dir" != /* ]]; then
  target_dir="$root/$target_dir"
fi

profile="${BLOCK_PROFILE:-fast}"
profile_dir="$profile"
if [[ "$profile" == dev ]]; then profile_dir=debug; fi
bin="$target_dir/$profile_dir/block"
if [[ -n "${CARGO_BUILD_TARGET:-}" ]]; then
  bin="$target_dir/${CARGO_BUILD_TARGET}/$profile_dir/block"
fi

if [[ ! -f "$bin" ]]; then
  echo "未找到 $profile 二进制：$bin" >&2
  echo "请先在仓库根目录运行 bun run build。dev:assets 只启动已构建的 block，不调用 Cargo。" >&2
  echo "默认查找 <仓库>/target/fast/block。BLOCK_PROFILE、CARGO_TARGET_DIR 与 CARGO_BUILD_TARGET 会改变这一路径；不读取 .cargo/config.toml。" >&2
  exit 1
fi

export BLOCK_DEV_ASSETS=1
export BLOCK_DEV_ASSETS_ROOT="$root"
exec "$bin" "$@"
