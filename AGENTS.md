## 验收标准

- 改完后执行&#32;<code>bun run build</code>：构建 release macOS&#32;<code>&#46;app</code>，安装替换&#32;<code>&#47;Applications&#47;Block&#46;app</code>&#32;并启动。失败如实报告，不把构建成功当成功能通过。
- 每次改完代码后，全量运行项目内置的窗口内自动点击自检：`/Applications/Block.app/Contents/MacOS/block --smoke-once`。按本次改动的影响面补齐验收覆盖，不能只跑原有步骤就宣称新增交互通过。
- 验收通过 `src/smoke.rs`、`src/smoke_steps.rs` 和后台 smoke worker 向隔离窗口投递 GPUI 鼠标、键盘事件；拖拽使用 MouseDown → MouseMove → MouseUp，断言实际选区、文本或业务状态。复用现有测试沙箱，不使用用户真实笔记和会话做写入验证。
- **开发验收不接管用户电脑。** 不使用 Codex 电脑工具、`agent-desktop`、LCU、AppleScript 或系统鼠标键盘注入来替代内置自检，不抢焦点、不拖动用户鼠标、不操作用户当前窗口。新增点击、拖拽或选区功能不构成实际桌面操作授权。
- 内置自检缺少覆盖时，优先补齐现有自动化入口；确有阻碍则说明缺失项，不擅自改用桌面操作。只有用户另行明确要求操作电脑时，才使用 Codex 当前会话内置的电脑使用工具。
- 崩溃与错误排查查看&#32;<code>Block&#47;logs&#47;block&#46;log</code>：Linux 在&#32;<code>dirs&#58;&#58;state&#95;dir</code>（<code>&#36;XDG&#95;STATE&#95;HOME</code>&#32;或&#32;<code>&#126;&#47;&#46;local&#47;state</code>），macOS 在&#32;<code>&#126;&#47;Library&#47;Application Support</code>，Windows 在&#32;<code>&#37;LOCALAPPDATA&#37;</code>（后两者是&#32;<code>dirs&#58;&#58;data&#95;local&#95;dir</code>，因为&#32;<code>state&#95;dir</code>&#32;为空）。macOS 崩溃报告仍看&#32;<code>&#126;&#47;Library&#47;Logs&#47;DiagnosticReports&#47;block&#45;&#42;&#46;ips</code>。日志文件只允许写入警告、错误与崩溃记录（<code>WARN</code>&#32;&#47;&#32;<code>ERROR</code>&#32;&#47;&#32;<code>PANIC</code>），不写入普通&#32;<code>INFO</code>&#32;日志。

## 版本与代码组织

- 手写实现按独立职责拆分，每个文件不超过 300 行。
- 新增依赖、工具链或其他技术栈时，一定先查最新稳定版并使用最新版，不凭记忆写版本号。不用&#32;<code>&#61;x&#46;y&#46;z</code>&#32;精确锁定版本，除非有写明原因的兼容性问题。

## 提交规范

- 只在 `main` 上开发，不新建分支或 worktree；当前不在 `main` 时先告知用户。
- 每次改动完成后，先获取并合并远端最新 `main`（`git fetch origin`，再将 `origin/main` 合入本地），再提交推送；不得只 fetch 不合并，也不得先提交本次改动再同步。同步前妥善保留未提交工作，恢复后也要检查冲突。
- 有冲突就逐项理解并解决，保留双方有效改动；按影响面完成必要验收后，只原子提交本任务改动并推送 `main`，提交说明用英文。不得覆盖无关工作或强制推送。
- 推送若因远端新增提交被拒绝，重新同步、解决冲突并复核后重试；完成后确认本地提交已包含在远端，不能把仅本地提交当作已推送。

## 界面约定

- 编辑区只有标准宽度，不提供「标准 &#47; 全宽」切换。
- 设计稿参考放在&#32;<code>designs&#47;thinkly&#45;reference&#47;</code>（来自 thinkly 项目的 DESIGN&#46;md、块把手、按钮等稿子和编辑区块行为清单），界面和编辑行为以它为准。
- 块把手只在鼠标悬停或光标所在的块旁显示，不常驻。
- 添加或修改 UI 组件与交互时，必须优先使用&#32;<code>src&#47;ui&#47;</code>&#32;路径下的独立组件库（已内置统一视觉样式、触发交互与进出场动效），禁止在业务代码里自行重复实现；如果&#32;<code>src&#47;ui&#47;</code>&#32;中尚无对应组件，新实现时必须抽离为通用组件放入&#32;<code>src&#47;ui&#47;</code>&#32;下统一维护。

