# AI 后端接入：差异归属记录

本记录只描述本核心代理可确认的工作，不是开工时的完整工作区快照。开工前已有未提交和未跟踪改动；无法仅凭当前 HEAD 判定共享文件全部差异的归属。最终提交前须由审计代理逐 hunk 核实，不能按目录或整个共享文件暂存。

- 本代理新增：`src/ai/chat_backend.rs`、`src/ai/chat_plan.rs`、`src/ai/chat_drafts.rs`、`src/ai/chat_roots.rs`、`src/ai/backend/bridge.rs`、`config.rs`、`runtime.rs`、`types.rs`、`gate.rs`、`priority.rs`、`docs/agent-integration-plan.md` 和本文件。共享代理可能在这些文件继续接线，提交前仍须复核当前内容。
- 本代理新增 `src/ai/chat_skills.rs`，并在共享 `agent.rs`、`chat.rs`、`chat_backend.rs` 和三种适配器的工具白名单中接入只读技能资源；`skill_resources.rs` 及其子目录由另一代理实现。`backend/config.rs` 的 dsh 默认模型回退也属于本任务。
- 本代理新增 `src/ai/chat_models.rs`、`backend/codex/models.rs`、`backend/claude/models.rs`，并更新公共类型、配置及模块注册，负责模型目录、显示名称、选择持久化和请求传参；UI 代理负责底部模型菜单及 `@` 当前笔记接线。
- 本代理修改共享文件：`src/ai/chat.rs` 的外部会话状态、停止、草稿、上下文和内置异步失败处理；`src/ai/chat_composer.rs` 的后端分流、草稿和范围校验；`src/ai/agent.rs` 的计划校验、笔记工具目录和路径保护；`src/ai/mod.rs` 的后端模块注册；`src/editor/session.rs` 中新增的 `has_parked_session` 方法。上述文件还有开工前或并行代理的差异，必须逐 hunk 确认。
- 本代理在共享后端注册文件 `src/ai/backend/mod.rs` 注册公共模块和适配器；在 dsh 适配器的 `dsh.rs` 和 `dsh/process.rs` 分别接入安装状态、终止时关闭工具桥。三个后端各自适配器及资源模块由其他代理负责，不能假定其全部差异已验收。
- `src/ai/backend_ui.rs`、`src/ai/dsh_ui.rs`、`src/ai/chat_ui.rs` 及 `src/ui/` 的相关界面由 UI 代理负责；`chat_ui.rs` 有开工前改动，需 UI 代理和审计代理逐 hunk 确认。
- `designs/agent-backends` 被全局忽略；仅在设计代理确认文件归属后精确强制暂存项目源、handoff 和 changes，不含 server/history。
- 其余 `src/editor/` 与 `src/smoke*` 等预存或并行任务差异不归本代理，本任务不得删除或整文件暂存。用户要求不建 worktree/分支；可用 HEAD/index 临时快照审计。
- 临时隔离验收入口与 fixture 不属于交付；待 UI 代理提供精确清单后，从提交快照排除，不影响运行中的验收进程。`src/editor/view.rs` 的 `block_plain_text` 属于必要的既有前置修复，按用户决定单独提交。
- UI 代理确认临时入口为 `src/main.rs`、`src/smoke.rs`、`src/smoke_client.rs` 的 `/tmp/block-agent-accept-LfK5JFMH/temporary-tracked.patch`，`src/ai/backend/dsh/process.rs` 的 `/tmp/block-agent-accept-LfK5JFMH/temporary-dsh.patch`，以及整个未跟踪的 `src/smoke_interactive.rs`；提交快照排除这些差异，不撤除运行中的入口。
- 底部模型选择及 `@` 当前笔记由 UI 代理负责；模型动态目录、原生请求参数及持久配置由核心代理负责。失败草稿重启恢复补丁涉及 `chat_drafts.rs`、`chat.rs`、`chat_composer.rs`，以及 UI 代理的 `chat_ui.rs` 草稿持久化接线；其重启验收仍待完成。

此前没有可核实的开工 baseline；如某 hunk 的来源无法独立确定，必须明确标记未知，不能推断为本任务或覆盖原改动。此记录不授权构建安装、提交或推送。
