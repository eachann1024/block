# Block 文档中心

> 本地优先的 Markdown 块笔记软件 · Ocean Paper 设计语言 · 开放插件生态

本目录是 **Block** 项目的唯一文档总源（Single Source of Truth），同时服务于 GitHub 仓库阅读与在线文档站 [docs.block.cooool.top](https://docs.block.cooool.top/)（推送 `docs/**` 后由 VitePress 自动构建发布）。

---

## 文档导航

### 核心规范
- **[设计语言与配色规范（DESIGN.md）](../DESIGN.md)**：Ocean Paper 色板、字号与几何尺度、全局 Pluck Switch 与 Scheme A 双色图标体系。
- **[项目主介绍（README.md）](../README.md)** · **[English README](../README.en.md)**：产品特性、下载入口与开源许可说明。

### 插件开发文档（`docs/plugins/`）
适合开发者在 GitHub 上按章节浏览，与官网扩展开发页保持同源同步：

| 章节 | 文件 | 说明 |
| --- | --- | --- |
| 1. 架构与硬边界 | [plugins/README.md](plugins/README.md) | 独立进程模型、超时与体积上限、设置页挂载、商店分类与信任等级、强制校验规则 |
| 2. 清单与权限 | [plugins/manifest.md](plugins/manifest.md) | 目录结构、`manifest.json` 字段约束、5 类敏感权限与 `permission_reasons` 自定义缘由 |
| 3. 协议、UI 与骨架 | [plugins/protocol-and-ui.md](plugins/protocol-and-ui.md) | 双向 JSON-RPC 2.0、`host.*` 方法、15 种 `UIPage` 控件与 Python 标准库骨架 |
| 4. 检查与上架 | [plugins/check-and-store.md](plugins/check-and-store.md) | `block-plugin-check` 本地协议检查、三层双语门禁与 `blockmd-plugin` 商店自动采集 |
| 5. 宿主界面扩展 | [plugins/ui-contributions.md](plugins/ui-contributions.md) | 顶栏按钮、内置设置分区、命令菜单、侧边面板、状态栏、弹窗与编辑器/外观宿主方法 |
| 6. 语言扩展 | [plugins/languages.md](plugins/languages.md) | 零子进程、零权限声明式语法高亮扩展（`contributes.languages` + `grammars/*.json`） |

### AI 自包含完整单文件
- **[plugin-dev-for-ai.md](plugin-dev-for-ai.md)**：由 `bun run docs:sync` 从上述 `docs/plugins/*.md` 自动拼装生成的完整单文件规范，适合一键复制给 AI 工具从零构建插件。请勿只改此文件，修改源章节后运行 `bun run docs:sync` 即可保持严格同步。
