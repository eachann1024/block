<!-- ai-guide:skip-start -->
# 双向 JSON-RPC 2.0 协议、声明式 UI 与 Python 骨架

> [← 清单与权限](manifest.md) · [文档中心](../README.md) · [检查与上架 →](check-and-store.md)

---
<!-- ai-guide:skip-end -->

## 3. 双向 JSON-RPC 2.0 通信协议

所有请求与响应均为单行 UTF-8 JSON（末尾 `\n`）。

### 3.1 宿主调用插件（Host -> Plugin）

1. **`plugin.initialize`**
   - 请求 `params`：
     ```json
     {
       "plugin_id": "example-sync",
       "data_dir": "/abs/path/to/apps-data/example-sync",
       "downloads_dir": "/Users/name/Downloads",
       "notebook_root": "/abs/path/to/notebook",
       "notebooks": ["/abs/path/to/notebook", "/abs/path/to/other-notebook"],
       "locale": "zh",
       "theme": "system",
       "tarot": false,
       "host_version": "1.0.854",
       "ui_capabilities": [
         "kv_tooltip", "treemap", "host_perf", "checklist", "form_layout",
         "multi_notebook", "card_collapse", "metric_tone", "log_columns", "file_picker",
         "action_feedback", "action_progress", "note_revision",
         "sidebar_sections", "file_badges", "sidebar_menu", "backdrops",
         "status_icon", "toast_icon", "notes_open", "log_actions", "panel_toggle",
         "editor_insert_mode", "dialog_token", "checklist_add_folder", "item_icons",
         "tab_menu", "file_path", "log_timeline", "progress_bar", "segmented_status",
         "store_changelog", "menubar", "menubar_drop"
       ]
     }
     ```
     - **运行时上下文**：`notebook_root` 仅在声明 `notes:read`、`notes:write` 或 `events:note_saved` 任一权限且目标笔记本非空时传入，否则为 `""`；`notebooks` 仅在声明 `notes:read` 时填充，否则为 `[]`。切换桌面笔记本不会立刻重启全部插件，仅在下次调用该插件且目标根目录与当前运行时上下文不同（含后台 `note.saved` 事件）时，宿主才关闭旧进程并按新 `notebook_root` 重新初始化。
     - `downloads_dir`（默认 `~/Downloads`，环境变量 `BLOCKMD_DOWNLOADS_DIR`）：**凡涉及导出文件、备份归档或清理转移的目录，一律默认输出到 `downloads_dir`**（单文件直接输出到 `downloads_dir/<文件名>`，多文件收拢到一个干净文件夹内，严禁默认塞进 `apps-data` 隐藏目录或在笔记本内新建 `.trash-*` 等隐藏目录）。
     - `ui_capabilities` 可能在旧宿主中省略，用于渐进启用特性：
       `kv_tooltip`（`kv` 行提示）、`treemap`（空间分布图控件）、`host_perf`（`host.perf.snapshot`；注意本地 CLI 校验器尚未注入性能回调，调用会返回 `-32005`，插件应做降级处理）、`checklist`（多选勾选列表）、`checklist_add_folder`（`checklist.add_folder` 加号添加文件夹）、`form_layout`（`layout` 与 `group`）、`multi_notebook`（多笔记本列表与读写）、`card_collapse`（卡片折叠）、`metric_tone`（`metrics[].tone` 柔和色板）、`log_columns`（`log_list` 对齐列、`sub_text`/`ratio`/`tone`/`tooltip` 与 `→` 映射模式）、`file_picker`（`folder.accept_extensions`）、`action_feedback`（`plugin.action` 的 `ok`/`tone` 与内联/按钮反馈）、`action_progress`（`host.action.progress`）、`note_revision`（`revision`/`expected_revision` 乐观并发）、`sidebar_sections`（左侧栏插件分区）、`file_badges`（文件树行尾角标）、`sidebar_menu`（左侧栏文件树右键菜单）、`backdrops`（编辑区自定义矢量背景）、`status_icon`（底部状态栏图标）、`toast_icon`（Toast 图标）、`notes_open`（`host.notes.open` 跳转打开笔记）、`log_actions`（`log_list` 条目点击触发 `action_id`）、`panel_toggle`（`host.ui.toggle_panel` 原子切换侧栏面板）、`editor_insert_mode`（`host.editor.insert` 支持 `"mode": "insert"|"replace"` 非破坏插入）、`dialog_token`（`host.ui.open_dialog` 返回宿主确认句柄 `dialog_token`）、`item_icons`（`switch` / `kv` 行前置图标 `icon` 与 `icon_strip` 图标条）、`tab_menu`（标签页右键菜单 `contributes.tab_menu` 与 `sidebar_menu.targets` 文件夹行）、`file_path`（`plugin.command` 的 `context.file` / `context.folder`，`file` 需 `"notes:path"`）、`log_timeline`（`log_list` 的 `"variant": "timeline"` 垂直时间轴模式，支持折叠静默区间 `kind: "span"`、`badge`/`span`/`details` 与聚合标签 `tags`）、`progress_bar`（`progress` 内联进度条控件及 `host.action.progress` 的独立通道 `channel` / `tone` / `running`）、`segmented_status`（`segmented` 选项的状态圆点 `tone`、悬停提示 `tooltip` 与通道实时状态绑定 `channel`）、`toast_banner`（右上角全局通知横幅支持 `body`/`action`、`shown` 返回值与默认插件图标）、`open_settings`（`host.ui.open_settings` 打开插件设置页）、`store_changelog`（`host.store.changelog` 查询应用商店插件更新日志接口）、`menubar`（系统菜单栏图标 `contributes.menubar_items` 与 `host.menubar.set`）、`menubar_drop`（菜单栏图标接收拖放，目前仅 macOS）。
   - 返回 `result`：`{"ready": true}`。

2. **`plugin.render`**
   - 请求 `params`：例如 `{"surface": "settings_page", "values": {"protocol": "webdav"}}`。
     - 首次打开、切换笔记本/当前笔记/笔记本列表、动作前表单补刷、定时轮询及 `note.saved` 成功后刷新时省略 `values`。
     - 用户在界面编辑控件时（离散控件约 25ms 防抖，文本输入约 220ms 防抖），`values` 仅包含用户尚未同步的改动字段。将这些字段合并到持久配置后生成预览；需要显式提交的表单由 `plugin.action` 保存。
   - 返回 `result`：包含必填 `page`（严格解码）与可选 `values`。
     - **值合并规则**：
       - 首次加载、笔记本/活动笔记/笔记本列表变化触发的重载、编辑预览刷新与动作前表单补刷都会消费 `result.values`（更新表单并清除对应字段的未同步标记）。
       - 定时轮询（`refresh_interval_ms`）与 `note.saved` 成功后的刷新忽略 `result.values`，但仍会用新 `page` 的控件默认值更新未被用户编辑的键（`!editedControls`）。
       - 编辑预览刷新中，新 `page` 的控件默认值只补全缓存里尚不存在的新键（已存在的键以用户编辑态或 `result.values` 为准）。

3. **`plugin.action`**
   - 用户点击页面按钮，或点击右上角通知横幅（`host.ui.toast` / `plugin.command`）上的操作按钮时触发（后者 `surface` 为 `"toast"`，`action_id` 为 `action.id`，`values` 为 `action.payload`）。注意：本地校验器会调用初始 `UIPage` 中的**全部** `header_actions` 与卡片顶层 `actions` 按钮（不判断 `visible_when` 与卡片折叠状态），需避免默认配置下触发非预期外部副作用。
   - 请求 `params`：
     ```json
     {
       "surface": "settings_page",
       "action_id": "save_config",
       "values": {
         "protocol": "local_mirror",
         "mirror_dir": "/Users/name/Sync",
         "selected_notebooks": ["/Users/name/Notes/Work"]
       }
     }
     ```
     `values` 为宿主缓存的整份表单值（覆盖 `segmented`、`select`、`input`、`folder`、`secret`、`switch`、`checklist`，含 `group` 内子控件，也可能残留当前隐藏、已移除或插件此前回传的键）；插件应按当前有效分支读取并合并持久化到 `host.storage.set`。
   - 返回 `result`（字段均可选；`page` 若返回须为完整合法 `UIPage` 对象，见第 4 节）：
     ```json
     {
       "ok": true,
       "tone": "success",
       "values": { "protocol": "local_mirror" },
       "status": "已同步 2 篇笔记",
       "open_path": "/Users/name/Downloads/欢迎使用 Block.html",
       "reveal_path": "/Users/name/Downloads/欢迎使用 Block.html"
     }
     ```
     - **表单更新顺序**：若同时返回 `page` 与 `values`，宿主先用新 `page` 的控件默认值覆盖对应键，再应用 `result.values` 并清除对应字段的未同步标记；若 `page.status` 为空且 `result.status` 非空，宿主自动将 `result.status` 填入 `page.status`。
     - **动作反馈（`ok` / `tone` / `status`）**：合法 `tone`（`"success"` | `"warning"`/`"warn"` | `"danger"`/`"error"` | `"info"`）优先级最高；未知或空 `tone` 回退按 `ok` 判定（`true` 为 `success`，`false` 为 `danger`）；两者均省略时按 `status` 关键词推断，未命中默认 `"success"`。卡片内按钮返回 `danger`/`warning` 且 `status` 非空时，在按钮上方显示提示条，`success`/`info` 在按钮旁显示单行状态；`header_actions` 在顶栏展示状态。`success` 态按钮会短暂切换为绿色勾选态（2.4s）；`action_id` 为 `test_conn`、以 `test_` 开头或以 `_test` 结尾的连接测试按钮会保持成功态，直到用户修改表单或触发下一次动作。
     - **打开与定位输出文件（`open_path` / `reveal_path`）**：切勿在插件内自行调用 `open`/`xdg-open` 子进程。宿主会先对路径做 `Clean` 归一化，再验证目标**已存在且位于 `downloads_dir` 内**，且 `downloads_dir` 之下的相对路径组件及目标均不得为符号链接（`downloads_dir` 本身及其祖先不在逐段检查范围内），不满足则静默忽略。`open_path` 禁止打开 `.app` 目录，且普通文件仅允许 `.html`、`.htm`、`.pdf`、`.txt`、`.md`、`.markdown`、`.csv`、`.tsv`、`.png`、`.jpg`、`.jpeg`、`.webp`、`.gif`、`.docx`、`.xlsx`、`.pptx`；其余格式（如 `.zip`）请使用 `reveal_path`（Linux 下对文件打开其父目录）。即便 `ok: false`，宿主仍会处理非空路径，因此失败动作应省略这两个字段。

4. **`plugin.event`**（笔记保存/删除事件需声明 `"events:note_saved"`，系统事件见 3.3 节）
   - 请求 `params`：
     - 单笔记保存/删除（`"note.saved"` / `"note.deleted"`）：`{"event": "note.saved", "notebook_root": "/abs/path/to/notebook", "path": "welcome.md", "timestamp": "2026-11-18T10:00:00Z"}`
     - 目录删除（`"note.deleted"`）：`{"event": "note.deleted", "notebook_root": "/abs/path/to/notebook", "path": "", "folder": "notes/archive", "timestamp": "2026-11-18T10:00:00Z"}`（仅目录级删除、重命名或移动触发时附带非空 `folder` 斜杠相对路径，此时 `path` 为 `""`）
   - 仅投递给已启用的订阅插件：`note.saved` 由块编辑保存、重命名/移动后新路径与其他插件通过 `host.notes.write` 产生的实际变更触发；`note.deleted` 由移入废纸篓、重命名/移动（旧路径）与 `host.notes.trash` 触发（均不含写入/删除者自身，不包含源码编辑保存分支）；`timestamp` 为每次派发/重试时的 UTC 时间。插件应以事件参数自带的 `notebook_root`、`path` 与可选 `folder` 为准。
   - 宿主按 `(plugin_id, notebook_root, 路径/dir:目录)` 合并排队：同文件在队/在途时若发生删除会覆盖为 `note.deleted` 并重新派发；目录删除会清除该目录前缀下挂起的单文件错误与后续补发（跨文件无全局顺序保证）。
   - 响应要求：宿主会将 `result` 解码为 `{"handled": <bool>, "outcome": <string>}`（返回 `{"handled": true}`、`{"handled": false}` 或 `{}` 均视为成功；`handled` 为 `false` 且 `outcome` 为空时按 `"skipped"` 处理以兼容未订阅删除事件的旧插件；返回非对象如 `true` 或错误类型如 `{"handled": "false"}` 会因解码失败报错）。业务失败须返回 JSON-RPC `error`，宿主才会在侧栏标错并在空闲、同路径未重新排队且无更高优先错误时展示重试入口（可能来自另一笔记本）。成功后若该笔记本已有缓存页且无忙碌动作，宿主会自动触发 `plugin.render` 刷新（即使当前未停留在插件页）。

5. **`plugin.shutdown`**
   - 请求 `params`：`{}`
   - 宿主发出 `plugin.shutdown` 前已将运行时标为关闭并停止受理反向 `host.*` 调用，因此关闭阶段**切勿调用 `host.storage.set` 等宿主 RPC**。推荐直接返回 `{"ok": true}`（宿主不校验响应内容），随后在 2 秒内正常退出。

---

### 3.2 插件调用宿主能力（Plugin -> Host）

插件在处理 `plugin.initialize`、`plugin.render`、`plugin.action`、`plugin.command` 或 `plugin.event` 请求期间，可向 `stdout` 发送 JSON-RPC 请求调用 `host.*`，并从 `stdin` 读取匹配 `id` 的响应后再返回外层结果（同时在途不超过 8 个；`plugin.shutdown` 期间宿主不再响应 `host.*`）。

常见错误码：`-32001` 缺少权限、`-32002` 存储失败或超 8 MiB、`-32003` 列举笔记失败（含未注册笔记本）、`-32004` 笔记文件系统错误（含文件打开/读写失败、路径下存在符号链接或超 16 MiB）、`-32005` 性能快照回调不可用或返回错误、`-32006` 编辑器脏状态/虚拟预览或版本冲突、`-32601` 未知方法、`-32602` 参数无效（含 `read`/`write` 未注册笔记本或参数路径不合规）。

笔记路径边界：支持扩展名 `.md`、`.markdown`、`.mdown`、`.mkd`、`.mkdn`、`.mdwn`、`.mdtxt`、`.mdtext`、`.mdx`（不区分大小写）。显式 `notebook_root` 会去空白并转绝对路径后与已注册笔记本精确匹配；省略时使用当前运行时上下文根。`host.notes.list` 跳过 `.` 开头项与非普通文件；`host.notes.read`/`write` 先对相对路径做 `Clean` 归一化，参数层校验须位于笔记本内、无 `:`、无 `.` 开头路径段且为支持后缀（不合规则报 `-32602`），文件系统层打开时拒绝符号链接且限制旧文件/读取 <= 16 MiB（失败报 `-32004`）。

1. **`host.storage.get`**（无需声明权限）
   - 参数解码失败按空对象处理。单键：`{"key": "config"}` -> 命中 `{"value": {"auto_sync": true}, "found": true}`，未命中 `{"value": null, "found": false}`；全量：`{}` -> `{"data": {"config": {"auto_sync": true}}}`。
2. **`host.storage.set`**（无需声明权限）
   - `{"key": "config", "value": {"auto_sync": true}}` -> `{"ok": true}`。`key` 须为非空白字符串（否则 `-32602`）；省略 `value` 会存成 `null`。宿主按原始 JSON 保留值（大整数游标等数值不会经浮点数转换而丢精度）。
   - `host.storage.set_many` 同样无需声明权限并受 8 MiB 限制。`{"values": {"config": {...}, "migration": null}}` 一次写入 1–64 个非空白键，成功返回 `{"ok": true}`。
3. **`host.notes.active`**（需 `"notes:read"`，忽略参数）
   - `{}` -> `{"path": "welcome.md"}`（返回宿主缓存的活动标签相对路径，可能为空、非 Markdown 或尚未更新）。
4. **`host.notebooks.list`**（需 `"notes:read"`，忽略参数）
   - `{}` -> `{"notebooks": [{"path": "/abs/path/to/notebook", "name": "notebook", "active": true}]}`（`active` 反映当前运行时上下文根，建议配合 `checklist` 控件勾选范围）。
5. **`host.notes.list`**（需 `"notes:read"`）
   - `{"notebook_root": "/abs/path/to/notebook"}`（可选；解码失败按省略处理；省略且运行时根为空时返回 `[]`；传未注册笔记本报 `-32003`）-> `{"notes": [{"path": "welcome.md", "size": 42, "mod_time": "2026-11-18T10:00:00Z"}]}`
6. **`host.notes.read`**（需 `"notes:read"`）
   - `{"notebook_root": "/abs/path/to/notebook", "path": "welcome.md"}`（`notebook_root` 可选，省略且运行时根为空报 `-32602`；`path` 必填）-> `{"path": "welcome.md", "content": "# 欢迎...", "revision": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}`
7. **`host.notes.write`**（需 `"notes:write"`）
   - `{"notebook_root": "/abs/path/to/notebook", "path": "notes/synced.md", "content": "# 内容", "expected_revision": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}` -> `{"ok": true, "changed": true}`（示例哈希须替换为 `host.notes.read` 实际返回的 `revision`，原样传固定值会在实际写入时触发 `-32006`）。
   - **必须显式传入字符串 `content`**（省略或传 `null` 直接返回 `-32602`，不会清空笔记；如需清空请显式传入 `""`）；单文件上限 **16 MiB**。
   - **冲突与幂等判定顺序**：
     1. 桌面端先检查编辑器会话：若目标笔记存在未保存修改或处于虚拟预览，直接返回 `-32006`（即使目标字节相同；此类冲突需用户先保存或退出预览）。
     2. 读取磁盘旧文件并逐字节比较：若文件已存在且内容完全相同，直接返回 `{"ok": true, "changed": false}`（即使 `expected_revision` 为 `""` 或旧哈希，也不报错、不写盘、不记历史、不触发事件）。
     3. 核对 `expected_revision`：省略或传 `null` 跳过校验；传 `""` 要求文件尚不存在；传读取时的 64 位小写 SHA-256 哈希则要求磁盘当前内容匹配，否则返回 `-32006`（不提供对外部写入进程的原子 CAS）。
   - 实际写入时原子落盘、尽力记录「插件保存」版本历史、刷新干净的编辑器标签页并向其他订阅插件派发 `note.saved`。响应不含新 `revision`，连续修改前请重新 `host.notes.read`。
8. **`host.notes.trash`**（需 `"notes:write"`）
   - `{"notebook_root": "/abs/path/to/notebook", "path": "notes/old.md", "expected_revision": "<读取时的 SHA-256>"}` -> 移入系统回收站返回 `{"ok": true, "trashed": true}`，文件已不存在返回 `{"ok": true, "trashed": false}`。
   - 供同步类插件把远端删除落到本地，不弹确认框；因此 **`expected_revision` 必填**（省略或空串报 `-32602`），须等于磁盘当前字节的 64 位小写 SHA-256，否则报 `-32006`，插件绝不会删掉自己没读到过的内容。
   - 路径规则同 `read`/`write`，但也允许笔记本内的附件等非 Markdown 普通文件（仍拒绝 `.` 开头路径段与符号链接）。目标笔记在编辑器中有未保存修改或处于虚拟预览时报 `-32006`；成功后宿主关闭对应标签页并重新扫描笔记本。需要用户逐次确认的删除请改用 `host.system.trash`。
9. **`host.notes.open`**（需 `"notes:read"`）
   - `{"notebook_root": "/abs/path/to/notebook", "path": "welcome.md"}`（`notebook_root` 可选；严格校验目标为已注册笔记本内的合法普通 Markdown 文件且非符号链接）-> 在桌面编辑器中切换打开目标笔记（若设置页开着会自动收起）并返回 `{"ok": true, "path": "welcome.md"}`；无 UI 宿主环境（如 CLI 校验）返回 `-32005`。
10. **`host.action.progress`**（无需额外权限；须先在 `plugin.initialize` 的 `ui_capabilities` 中检查 `"action_progress"`，旧宿主不支持时不得发送）
   - 在 `plugin.action` 执行期间实时上报进度，推荐以 JSON-RPC 2.0 通知（省略 `id`，无需等待响应，宿主走内联快速通道以保证顺序且不占并发槽）发送，也支持普通调用（返回 `{"ok": true}`）：
     - 计数模式：`{"done": 12, "total": 48}`（`total > 0`，宿主在标题栏运行胶囊中展示 `12/48`）
     - 百分比模式：`{"progress": 0.32}`（`0.0`–`1.0`，仅在未传 `total` 时生效，宿主在标题栏运行胶囊中展示 `32%`）
     - 可选 `label` 覆盖运行动词（最多 32 字符；默认由宿主按 `action_id` 推断，如 `同步中` / `迁入中` / `上传中`）
     - 分通道模式（需 `"progress_bar"`）：附加可选 `channel`（最多 64 字符，小写字母/数字/`-`/`_`）、`tone`（`""` | `"accent"` | `"info"` | `"success"` | `"warning"` | `"danger"`）与 `running`（布尔值，省略时按是否达终态自动推断），宿主将进度路由至绑定相同 `channel` 的 `progress` 控件与 `segmented` 选项，互不覆盖顶栏总进度。
   - 空载荷或无效参数不会覆盖当前进度；动作结束或取消时宿主自动清空进度，并在内部节流重绘（保留首帧与末帧）。
11. **`host.perf.snapshot`**（需 `"notes:read"`）
   - `{"refresh_disk": false}`（可选，解码失败按 `false`；仅在宿主尚无磁盘扫描缓存时，传 `false` 才会触发首次扫描，内置性能窗口可能已生成缓存）-> 返回宿主资源与 7 类磁盘占用摘要（分类 ID：`notes`、`assets`、`history`、`recovery`、`ai_sessions`、`logs`、`config`；每类 `entries` 最多前 6 条摘要；`scan_error`、`file_count`、`entries` 与明细 `path`/`count` 可省略；磁盘扫描异常写在成功响应的 `scan_error` 中，仅回调不可用或失败时返回 `-32005`）：
     ```json
     {
       "cpu_percent": 1.2,
       "rss_bytes": 125829120,
       "total_bytes": 52428800,
       "last_scan": "01:00:00",
       "categories": [
         {
           "id": "notes",
           "label": "笔记",
           "detail": "Markdown 正文",
           "bytes": 10485760,
           "file_count": 128,
           "entries": [
             { "name": "Work", "path": "/abs/path/Work", "bytes": 5242880, "count": 64 }
           ]
         }
       ]
     }
     ```

12. **`host.store.changelog`**（无需额外权限；须先在 `plugin.initialize` 的 `ui_capabilities` 中检查 `"store_changelog"`）
    - 查询指定插件（省略 `id` 时默认查当前插件自身）的商店更新日志：`{"id": "block-sync", "locale": "zh-CN", "limit": 10}`（`id`、`locale`、`limit` 均可选，`limit` 默认 `10`、上限 `20`）。
    - 宿主按「商店静态 JSON API（`changelogs/<id>.json` + jsDelivr 镜像）→ GitHub Releases API → 商店索引内联 `changelog` → 本地 `CHANGELOG.md`」顺序解析，返回结构化 `PluginChangelog`（含 `id`、`repo`、`latest_version`、`changelog_url`、`source`、`releases[]` 及双语 `zh`/`en` 分组条目）；暂无更新日志返回 `-32004`，拉取失败返回 `-32005`。

13. **`host.log`**（无需额外权限；旧宿主返回 `-32601`，插件应静默停用）
    - `{"entries": [{"level": "info", "event": "delete_remote", "notebook_root": "/abs/path/to/notebook", "path": "notes/old.md", "protocol": "webdav", "detail": "成功"}]}` -> `{"ok": true}`，把插件自己的操作记录写进宿主诊断日志 `logs/block.log`，便于用户反馈时一并导出。
    - `level` 取 `info`（默认）/`warn`/`error`；每次最多 200 条，超出部分丢弃；各字段折叠为单行并截断至 400 字节。合法的笔记本相对 `path` 会被换成绝对路径再脱敏，与宿主自身日志的路径代号一致。**不得传入笔记正文或密钥。**

### 3.3 系统集成

桌面宿主在 `plugin.initialize.ui_capabilities` 中提供 `"system_integration"`。`host.system.capabilities {}` 无需额外权限，返回布尔值对象，字段为 `notify`、`notify_click`、`secrets`、`open_url`、`reveal`、`open_dialog`、`save_dialog`、`clipboard_read`、`clipboard_write`、`trash`、`badge`、`wake`、`network`、`focus`。结果反映当前桌面可用性，实际调用还受清单权限、系统授权和服务状态影响。

| 接口 | 权限 | 参数与返回 |
| --- | --- | --- |
| `host.system.notify` | 无需声明（旧清单 `system:notify` 仍兼容） | `{"title":"同步失败","body":"打开同步设置查看详情。","tone":"error"}`。开放给所有插件，直接发送操作系统原生系统通知（无论 Block 窗口是否聚焦；若需应用内右上角全局横幅，请改用 `host.ui.toast`）。`title` 和 `body` 必填，分别截断至 80 和 240 字符。可传 `notebook_root` 指定已注册笔记本内的插件设置，或同时传 `rel_path` 指定 Markdown 笔记，需 `notes:read`。可用 `timeout_ms` 缩短等待时间，默认 15000 毫秒。可传最长 128 字节的 `dedup_key` 区分文案及目标相同的独立事件，同一事件重试时沿用原值。桌面支持通知点击时，打开目标笔记或插件设置。宿主提交至系统通知服务或识别到同一事件 2 分钟内已经送达时返回 `delivered:true`。需要跨重启补送的插件应保留待发记录，直至收到 `delivered:true`。2 分钟内重复的相同事件返回 `suppressed:true`。每个插件 10 秒内最多投递 5 条不同通知，超出时返回 `-32006`，稍后重试即可。 |
| `host.system.open_url` | `system:open` | `{"url":"https://example.com"}`，仅接受无账号和密码的 HTTP(S) 链接。 |
| `host.system.reveal` | `system:open`、`notes:read` | `{"rel_path":"assets/example.png","notebook_root":"/abs/notebook"}`，在系统文件管理器里显示文件。 |
| `host.system.open_dialog`、`host.system.save_dialog` | `system:dialogs` | 可传 `title`、`default_path`、`extensions`。打开框另支持 `directory`、`multiple`。选目录时省略 `extensions`。返回 `{"ok":true,"paths":["/abs/file"],"canceled":false}`，用户主动取消时 `canceled` 为 `true` 且 `paths` 为空。发起动作被用户取消或动作超时时返回 `-32007`（若系统原生选择框无法由宿主强制关闭，用户后续关闭时宿主会直接丢弃过期结果）。 |
| `host.clipboard.read`、`host.clipboard.write` | `system:clipboard_read`、`system:clipboard_write` | 读取传 `{}`，返回 `{"ok":true,"text":"..."}`。写入传 `{"text":"..."}`。支持最多 64 KiB UTF-8 文本。 |
| `host.system.trash` | `system:trash`、`notes:write` | 与 `reveal` 参数相同，仅支持普通文件。用户在原生确认框确认后，宿主移到系统回收站并更新相关标签页。 |
| `host.system.badge` | 无需声明（旧清单 `system:badge` 仍兼容） | `{"count":3}`，范围 0–999，0 清除本插件角标。宿主合计已启用插件的数值，上限 999。 |
| `host.secrets.get`、`host.secrets.set`、`host.secrets.delete` | `system:secrets` | `{"key":"auth.token"}`，写入另传 `value`（1–1536 字节，无 NUL）。键以 ASCII 字母或数字开头，后续允许字母、数字、`.`、`_`、`-`，总长最多 128 字符。读取返回 `{"ok":true,"found":true,"value":"..."}` 或 `{"ok":true,"found":false}`。删除支持重复调用。宿主按应用数据目录、插件 ID 和安装来源隔离凭据。缺少宿主安装来源记录时，凭据接口返回不可用。 |

其余写操作成功返回 `{"ok":true}`。打开网页、定位文件、文件选择、剪贴板和回收站操作需要由用户触发的 `plugin.action` 或 `plugin.command`，且 Block 窗口有焦点。授权随动作结束或切换笔记本而失效。笔记本路径使用 `/` 分隔，相对路径须指向可见项目，路径组件须为真实文件或目录。省略 `notebook_root` 时使用运行时笔记本。系统能力不可用时返回 `-32005`，繁忙时返回 `-32006`，操作取消或失效时返回 `-32007`。系统通知（`host.system.notify`）与右上角全局通知横幅（`host.ui.toast`）均无需声明权限，开放给所有插件直接调用。宿主生成的 Sync 失败提醒投递失败后，可在应用恢复焦点时补送，补送前复查插件身份和 Sync 通知开关。

`events:system` 通过 `plugin.event` 接收 `system.wake`、`network.changed`、`app.focus`。事件包含 `event` 和 UTC `timestamp`，唤醒和网络变化包含系统报告的 `online` 布尔值。需要笔记权限或保存事件权限才能收到 `notebook_root`。宿主可启动已启用的订阅插件，并按插件串行处理，排队中的同类事件只保留最新一条。`network.changed` 涵盖系统报告的状态变化和检测到的网卡地址变化。服务器可达性由插件自行判断。

macOS 使用通知中心、钥匙串和 Dock。macOS 通知的点击目标可跨 Block 重启保留七天，点击时重新检查安装状态和权限。Windows 使用系统通知、凭据管理器和任务栏叠加角标，通知点击依赖系统横幅仍然有效。Linux 使用桌面通知服务、Secret Service 和兼容 Unity Launcher 协议的 Dock，唤醒事件依赖 logind。Linux 需要通知服务支持 `actions` 才能点击通知，`notify_click:false` 时仍可能显示通知。Linux 系统凭据需在系统凭据管理器中先解锁。单次运行最多保留最近 128 条插件原生通知。系统选择框已打开时，原生后端可能需要用户关闭窗口，宿主会丢弃已过期动作的选择结果。缺少相应服务时，插件应根据能力查询或调用错误调整界面和行为。

回收确认前，宿主先保存正在编辑的内容。确认后，宿主记录恢复位置，将文件移到同卷的暂存目录并复核文件身份。Block 数据目录与原文件分处不同卷时，暂存目录建在原文件旁。Windows 还需要文件所在卷支持硬链接。条件不满足时保留原文件。回收失败时优先恢复原路径，原路径已被占用时保留暂存文件。系统「还原」会将文件放回暂存目录。Block 会保留原路径记录，启动时提示可恢复文件及其原路径。凭据权限约束宿主 RPC，独立插件进程仍以当前系统用户运行，其直接访问由操作系统控制。

---

## 4. 声明式 UI Schema（`UIPage`）精确定义

同一 `UIPage` 内：所有 `card.id` 唯一、所有表单控件 `item.id` 唯一、所有按钮 `button.id`（含 `header_actions` 与 `actions` 项）唯一。宿主已在设置窗口标题栏与插件页头部统一提供面包屑、单行合并状态位、主操作位、拨弦启用开关（Pluck Switch）与「⋯」更多菜单（含次要操作、查看说明/应用商店、重新加载与红色卸载），插件无需自己声明开关、取消任务按钮或卸载按钮。

### `UIPage` 顶层最小可复制示例
```json
{
  "title": "示例同步",
  "status": "上次同步 14:20",
  "header_actions": [
    { "id": "sync_now", "label": "立即同步", "variant": "primary" }
  ],
  "cards": [
    {
      "id": "target-card",
      "title": "存储目标",
      "items": [
        {
          "kind": "segmented",
          "id": "protocol",
          "label": "存储目标",
          "value": "local_mirror",
          "options": [
            { "value": "local_mirror", "label": "目录镜像" },
            { "value": "webdav", "label": "WebDAV" }
          ]
        }
      ]
    }
  ]
}
```
- `title`：必填非空字符串（桌面设置标题栏展示 `manifest.name`，但 `UIPage.title` 仍须提供）。
- `status`：可选只读次级状态短文本（仅展示真实执行结果、配置摘要或最近运行时间，闲置或未配置时传 `""`；严禁写 `"已就绪"` / `"已启用"` / `"已停用"` / `"迁入中…"` 等占位或运行中文案）。为保持顶栏克制，宿主在正常闲置时仅展示可提取的极简 `HH:MM` 时间（悬停显示完整原文）或隐藏冗长常规描述，仅在告警/出错时展示完整提示或红色错误详情胶囊。
- `progress`：可选 `0`–`1` 浮点数，用于声明式独立后台任务在空闲轮询刷新时展示百分比进度；`plugin.action` 执行期间请使用 `host.action.progress`。
- `cards`：必填，最多 64 张卡片；所有卡片的 `items` 合计最多 400 项。
- `refresh_interval_ms`：可选定时刷新间隔（`500`–`60000` 毫秒，`0` 或省略表示不自动轮询；仅在最近绘制时间窗口内且空闲时触发）。
- `header_actions`：可选标题栏动作按钮数组；每项含 `id`（必填唯一）、`label`（必填）、`variant`（仅限 `"primary"` | `"secondary"` | `"danger"`）。宿主将首个 `variant: "primary"`（无 primary 时回退首个 `danger`）放在标题栏主操作位，并**统一强制渲染为轻量幽灵按钮**（`26px` 高、透明底、悬停轻灰底；运行中变为同高轻量进度与 `■` 取消按钮），其余 `secondary` / `danger` / 多余按钮自动收进标题栏 `⋯` 菜单顶部。
- `cards`：必填非空数组；每项含 `id`（必填唯一）、`title`（可选）、`meta`（可选右侧副信息）、`badge`（可选右侧胶囊徽章）、`collapsible` / `default_collapsed`（可选折叠配置卡片，展开状态保存在当前控制器内存中）、`items`（必填非空数组；若卡片内所有顶层项均被 `visible_when` 隐藏，整个卡片自动隐藏）。

### 16 种 `UIItem` 控件（按 `kind` 区分）

**交互与布局准则**

- 选择本地文件夹使用 `folder`，需要同时允许选文件时附加 `accept_extensions`（如 `[".zip"]`）；远端路径和分支等文本使用 `input` 并给合理默认值；单项下拉用 `select`，多选列表（如勾选多个笔记本）用 `checklist`。
- 离散控件（`switch`、`segmented`、`select`、`checklist`、`folder`）切换后建议立即在 `plugin.render` 中持久化并通过 `result.values` 回传，不要强制用户再点「保存」；仅在包含远端地址、密钥等多项文本输入时提供「测试连接 / 保存」按钮。
- 涉及多笔记本同步或批量处理的插件，不要扫描并列出全部笔记本：声明 `ui_capabilities` 含 `"checklist_add_folder"` 时，用 `add_folder: true` 的 `checklist` 只展示用户通过右上角加号主动添加的文件夹，只对列表内的文件夹执行操作；旧宿主缺少该能力时回退为 `host.notebooks.list` 加普通 `checklist` 勾选。
- `layout` 合法值为 `""` | `"stacked"` | `"full"`：`"stacked"` 由 `segmented`/`folder`/`input`/`secret` 消费，`"full"` 仅 `segmented` 消费（且此时可省略 `label`）。
- 柔和色板键（用于 `metrics[].tone`、`log_list` 占比条与 `treemap.tiles[].color`）：`"assets"`（绿）、`"config"`（蓝）、`"notes"`（靛紫）、`"history"`（紫）、`"recovery"`（青）、`"logs"`（红）、`"ai_sessions"`（琥珀），省略或未知值回退中性底色。

任意 `UIItem`（含 `group.fields` 子项）均支持可选字段 **`visible_when`**，按其他控件当前值**零延迟动态显隐**（`control_id` 必填，至少提供 `equals` 或非空 `in`；`in` 为字符串数组且非空时优先于 `equals`；`equals: true` 与 `equals: "true"` 按 JSON 类型严格区分；未知 `control_id` 视为隐藏；若需按条件隐藏整组避免卡片只剩空标题，请将 `visible_when` 写在 `group` 上）：
```json
{
  "visible_when": { "control_id": "protocol", "equals": "webdav" }
}
```
或匹配多个字符串值：
```json
{
  "visible_when": { "control_id": "provider", "in": ["r2", "s3"] }
}
```

1. **`segmented`（分段切换，适合 2–3 个短选项）**：`id` 必填，`label` 在 `layout != "full"` 时必填，`options` 至少 2 项（每项 `value` 与 `label` 必填非空；需 `"segmented_status"` 时可选 `tone` 显示状态圆点、`tooltip` 显示悬停提示、`channel` 绑定 `host.action.progress` 实时运行/完成状态），`value`（`string`）必须命中某个 `options[].value`，可选 `layout`（`""` | `"stacked"` | `"full"`）：
   ```json
   {
     "kind": "segmented",
     "id": "protocol",
     "label": "存储目标",
     "value": "local_mirror",
     "options": [
       { "value": "local_mirror", "label": "目录镜像" },
       { "value": "webdav", "label": "WebDAV" },
       { "value": "git", "label": "Git 仓库" }
     ]
   }
   ```
2. **`select`（下拉选单）**：`id`、`label` 必填，`options` 至少 1 项（每项 `value` 与 `label` 必填非空），`value`（`string`）必须命中某个 `options[].value`，`placeholder` 可选。
3. **`input`（单行文本输入框）**：`id`、`label` 必填，`value` 必须为 `string`，`placeholder`、`layout`（`""` | `"stacked"`）可选：
   ```json
   {
     "kind": "input",
     "id": "endpoint_url",
     "label": "服务器地址",
     "value": "",
     "placeholder": "https://dav.example.com/dav"
   }
   ```
4. **`folder`（本地文件夹 / 可选文件选择器）**：`id`、`label`、`value` 必填，`value` 传完整路径或 `""`；可选 `layout`（`""` | `"stacked"`）与 `accept_extensions`（非空字符串数组如 `[".zip"]` 且宿主提供文件选择器时，额外显示文件选择按钮）。目录选择器允许新建目录，完整路径通过悬停提示展示；对话框默认打开当前绝对路径或笔记本根，切换笔记本会丢弃旧选择。注意：`accept_extensions` 仅为系统对话框过滤提示，宿主不复核选中路径后缀，插件处理前须自行校验文件类型与内容：
   ```json
   {
     "kind": "folder",
     "id": "source_path",
     "label": "导入目录或压缩包",
     "layout": "stacked",
     "value": "",
     "accept_extensions": [".zip"]
   }
   ```
5. **`secret`（密码/凭证输入框）**：字段同 `input`，`value` 必须为 `string`（宿主自动掩码并支持显示/隐藏切换）。
6. **`switch`（布尔开关）**：`id`、`label` 必填，`value` 必须为 `bool`（宿主统一使用 Ocean Paper Pluck Switch 渲染）：
   ```json
   {
     "kind": "switch",
     "id": "auto_sync",
     "label": "保存笔记后自动同步",
     "value": true
   }
   ```
   - **`icon`（可选，需 `"item_icons"` 能力；`kv` 的每行 `rows[].icon` 同理）**：行首图标。二选一：`{"name": "<宿主内置图标名>"}`（小写字母、数字与 `-`，未知名称渲染为空白），或与 `DuoIconSpec` 同形的内联矢量 `{"view_box": "0 0 24 24", "stroke_paths": [...], "fill_paths": [...], "accent_paths": [...]}`；可加 `tooltip`。第三种为文字胶囊 `{"chip": "统一发送", "dot": true}`（`chip` 最多 16 字，`dot` 加强调圆点，不可与图标字段混用）：行首只绘制圆点，`icon_strip` 中绘制为顶栏同款胶囊。`host.titlebar.get_items` 返回的 `items[].icon` 可直接透传。
7. **`actions`（卡片内操作按钮行）**：不含 `id`/`label`，仅含非空 `buttons` 数组：
   ```json
   {
     "kind": "actions",
     "buttons": [
       { "id": "save_config", "label": "保存配置", "variant": "primary" },
       { "id": "test_conn", "label": "测试连接", "variant": "secondary" }
     ]
   }
   ```
8. **`metrics`（多列数据指标宫格）**：不含 `id`/`label`，仅含非空 `metrics` 数组（每项 `label`、`value` 必填非空，`tone` 可选，取值见柔和色板键；宿主按数量与视口宽度自动渲染为 1–4 列等宽卡片）：
   ```json
   {
     "kind": "metrics",
     "metrics": [
       { "label": "笔记篇数", "value": "128", "tone": "notes" },
       { "label": "附件体积", "value": "14.2 MB", "tone": "assets" }
     ]
   }
   ```
9. **`kv`（只读状态键值行）**：`rows` 为非空数组，每行 `label` 与 `value` 均必填非空。若 `ui_capabilities` 包含 `kv_tooltip`，可加 `tooltip` 在悬停时展示补充信息（勿放密码或令牌）：
   ```json
   {
     "kind": "kv",
     "rows": [
       { "label": "当前笔记本", "value": "团队笔记", "tooltip": "/Users/name/Documents/团队笔记" }
     ]
   }
   ```
10. **`preview`（实时视觉预览卡片）**：不含 `id`/`label`，必填 `preview` 对象：
    - `variant`：`"article"`（最多显示 14 条非空行）| `"cards"`（每卡最多 5 条非空行；`cards` 为空时回退 `article`）| `"diff"`（`before`/`after`/`body` 各最多 6 条非空行）| `"plan"`（默认，最多 10 条非空行）
    - `palette`：可选 `"ocean"`（默认）| `"paper"`（浅色纸感）| `"ink"`（暗色墨夜）| `"warm"`（暖纸衬线）。`article` / `cards` / `diff` 按所选纸面绘制；`plan` 在深色主题下若选 `paper` 或 `warm`，列表改跟应用底色，避免浅色字落在白底上。
    - `title`：必填；`badge`、`subtitle` 可选
    - `body`：多行正文（`"article"` 支持 `# `、`## `、`> `、`[1] ` 脚注行）
    - `before` / `after`：用于 `"diff"` 对照文本
    - `cards`：用于 `"cards"` 的卡片数组 `[{"badge": "封面", "title": "...", "body": "..."}]`
    ```json
    {
      "kind": "preview",
      "preview": {
        "variant": "article",
        "palette": "paper",
        "title": "实时排版预览 · 欢迎使用 Block",
        "badge": "海盐纸感 · 单文件 HTML",
        "subtitle": "欢迎使用 Block.html",
        "body": "# 欢迎使用 Block\n## 核心特性\n> 模块化无侵入插件体系"
      }
    }
    ```
11. **`log_list`（运行记录 / 队列 / 占比列表）**：`empty_text` 必填非空，`label` 可选，`entries` 可为空数组。每项 `title` 必填，其余可选：
    - `sub_text`：标题旁的次级说明
    - `status` / `time`：右侧自动对齐的等宽列；若**全部条目**均传 `"status": "→"`，宿主切换为 `title → time` 双栏映射样式（隐藏 `sub_text` 与前导状态点，保留占比条与 `tooltip`）
    - `tone`：前导状态点支持语义色（`"success"` | `"warning"`/`"warn"` | `"danger"`/`"error"` | `"info"`/`"accent"`）与柔和色板键；`ratio` 占比条仅使用柔和色板键（传 `"success"` 等语义色会回退中性底色）
    - `ratio`：约定传 `0`–`1` 浮点数（`<= 0` 不画条，`> 0` 渲染时夹到 `4%`–`100%` 并隐藏前导状态点）
    - `tooltip`：整行悬停提示
    - `action_id`：可选点击动作 ID（`ui_capabilities` 含 `log_actions` 时生效，<= 128 字符、禁止 Emoji；点击该行时触发当前表面的 `plugin.action`，常配合 `host.notes.open` 实现侧栏/面板一键跳转笔记）
    - **垂直时间轴模式（需 `ui_capabilities` 含 `log_timeline`）**：控件级传 `"variant": "timeline"`（省略或 `"list"` 为默认列表）。此时每项支持可选扩展字段：
      - `kind`：`""` / `"event"`（圆点事件节点）或 `"span"`（虚线折叠静默区间卡片）
      - `badge`：`span` 卡片左侧胶囊标签（如 `"静默 44 分钟"`）
      - `span`：`span` 卡片右侧等宽时间段（如 `"16:09 → 16:53"`）
      - `tags`：普通事件下方流式排列的紧凑标签数组（最多 24 项，适合聚合展示同批变动的文件/附件）
      - `details`：`span` 卡片点击展开后的时间点/明细数组（最多 64 项，点击卡片自动在前端切换展开与收起）
    ```json
    {
      "kind": "log_list",
      "empty_text": "暂无同步记录",
      "entries": [
        { "title": "welcome.md", "sub_text": "Work", "time": "12:30", "status": "成功", "tone": "success" }
      ]
    }
    ```
12. **`checklist`（多选勾选列表）**：`id` 必填，`label`、`empty_text` 可选，`value` 为已选 `string[]`（省略或 `null` 归一化为 `[]`，不会在解码时过滤旧选项值），`options` 每项含 `value`（必填唯一）、`label`（必填）及可选 `detail` / `badge` / `tooltip`；多于 1 项时宿主自动提供「全选 / 取消全选」。注意 `checklist` 只能放在卡片顶层，不能放入 `group`。
    - **`add_folder`（可选 `bool`，仅限 `checklist`，需 `"checklist_add_folder"` 能力）**：改为「用户添加的文件夹列表」。宿主在卡片标题栏右上角（或控件自身标题行右侧）显示加号按钮，点击弹出系统目录选择器；选中的目录会登记为宿主笔记本（不切换当前笔记本，`host.notes.*` 因此可访问），以绝对路径追加到 `value` 并立即触发 `plugin.render`。列表只渲染 `value` 中存在对应 `options` 的项，每行没有勾选框，行尾提供移除按钮（仅从 `value` 移除，不会删除笔记本或文件）。插件应让 `options` 只包含 `value` 内的路径；`value` 为空时显示 `empty_text`：
      ```json
      {
        "kind": "checklist",
        "id": "selected_notebooks",
        "add_folder": true,
        "value": ["/Users/name/Notes/Work"],
        "empty_text": "点击右上角 + 添加要同步的文件夹",
        "options": [
          { "value": "/Users/name/Notes/Work", "label": "Work", "detail": "~/Notes/Work", "tooltip": "/Users/name/Notes/Work" }
        ]
      }
      ```
    - 普通勾选列表示例：
    ```json
    {
      "kind": "checklist",
      "id": "selected_notebooks",
      "value": ["/Users/name/Notes/Work"],
      "empty_text": "暂无已打开的笔记本",
      "options": [
        {
          "value": "/Users/name/Notes/Work",
          "label": "Work",
          "badge": "当前",
          "detail": "~/Notes/Work",
          "tooltip": "/Users/name/Notes/Work"
        }
      ]
    }
    ```
13. **`group`（并排表单分组）**：`fields` 至少包含 2 个表单项（仅允许 `input`、`secret`、`segmented`、`select`、`folder`、`switch`），宿主自动并排展示；未显式设 `layout`（或 `layout: ""`）的子项默认按 `"stacked"` 排布（`switch` 仍横排），各子项可独立声明 `visible_when`。
14. **`treemap`（磁盘/空间分布图，固定 Bento 布局按字节排序，非严格面积比例）**：`tiles` 为必填非空数组，`id`（同页有多个图时须显式传不同 `id` 以免共享选中状态）、`total_bytes`（非负整数 `uint64`，`0` 或省略时取各瓦片 `bytes` 之和，求和采用 `uint64` 饱和加法防止溢出归零）、`summary` 可选：
    ```json
    {
      "kind": "treemap",
      "id": "storage-map",
      "total_bytes": 15728640,
      "summary": {
        "total_label": "总占用",
        "total_value": "15.0 MB",
        "sub_text": "共 2 类数据",
        "metrics": [{ "label": "文件数", "value": "140" }]
      },
      "tiles": [
        {
          "id": "notes",
          "label": "笔记",
          "detail": "Markdown 正文",
          "bytes": 10485760,
          "file_count": 120,
          "color": "notes",
          "entries": [
            { "name": "Work", "path": "/abs/path/Work", "bytes": 6291456, "count": 72 }
          ]
        },
        {
          "id": "assets",
          "label": "附件",
          "detail": "图片与文档",
          "bytes": 5242880,
          "file_count": 20,
          "color": "assets"
        }
      ]
    }
    ```
    其中 `total_bytes` 与两级 `bytes` 均为无符号整数（负数或小数会解码失败）；`summary` 若存在，`total_value` 必填（`summary.metrics[].tone` 可解码但渲染不消费）；每个 `tile` 的 `id`（唯一）与 `label` 必填，`color` 默认取 `tile.id`（柔和色板键）；每条 `entries` 的 `name` 必填，`path`/`count` 可选（`count <= 0` 汇总时按 `1` 计数）。

15. **`icon_strip`（图标条预览，需 `"item_icons"` 能力）**：不含 `id`，`icons` 为图标数组（格式同 `switch.icon`，最多 32 个，可各带 `tooltip`），宿主按顺序右对齐绘制成顶栏样式的一行；`label` 可选（显示在左侧），`icons` 为空时显示可选的 `empty_text`。不能放入 `group`：
    ```json
    {
      "kind": "icon_strip",
      "icons": [{ "name": "message-square", "tooltip": "反馈" }, { "name": "more", "tooltip": "页面操作" }],
      "empty_text": "未显示任何图标"
    }
    ```

16. **`progress`（独立进度条，需 `"progress_bar"` 能力）**：用于为插件内不同页面或目标展示独立状态与同步/任务进度条；`label` 与 `status` 至少提供一项，`progress` 为可选 `0`–`1` 静态进度，`tone` 为可选状态色（`""` | `"accent"` | `"info"` | `"success"` | `"warning"` | `"danger"`），`tooltip` 为可选悬停详情，`channel` 为可选实时通道 ID（在 `plugin.action` 期间自动接收同通道的 `host.action.progress` 更新并在未定计数阶段展示平滑扫动进度条）：
    ```json
    {
      "kind": "progress",
      "id": "progress_git",
      "channel": "git",
      "label": "Git 仓库同步进度",
      "status": "17:32 · 已是最新",
      "progress": 1.0,
      "tone": "success"
    }
    ```

---

## 5. Python 标准库双向通信骨架

```python
#!/usr/bin/env python3
import json
import sys

if hasattr(sys.stdin, "reconfigure"):
    sys.stdin.reconfigure(encoding="utf-8")
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")


class HostRPCError(RuntimeError):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code
        self.message = message


class HostBridge:
    def __init__(self):
        self._seq = 1000

    def send(self, obj):
        sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
        sys.stdout.flush()

    def notify_host(self, method, params=None):
        self.send({"jsonrpc": "2.0", "method": method, "params": params or {}})

    def call_host(self, method, params=None):
        self._seq += 1
        req_id = f"p-{self._seq}"
        self.send({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})
        while True:
            line = sys.stdin.readline()
            if not line:
                raise RuntimeError("stdin closed while awaiting host response")
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                continue
            if not isinstance(msg, dict):
                continue
            if str(msg.get("id")) == req_id:
                if "error" in msg and msg["error"]:
                    err = msg["error"]
                    raise HostRPCError(err.get("code", -32000), err.get("message", "host error"))
                return msg.get("result", {})

    def serve(self, handler):
        while True:
            line = sys.stdin.readline()
            if not line:
                break
            line = line.strip()
            if not line:
                continue
            try:
                req = json.loads(line)
            except json.JSONDecodeError:
                continue
            if not isinstance(req, dict):
                continue
            method = req.get("method")
            if not isinstance(method, str) or not method:
                continue
            req_id = req.get("id")
            params = req.get("params")
            if not isinstance(params, dict):
                params = {}
            try:
                result = handler(self, method, params)
                if req_id is not None:
                    self.send({"jsonrpc": "2.0", "id": req_id, "result": result})
                if method == "plugin.shutdown":
                    break
            except Exception as exc:
                if req_id is not None:
                    self.send({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32000, "message": str(exc)}})
```

<!-- ai-guide:skip-start -->
---

下一步：[4. 本地协议检查与发布到应用商店 →](check-and-store.md)
<!-- ai-guide:skip-end -->
