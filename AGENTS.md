# Block

## 目录结构

- `cmd/block`：程序入口与 CLI
- `internal/app`：工作区、顶栏、侧栏、窗口与快捷键路由
- `internal/editor`：块级编辑器、选区、滚动、表格与批注
- `internal/ui`：通用 UI 控件、弹窗、Tooltip 与快捷键解析（`shortcuts.go`）
- `internal/theme`：Ocean Paper 色板与尺度常量
- `internal/files`：数据目录（`datadir.go`）、状态读写、扫描与搜索
- `internal/ai`：AI 面板、会话存储与修改计划
- 其余业务包：`markdown`、`export`、`modals`、`plugins`、`model`、`codepreview`、`icons`、`smoke`、`updater`

## 常用命令

- 检查：`bun run check`（`go vet` + `scripts/lint_design.py`）
- 构建：`bun run build`（仅构建不安装：`bun run build:ci`）
- 测试：`bun run test`
- 符号索引：`python3 scripts/gen_ai_map.py -q <关键词>`（查 `.ai-map.md`，接口大改后用 `bun run ai:map` 更新）
- 运行态排查：`bun run ai:state`（查看当前打开笔记与最近 AI 修改计划）

## 检索与效率

- **先定位再精读**：先用 `gen_ai_map.py -q` 或 `grep` 拿行号，再按行范围（120–200 行）读取；超过 300 行的文件不整份读取。
- **并发与早停**：互不依赖的搜索与读取同轮并发；证据足够定因或动手即停止探索，不顺手泛读无关文件。
- **依赖边界**：排查默认留在 `cmd/` 和 `internal/`；不主动翻阅 `go/pkg/mod`源码，仅在业务层无法解释时按精确符号查目标函数。

## 核心约定

- 视觉与设计 token 以 `internal/theme/`（`tokens.go`、`theme.go`、`elevation.go`）和 `DESIGN.md` 为准，禁止在 UI 业务代码里写裸颜色/圆角/字号/字重/阴影（由 `bun run check` 校验）；编辑区仅标准宽度，块把手仅在悬停或光标所在块旁显示。
- 通用 UI 组件优先复用或抽入 `internal/ui/`，不在业务包重复实现。
- 快捷键走 `internal/ui/shortcuts.go` 与 `modals.EffectiveShortcut`，不抢占输入框编辑与输入法；保留设置页禁止打开 AI 等既有业务限制。
