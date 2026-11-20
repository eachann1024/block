<!-- ai-guide:skip-start -->
# UI Contributions（宿主界面扩展贡献）

> [← 检查与上架](check-and-store.md) · [文档中心](../README.md) · [语言扩展 →](languages.md)

---
<!-- ai-guide:skip-end -->

## 8. UI contributions（宿主界面扩展贡献）

插件可通过 `manifest.json` 的 `contributes` 字段在宿主设置页之外挂载顶栏按钮、内置设置标签页分区、命令/快捷键、页面/编辑器菜单、右侧边栏面板、状态栏条目与弹窗，并按需申请对应的 UI 权限与编辑器/界面事件权限。

### 8.1 新增权限（9 项）

| 权限枚举 | 说明 | 关联贡献 / 能力 |
| --- | --- | --- |
| `"ui:titlebar"` | 顶栏按钮与顶栏槽位显隐控制 | `contributes.titlebar_buttons`、`host.titlebar.get_items` / `set_hidden` / `set_button` |
| `"ui:settings"` | 内置设置标签页扩展分区 | `contributes.settings_sections`、`host.ui.editor_background` / `set_editor_background`、`host.ui.tarot_state` / `set_tarot` |
| `"ui:commands"` | 命令面板、快捷键与菜单项 | `contributes.commands`、`contributes.page_menu`、`contributes.editor_menu` |
| `"ui:panels"` | 右侧边栏面板 | `contributes.panels`、`host.ui.open_panel` / `close_panel` |
| `"ui:status"` | 底部状态栏条目 | `contributes.status_items`、`host.status.set` |
| `"ui:notify"` | Toast 轻提示与插件弹窗 | `contributes.dialogs`、`host.ui.toast`、`host.ui.open_dialog` / `close_dialog` |
| `"editor:read"` | 读取当前编辑器选区 | `host.editor.selection` |
| `"editor:write"` | 替换选区或光标处插入文本 | `host.editor.insert` |
| `"events:workspace"` | 订阅工作区界面事件 | `plugin.event`（`note.opened`、`theme.changed`、`locale.changed`） |

### 8.2 `manifest.contributes` 字段定义

通用校验规则：
- 所有贡献项 `id` 必须匹配正则 `^[a-z][a-z0-9_-]{0,31}$`，且在同一类别内不可重复。
- 所有 `LocalizedText`（`title`、`tooltip` 等）均须同时提供非空 `zh` 与 `en`，且禁止包含 Emoji；所有 `icon`（`DuoIconSpec`）须满足 `view_box == "0 0 24 24"` 且至少包含 1 条 `stroke_paths`；三层路径合计最多 16 条、路径数据合计最多 4096 字节。
- 数量上限：`settings_sections` 8、`commands` 64、`page_menu` 16、`editor_menu` 16、`panels` 8、`status_items` 4、`dialogs` 16。
- 只要声明了某类非空贡献数组，`manifest.permissions` 中就必须包含对应的权限。

1. **`titlebar_buttons`**（需 `"ui:titlebar"`，最多 **3** 个）：在顶栏右侧挂载图标按钮。所有插件的按钮合计最多直接显示 **3** 个（窗口宽度小于 900 时 1 个，小于 720 时 0 个）；用户可在「更多插件」菜单（悬停星标或选中后按 `P`）或右键顶栏插件图标固定常用按钮（最多固定 3 个，优先展示；槽位满时优先回收已停用/卸载项，有效项满 3 个时替换最早固定项并提示），其余按声明顺序收进顶栏的「更多插件」菜单（带计数角标、插件图标与活跃/角标提示）。菜单行文字取 `tooltip`（缺省为 `id`），因此务必填写可读的 `tooltip`。
   - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（必填 `LocalizedText`）。
   - `command`（可选）与 `panel`（可选）：**必须且只能二选一**，分别引用同清单 `commands[].id` 或 `panels[].id`（点击时执行命令或切换面板）。
2. **`settings_sections`**（需 `"ui:settings"`）：在内置设置标签页末尾追加一张声明式设置分区卡片（单卡片标题与分区标题一致时宿主自动去重）。
   - `id`（必填）、`title`（必填 `LocalizedText`）。
   - `tab`（必填）：仅允许 `"ai"` | `"appearance"` | `"folders"` | `"shortcuts"`。
   - 对应渲染表面：`"settings_section:<id>"`。
3. **`commands`**（需 `"ui:commands"`）：注册到命令面板并支持默认快捷键。
   - `id`（必填）、`title`（必填 `LocalizedText`）。
   - `shortcut`（可选字符串）：`shortcuts.go` 规格形式，如 `"cmd-shift-t"`。宿主与编辑器已占用的组合（如 `cmd-f`、`cmd-b`、`cmd-s`、`cmd-1`…`cmd-9`）不会触发插件命令。
4. **`page_menu`**（需 `"ui:commands"`）：向页面右上角 `…` 菜单追加条目。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填，须引用已声明的 `commands[].id`）。
5. **`editor_menu`**（需 `"ui:commands"`）：向编辑器右键菜单追加条目。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填，须引用已声明的 `commands[].id`）。
6. **`panels`**（需 `"ui:panels"`）：注册右侧边栏面板。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）。
   - 对应渲染表面：`"panel:<id>"`。
7. **`status_items`**（需 `"ui:status"`）：注册底部状态栏条目。
   - `id`（必填）、`text`（可选 `LocalizedText`，作为 `host.status.set` 覆盖前的初始文案；解析后为空白时不显示）、`command`（可选，非空时须引用 `commands[].id`）。
8. **`dialogs`**（需 `"ui:notify"`）：注册模态弹窗（由 `host.ui.open_dialog` 打开）。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`width`（可选整数；`0` 或省略默认 `460`，显式指定时须在 `320`–`720` 之间）。
   - 对应渲染表面：`"dialog:<id>"`。

### 8.3 多表面（Surfaces）渲染与状态隔离

- `manifest.surfaces` 仍固定填写 `["settings_page"]`；`settings_sections`、`panels`、`dialogs` 会自动启用下列前缀表面：
  - `"settings_section:<id>"`
  - `"panel:<id>"`
  - `"dialog:<id>"`
- 宿主调用 `plugin.render` 与 `plugin.action` 时会在 `params.surface` 中传入对应的表面标识，插件按 `surface` 返回各自独立的 `UIPage` 与 `ActionResult`。
- 宿主按 `(plugin_id, notebook_root, surface)` 分别隔离加载态、忙碌/反馈状态、表单值、定时刷新（`refresh_interval_ms`）与笔记本切换感知；停用或卸载插件时会销毁其全部表面视图。

### 8.4 `plugin.command` 与工作区 `plugin.event`

1. **`plugin.command`**（Host -> Plugin）
   - 触发来源：命令面板、快捷键、顶栏按钮、页面菜单、编辑器菜单或状态栏条目（30 秒超时；若插件未启动，宿主会自动拉起）。
   - 请求 `params`：
     ```json
     {
       "command_id": "toggle_focus",
       "context": {
         "note": "welcome.md",
         "selection": "选中的文本",
         "source": "palette"
       }
     }
     ```
     - `context.note`（可选，缺省时由宿主填充当前活动笔记相对路径）、`context.selection`（可选，仅当插件持有 `"editor:read"` 时下发）、`context.source`（`"palette"` | `"shortcut"` | `"titlebar"` | `"page_menu"` | `"editor_menu"` | `"status"`）。
   - 返回 `result`：`{"ok": true, "tone": "info", "status": "已执行"}`（字段均可选；`status` 非空时宿主会弹出对应 `tone` 的 Toast 提示，默认 `"info"`）。

2. **工作区 `plugin.event`**（Host -> Plugin，需 `"events:workspace"`）
   - 仅向**已启动运行中**的插件派发（5 秒超时），宿主**不会**为这类事件冷启动已停止的插件。
   - 请求 `params`：`{"event": "<事件名>", "timestamp": "2026-11-20T02:00:00Z", ...}`
     - 支持事件：`"note.opened"`、`"theme.changed"`、`"locale.changed"`（附加字段随事件携带）。
   - 返回 `result`：`{"handled": true}`（可选）。

### 8.5 UI 贡献宿主方法（Plugin -> Host）

1. **`host.titlebar.get_items`**（需 `"ui:titlebar"`，参数 `{}`）
   - 返回：`{"items": [{"id": "feedback", "label": "反馈", "locked": false, "hidden": false, "plugin": ""}]}`
   - 内置槽位 ID：`"feedback"`、`"ai"`、`"send_all"`、`"wrap"`、`"zoom"`、`"reveal"`、`"more"`（`locked: true`）；插件按钮槽位 ID 为 `"plugin:<pluginID>/<buttonID>"`。
2. **`host.titlebar.set_hidden`**（需 `"ui:titlebar"`）
   - 参数：`{"hidden": ["feedback", "plugin:my-plugin/btn"]}`（按插件隔离持久化于 `<BlockAppData>/plugins/contrib-state.json`，停用插件时自动收回其隐藏设置）。
   - **不可隐藏约束**：内置槽位 `"more"`（页面 `…` 操作按钮）永久锁定，**永远不可隐藏**（传入 `"more"` 或未知槽位 ID 会被宿主忽略）。
   - 返回：`{"ok": true, "items": [...]}`（返回更新后的完整槽位列表）。
3. **`host.titlebar.set_button`**（需 `"ui:titlebar"`）
   - 参数：`{"id": "<button_id>", "badge": "3", "active": true}`（`id` 必填且须为本插件声明的 `titlebar_buttons[].id`；`badge` 可选，会去首尾空白并截断至最多 3 个 Unicode 字符；`active` 可选布尔值）。
   - 返回：`{"ok": true}`。
4. **`host.status.set`**（需 `"ui:status"`）
   - 参数：`{"id": "<item_id>", "text": "Synced", "tooltip": "上次同步 10:00"}`（`id` 必填且须为本插件声明的 `status_items[].id`；`text` 禁止含 Emoji 并截断至 40 个字符；`tooltip` 截断至 200 个字符）。
   - 返回：`{"ok": true}`。
5. **`host.ui.toast`**（需 `"ui:notify"`）
   - 参数：`{"message": "操作完成", "tone": "info"}`（`message` 必填非空白，截断至 160 个字符；`tone` 仅接受 `"success"` | `"error"` | `"warning"`，其余归一化为 `"info"`）。
   - 返回：`{"ok": true}`。
6. **`host.ui.open_dialog` / `host.ui.close_dialog`**（需 `"ui:notify"`）
   - 参数：`{"id": "<dialog_id>"}`（`id` 须为本插件声明的 `dialogs[].id`）。
   - 返回：`{"ok": true}`。
7. **`host.ui.open_panel` / `host.ui.close_panel`**（需 `"ui:panels"`）
   - 参数：`{"id": "<panel_id>"}`（`id` 须为本插件声明的 `panels[].id`）。
   - 返回：`{"ok": true}`。
8. **`host.ui.refresh`**（仅要求目标表面属于 `"settings_page"` 或本插件 `contributes` 声明的表面）
   - 参数：`{"surface": "panel:outline"}`（异步触发对应表面的后台 `plugin.render` 重绘；用户在当前笔记本中尚未打开过的表面不会渲染，首次打开时自然加载最新内容）。
   - 返回：`{"ok": true}`。
9. **`host.editor.selection`**（需 `"editor:read"`，参数 `{}`）
   - 返回：`{"note": "welcome.md", "text": "选中文本", "has_focus": true}`（编辑器不可用时返回 `-32005`）。
10. **`host.editor.insert`**（需 `"editor:write"`）
    - 参数：`{"text": "插入内容", "note": "welcome.md"}`（`note` 可选：给定时只有该笔记仍是活动笔记才写入，否则返回 `-32004`；在 `plugin.command` 处理期间省略时默认绑定发起命令的笔记。替换当前选区或在光标处插入；`text` 上限 4 MiB，超限返回 `-32602`，宿主没有编辑器返回 `-32005`；没有打开的笔记或插入失败返回 `-32004`）。
    - 返回：`{"ok": true}`。
11. **`host.ui.tarot_state` / `host.ui.set_tarot`**（需 `"ui:settings"`、`"ui:commands"` 或 `"ui:titlebar"` 任一权限）
    - `host.ui.tarot_state`（参数 `{}`）：返回 `{"supported": true, "enabled": false, "room": "writing", "collected": 3, "total": 22, "daily_drawn": false, "daily_note": "", "active_note": ""}`。
    - `host.ui.set_tarot`（参数 `{"enabled": true, "toggle": false, "room": "writing"|"table"|"daily"|"deck"}`，字段均可选）：切换宿主原生塔罗模式（午夜铜版）或直接进入指定房间（从设置页指定 `room` 时自动收起设置），返回最新状态对象。
12. **`host.ui.editor_background` / `host.ui.set_editor_background`**（需 `"ui:settings"`、`"ui:commands"` 或 `"ui:titlebar"` 任一权限）
    - `host.ui.editor_background`（参数 `{}`）：返回当前插件设置的编辑器背景状态 `{"mode": "off"|"dots"|"grid", "contrast": "standard"|"high"}`。
    - `host.ui.set_editor_background`（参数 `{"mode": "dots", "contrast": "standard"}`）：设置与编辑器行高对齐的点阵（`"dots"`）、方格（`"grid"`）或关闭（`"off"`）背景及对比度（`"standard"` | `"high"`，兼容 `"strong"` 归一化为 `"high"`），持久化于 `contrib-state.json` 并按插件隔离；停用插件时自动失效，返回归一化后的状态对象。

<!-- ai-guide:skip-start -->
---

下一步：[6. 语言扩展规范 →](languages.md)
<!-- ai-guide:skip-end -->
