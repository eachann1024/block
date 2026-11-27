# Block 文档中心

> 本地优先的 Markdown 块笔记软件 · Ocean Paper 设计语言 · 开放插件生态

本目录是 **Block** 项目的唯一文档总源（Single Source of Truth），同时服务于 GitHub 仓库阅读与在线文档站 [docs.block.cooool.top](https://docs.block.cooool.top/)（推送 `docs/**` 后由 VitePress 自动构建发布；英文站点见 [English Documentation](en/README.md)）。

---

## 文档导航

### 核心规范
- **[设计语言与配色规范（DESIGN.md）](../DESIGN.md)**：Ocean Paper 色板、字号与几何尺度、全局 Pluck Switch 与 Scheme A 双色图标体系。
- **[项目主介绍（README.md）](../README.md)** · **[English README](../README.en.md)**：产品特性、下载入口与开源许可说明。
- **[隐私政策](https://block.cooool.top/privacy/)**

### 插件开发文档（`docs/plugins/`）
外部插件独立建仓（添加 `blockmd-plugin` Topic；商店采集优先取最新正式 Release，无可用 Release 时回退到固定提交 SHA 的默认分支源码包），无需向主仓提 PR 即可参与商店自动采集（抓取 stars 降序前 100 仓库，桌面端也可直接通过本地目录或仓库名安装）；与官网扩展开发页保持同源同步：

| 章节 | 文件 | 说明 |
| --- | --- | --- |
| 1. 开发前必读与 AI 指南 | [plugins/README.md](plugins/README.md) | 无需提 PR 上架边界、三类开发路径、8 条必避红线、开发与验收步骤、沙盒硬边界与旧版运行时迁移 |
| 2. 清单与权限 | [plugins/manifest.md](plugins/manifest.md) | 目录结构、`manifest.json` 必填与白名单字段、30 项有效权限（5 类 15 项敏感权限 + 3 项静默事件 + 12 项默认放行）与 `permission_reasons` |
| 3. 协议、UI 与骨架 | [plugins/protocol-and-ui.md](plugins/protocol-and-ui.md) | 双向 JSON-RPC 2.0、[宿主方法总表](plugins/protocol-and-ui.md#宿主方法总表)、16 种 `UIItem` 控件、`block-sdk.js` 双语沙盒骨架与平台实现说明 |
| 4. 宿主界面扩展 | [plugins/ui-contributions.md](plugins/ui-contributions.md) | 多表面模型、`manifest.contributes` 字段、`plugin.command`/工作区事件与通用宿主界面方法 |
| 5. 语言扩展 | [plugins/languages.md](plugins/languages.md) | 零子进程、零权限声明式语法高亮扩展（`contributes.languages` + `grammars/*.json`）与宿主文件关联参考 |
| 6. 检查与上架 | [plugins/check-and-store.md](plugins/check-and-store.md) | `block-plugin-check` 命令与覆盖边界、双语门禁、`blockmd-plugin` 采集规则（前 100 上限）与离线兜底索引机制 |

### AI 自包含完整单文件
- **[plugin-dev-for-ai.md](plugin-dev-for-ai.md)** · **[English AI Spec](en/plugin-dev-for-ai.md)**：由 `bun run docs:sync` 从上述中英文分章自动拼装生成的完整单文件规范，适合直接交给 AI 工具从零构建插件。请勿只改生成文件，修改源章节后运行 `bun run docs:sync` 即可保持双语严格同步。
