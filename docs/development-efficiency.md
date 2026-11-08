# 开发效率

共享 `main` 时怎么构建、查索引、迭代素材，以及 AI 草稿代码的边界。行为以对应文件为准，这里不抄定义，也不写没有实测过的耗时。

## 实施计划与协作

- 构建：本地应用代码降低优化等级，发布配置保持不变；记录锁等待、编译、打包、签名与安装耗时。
- 定位：增量更新代码索引，补全主要状态类型的私有方法与跨文件模块关系。
- 素材：直接启动现有二进制读取磁盘素材，省去素材迭代中的编译。
- 状态：先将 AI 草稿恢复判断抽成纯逻辑；暂不全仓搬目录或拆 crate。
- 共享 main 上按文件分工，保留其他任务改动，由协调方在收尾时统一构建一次。

## 构建

- 轻量检查是 `bun run check`。
- 本地安装构建使用 `Cargo.toml` 的 `[profile.fast]`。`bun run build` 会安装应用；profile 选择在 `scripts/build-app.mjs`（安装默认 fast，可用 `BLOCK_PROFILE` 覆盖）。`build:ci` 不安装，走 release。
- 耗时记录在 `dist/build-timing.json` 和控制台 `BUILD_TIMING`；`BLOCK_CARGO_TIMINGS=1 bun run build` 可附带 Cargo 明细。首次切换优化等级会重编应用，不能据此推算稳定增量构建的提速。

## 开发资源

首次需要产物时运行 `bun run build`。这次会编译。

之后只有素材变化时，重新运行 `bun run dev:assets`。这不是热更新：已经打开的进程不会自己换素材，先退出，再运行一次。

`dev:assets` 执行 `scripts/dev-assets.sh`，直接启动已构建的本地二进制，不调用 Cargo，所以 `include_bytes!` 的素材变化不会触发编译。

脚本会设置：

- `BLOCK_DEV_ASSETS=1`
- `BLOCK_DEV_ASSETS_ROOT` 为当前仓库的绝对路径

默认选择 fast profile，`BLOCK_PROFILE` 可覆盖（dev 对应 debug 目录）。未设置环境变量时查找 `<仓库>/target/fast/block`。`CARGO_TARGET_DIR` 换成别的目标目录；`CARGO_BUILD_TARGET` 再在中间加一层 triple 目录。脚本不读取 `.cargo/config.toml`。找不到文件就提示先运行 `bun run build`，并以失败退出。

`bun run dev` 和 `bun run build` 不设置这两个变量，发布路径不变。

## 索引

- 更新地图：`python3 scripts/gen_ai_map.py` 或 `bun run ai:map`。
- 查询：`python3 scripts/gen_ai_map.py -q 关键词`。查询先比较 `src/**/*.rs` 的新增、删除和修改，只重读变过的文件，再写回 `.ai-map.md`。
- 强制全读：`--rebuild`。
- 缓存是 `tmp/ai-map.cache.json`（目录已在 `.gitignore`）。可以删掉，下次查询会重建。不要手改 `.ai-map.md`。
- 除了原来的公开签名和 smoke probe，还可以查到 `Editor`、`AiChat`、`Workspace` 的 impl 分布、这些 impl 里的私有方法，以及 `#[path]` 解析后的源文件（文件不存在会标出 missing）。
- 查询打印有条数上限。完整列表在 `.ai-map.md`。

## AI 草稿

失败发送是放回输入框、记下 retry，还是放进延后队列，这些判断在 `src/ai/chat_draft_state.rs`。写盘、改输入框，以及 `AiChat` 上原来的方法名，仍在 `src/ai/chat_drafts.rs`。

会话标签上的草稿仍是 `AiChat.tab_drafts`，由 `src/ai/chat_composer.rs` 保存和切换。这次没有搬过去。

`ai-active-draft.json` 和 `ai-failed-drafts.json` 的字段不变。启动时仍只恢复当时会话列表里存在的会话，以及属于该会话的 retry。

## 收尾

索引、草稿和这份说明不单独编译。合并进树之后，由协调方跑一次 `bun run build`。
