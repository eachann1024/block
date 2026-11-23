<!-- ai-guide:skip-start -->
# UI Contributions（宿主界面扩展贡献）

> [← 检查与上架](check-and-store.md) · [文档中心](../README.md) · [语言扩展 →](languages.md)

---
<!-- ai-guide:skip-end -->

## 8. UI contributions（宿主界面扩展贡献）

插件可通过 `manifest.json` 的 `contributes` 字段在宿主设置页之外挂载顶栏按钮、内置设置标签页分区、命令/快捷键、页面/编辑器/侧栏/标签页右键菜单、左侧栏分区、文件树行角标、编辑区自定义矢量背景、右侧边栏面板、状态栏条目、弹窗与系统菜单栏图标，并按需申请对应的 UI 权限、编辑器/界面事件权限与文件路径权限。

### 8.1 新增权限（13 项）

| 权限枚举 | 说明 | 关联贡献 / 能力 |
| --- | --- | --- |
| `"ui:titlebar"` | 顶栏按钮与顶栏槽位显隐控制 | `contributes.titlebar_buttons`、`host.titlebar.get_items` / `set_hidden` / `set_button` |
| `"ui:settings"` | 内置设置标签页扩展分区 | `contributes.settings_sections`、`host.ui.editor_background` / `set_editor_background`、`host.ui.tarot_state` / `set_tarot` |
| `"ui:commands"` | 命令面板、快捷键与菜单项 | `contributes.commands`、`contributes.page_menu`、`contributes.editor_menu`、`contributes.tab_menu` |
| `"ui:panels"` | 右侧边栏面板 | `contributes.panels`、`host.ui.open_panel` / `close_panel` / `toggle_panel` |
| `"ui:status"` | 底部状态栏条目 | `contributes.status_items`、`host.status.set` |
| `"ui:menubar"` | 系统菜单栏图标（macOS 菜单栏、Windows / Linux 通知区域），含菜单与拖放 | `contributes.menubar_items`、`host.menubar.set` |
| `"ui:notify"` | 应用内全局通知与插件弹窗 | `contributes.dialogs`、`host.ui.toast`、`host.ui.open_dialog` / `close_dialog` |
| `"ui:sidebar"` | 左侧栏分区、文件树角标与侧栏右键菜单 | `contributes.sidebar_sections`、`contributes.file_badges`、`contributes.sidebar_menu`、`host.sidebar.set_badges` / `get_badges` |
| `"ui:appearance"` | 编辑区自定义矢量背景（水印/平铺图案） | `contributes.backdrops`、`host.ui.backdrop` / `set_backdrop` |
| `"editor:read"` | 读取当前编辑器选区 | `host.editor.selection` |
| `"editor:write"` | 替换选区或光标处插入文本 | `host.editor.insert` |
| `"events:workspace"` | 订阅工作区界面事件 | `plugin.event`（`note.opened`、`theme.changed`、`locale.changed`） |
| `"notes:path"` | 读取命令目标文件的本机绝对路径（含笔记本外打开的文件），不读内容 | `plugin.command` 的 `context.file` |

### 8.2 `manifest.contributes` 字段定义

通用校验规则：
- 所有贡献项 `id` 必须匹配正则 `^[a-z][a-z0-9_-]{0,31}$`，且在同一类别内不可重复。
- 所有 `LocalizedText`（`title`、`tooltip` 等）均须同时提供非空 `zh` 与 `en`，且禁止包含 Emoji；所有 `icon`（`DuoIconSpec`）须满足 `view_box == "0 0 24 24"` 且至少包含 1 条 `stroke_paths`；三层路径合计最多 16 条、路径数据合计最多 4096 字节。
- 数量上限：`titlebar_buttons` 3、`settings_sections` 8、`commands` 64、`page_menu` 16、`editor_menu` 16、`sidebar_menu` 16、`tab_menu` 16、`panels` 8、`sidebar_sections` 4、`file_badges` 4、`backdrops` 8、`status_items` 4、`dialogs` 16、`menubar_items` 2。
- 只要声明了某类非空贡献数组，`manifest.permissions` 中就必须包含对应的权限。

1. **`titlebar_buttons`**（需 `"ui:titlebar"`，最多 **3** 个）：在顶栏右侧挂载图标按钮。顶栏有空位时所有插件按钮都直接显示，仅在空间不足时把放不下的按钮收起；用户可在「更多插件」菜单（点星标或在选中/悬停行按 `P`）或右键/聚焦顶栏插件图标（按 `P` / `Shift+F10`）固定常用按钮（最多固定 3 个，优先展示；槽位满时优先回收已停用/卸载项，有效项满 3 个时替换最早固定项并提示），其余按声明顺序收进顶栏的「更多插件」菜单（带计数角标、插件图标与活跃/角标提示）。菜单行文字取 `tooltip`（缺省为 `id`），因此务必填写可读的 `tooltip`。
   - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（必填 `LocalizedText`）。
   - `command`（可选）与 `panel`（可选）：**必须且只能二选一**，分别引用同清单 `commands[].id` 或 `panels[].id`（点击时执行命令或切换面板）。
2. **`settings_sections`**（需 `"ui:settings"`）：在内置设置标签页末尾追加一张声明式设置分区卡片（单卡片标题与分区标题一致时宿主自动去重）。
   - **允许但不建议**：插件设置应放在插件自己的设置页（`manifest.surfaces: ["settings_page"]`），用户在插件页里就能找到全部选项；只有确实属于宿主某个内置页的单项（极少数）才考虑用本扩展点，且不要把同一组设置在两处重复展示。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - `tab`（必填）：仅允许 `"ai"` | `"appearance"` | `"folders"` | `"shortcuts"`。
   - 对应渲染表面：`"settings_section:<id>"`。
3. **`commands`**（需 `"ui:commands"`）：注册到命令面板并支持默认快捷键。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - `shortcut`（可选字符串）：`shortcuts.go` 规格形式，如 `"cmd-alt-p"`。宿主与编辑器已占用的组合（如 `cmd-f`、`cmd-b`、`cmd-s`、`cmd-shift-p`、`cmd-shift-k`、`cmd-1`…`cmd-9`）不会触发插件命令。
   - **宿主统一快捷键托管**：只要插件声明了 `commands`，宿主会自动将其纳入「设置 / 快捷键」（按插件自动分组，支持按「全部 / 内置 / 插件」筛选），并在该插件独立设置页底部自动注入「快捷键」分组；用户可直接点击录键、按 `Backspace`/`Delete` 清空或一键恢复默认，自定义结果会自动同步到快捷键分发、命令面板以及侧栏/页面/标签页右键菜单，插件无需也不应自行绘制快捷键设置 UI。
4. **`page_menu`**（需 `"ui:commands"`）：向页面右上角 `…` 菜单追加条目。Markdown 笔记、代码/HTML/SVG 源码与图片预览等所有页面都会显示。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填，须引用已声明的 `commands[].id`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
5. **`editor_menu`**（需 `"ui:commands"`）：向编辑器右键菜单追加条目（渲染原生彩色 SVG 图标）。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填，须引用已声明的 `commands[].id`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
6. **`sidebar_menu`**（需 `"ui:sidebar"`，且 `command` 引用已声明的 `commands[].id`）：向左侧栏文件树右键菜单追加条目。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - `targets`（可选字符串数组，仅 `sidebar_menu` 可用）：`"note"` | `"folder"`，不可重复；省略时为 `["note"]`（只在笔记行显示）。声明 `["note", "folder"]` 可同时出现在文件夹行。
   - 触发时 `context.source` 为 `"sidebar_menu"`；右键笔记时 `context.note` 为其相对路径，右键文件夹时改为 `context.folder`（文件夹相对路径，`note` 为空）。
6.1. **`tab_menu`**（需 `"ui:commands"`，且 `command` 引用已声明的 `commands[].id`）：向顶栏标签页右键菜单顶部追加条目。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - 触发时 `context.source` 为 `"tab_menu"`，目标是被右键的标签（不一定是当前活动标签）。目标在当前笔记本内时 `context.note` 为其相对路径；笔记本外打开的文件 `note` 为空，只能通过 `context.file`（需 `"notes:path"`）拿到路径。
7. **`panels`**（需 `"ui:panels"`）：注册右侧边栏面板（标题栏自动渲染 `icon`）。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）。
   - 对应渲染表面：`"panel:<id>"`。
8. **`sidebar_sections`**（需 `"ui:sidebar"`，最多 **4** 个）：在左侧栏文件树下方、笔记本切换器上方追加紧凑可折叠声明式卡片。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）、`collapsible`（可选 bool）、`default_collapsed`（可选 bool）。
   - 对应渲染表面：`"sidebar_section:<id>"`。
9. **`file_badges`**（需 `"ui:sidebar"`，最多 **4** 个）：声明左侧栏文件树笔记行尾角标图标，运行时通过 `host.sidebar.set_badges` 按笔记相对路径推送（单行最多展示 2 个角标，停用或卸载插件立即清除）。
   - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（可选 `LocalizedText`）。
10. **`backdrops`**（需 `"ui:appearance"`，最多 **8** 个）：声明编辑区自定义矢量背景图案，运行时通过 `host.ui.set_backdrop` 启用、切换或调节（宿主自动钳制透明度在 `0.03`–`0.25` 以保证正文清晰可读，停用或卸载插件立即消失）。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）。
    - `pattern`（可选：`"scatter"` | `"grid"` | `"watermark"` | `"corners"`，默认 `"scatter"`）、`density`（可选：`"sparse"` | `"medium"` | `"dense"`，默认 `"medium"`）、`opacity`（可选：`0.01`–`0.35`，渲染钳制 `0.03`–`0.25`，默认 `0.09`）、`tint`（可选：`"accent"` | `"muted"` | `"warm"` | `"glow"`，默认 `"accent"`）、`dark_glow`（可选 bool，深色主题下提亮为夜光质感）。
11. **`status_items`**（需 `"ui:status"`）：注册底部状态栏条目。
    - `id`（必填）、`text`（可选 `LocalizedText`，作为 `host.status.set` 覆盖前的初始文案；解析后为空白时不显示）、`command`（可选，非空时须引用 `commands[].id`）、`icon`（可选 `DuoIconSpec`）。
12. **`dialogs`**（需 `"ui:notify"`）：注册模态弹窗（由 `host.ui.open_dialog` 打开，标题栏渲染图标）。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`width`（可选整数；`0` 或省略默认 `460`，显式指定时须在 `320`–`720` 之间）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
    - 对应渲染表面：`"dialog:<id>"`。
13. **`menubar_items`**（需 `"ui:menubar"`，最多 **2** 个）：在系统菜单栏放一个图标，Block 窗口不在前台也能用，适合截图上传、快速记录这类随手操作。插件启用后图标出现，停用或卸载后消失。
    - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（必填 `LocalizedText`）。菜单栏图标按系统模板图只画 `stroke_paths` 与 `accent_paths` 的线条，`fill_paths` 不绘制。
    - 点击行为：`command`（点击直接执行，须引用 `commands[].id`）、`menu`（点击弹出菜单，最多 16 条）、`drop`（接收拖放）。`command` 与 `menu` 不能同时声明；三者全省略时校验失败。插件声明了 `settings_page` 且图标有菜单时，菜单末尾自动追加「插件设置…」。
    - `menu[]` 条目：`{"id", "title": LocalizedText, "command", "input"}` 或分隔线 `{"separator": true}`（分隔线不能带其他字段）。`input` 仅接受 `"clipboard"`：点击时宿主读取剪贴板，按 `drop` 的规则过滤后随命令下发（未声明 `drop` 时接收文件、图片与文本）；剪贴板里没有可接收的内容时不执行命令并提示用户。
    - `drop`：`{"command", "accept": ["files", "image", "text"], "extensions": ["png", "jpg"]}`。`command` 必填；`accept` 省略时为 `["files", "image"]`；`extensions` 为小写、不带点的扩展名（最多 32 个），只限制 `files`，省略时接收任意普通文件。拖放目前仅 macOS 支持（以 `ui_capabilities` 含 `menubar_drop` 为准），其余平台图标与菜单照常工作。
    - 被引用的命令同样出现在命令面板，从命令面板触发时没有 `context.files` 等菜单栏字段，插件需自行兜底。
    - 示例：
      ```json
      "menubar_items": [{
        "id": "uploader",
        "icon": {"view_box": "0 0 24 24", "stroke_paths": ["M12 16V5", "M8 9l4-4 4 4", "M5 19h14"]},
        "tooltip": {"zh": "拖入图片即上传", "en": "Drop images to upload"},
        "menu": [{"id": "clipboard", "title": {"zh": "上传剪贴板", "en": "Upload Clipboard"}, "command": "upload", "input": "clipboard"}],
        "drop": {"command": "upload", "accept": ["files", "image"], "extensions": ["png", "jpg", "jpeg", "gif", "webp"]}
      }]
      ```

### 8.3 多表面（Surfaces）渲染与状态隔离

- 需要独立设置页时在 `manifest.surfaces` 中填写 `["settings_page"]`（无独立设置页的插件可省略）；`settings_sections`、`panels`、`sidebar_sections`、`dialogs` 会自动启用下列前缀表面：
  - `"settings_section:<id>"`
  - `"panel:<id>"`
  - `"sidebar_section:<id>"`
  - `"dialog:<id>"`
- 宿主调用 `plugin.render` 与 `plugin.action` 时会在 `params.surface` 中传入对应的表面标识，插件按 `surface` 返回各自独立的 `UIPage` 与 `ActionResult`。
- 宿主按 `(plugin_id, notebook_root, surface)` 分别隔离加载态、忙碌/反馈状态、表单值、定时刷新（`refresh_interval_ms`）与笔记本切换感知；停用或卸载插件时会销毁其全部表面视图。

### 8.4 `plugin.command` 与工作区 `plugin.event`

1. **`plugin.command`**（Host -> Plugin）
   - 触发来源：命令面板、快捷键、顶栏按钮、页面菜单、编辑器菜单、侧栏右键菜单、标签页右键菜单、状态栏条目或系统菜单栏图标（30 秒超时；若插件未启动，宿主会自动拉起）。
   - 请求 `params`：
     ```json
     {
       "command_id": "toggle_focus",
       "context": {
         "note": "welcome.md",
         "file": "/abs/path/to/notebook/welcome.md",
         "selection": "选中的文本",
         "source": "palette"
       }
     }
     ```
     - `context.note`（可选，缺省时由宿主填充当前活动笔记相对路径；`sidebar_menu` / `tab_menu` 时为右键目标的相对路径，目标不在当前笔记本内时为空）。
     - `context.folder`（可选，仅 `sidebar_menu` 右键文件夹行时下发，文件夹相对路径）。
     - `context.file`（可选，仅当插件持有 `"notes:path"` 时下发）：目标的本机绝对路径。菜单类来源为右键目标，其余来源为当前活动标签；包括代码、图片等非 Markdown 文件与笔记本外打开的文件。没有打开的标签时为空。
     - `context.selection`（可选，仅当插件持有 `"editor:read"` 时下发）。
     - `context.source`（`"palette"` | `"shortcut"` | `"titlebar"` | `"page_menu"` | `"editor_menu"` | `"sidebar_menu"` | `"tab_menu"` | `"status"` | `"menubar"` | `"menubar_drop"`）。`"menubar"` 为点击菜单栏图标或其菜单条目，`"menubar_drop"` 为拖放到图标上。
     - 菜单栏来源额外下发：`context.menubar_item`（图标 `id`）、`context.payload`（动态菜单条目的 `payload`）、`context.kind`（`"files"` | `"image"` | `"text"`，仅拖放或 `input: "clipboard"` 时）、`context.files`（本机绝对路径，最多 50 个）、`context.text`（最多 64 KiB）。
     - 一次拖放或粘贴只下发一种内容，优先级为文件、图片、文本（从 Finder 拖文件时附带的文件名文本会被忽略）。图片数据（截图、网页图片）由宿主存为 PNG 后以 `files` 下发，`kind` 为 `"image"`；这些临时文件位于插件专属目录，超过 24 小时自动清理，需要保留时请自行复制。
   - 返回 `result`：`{"ok": true, "tone": "info", "status": "已执行", "show_plugin_icon": true, "copy_text": "https://..."}`（字段均可选；`status` 非空时宿主会弹出对应 `tone` 的 Toast 提示，默认 `"info"` 并附带插件图标，传 `"show_plugin_icon": false` 可恢复纯色圆点）。
     - `copy_text`：宿主把这段文本写入系统剪贴板，适合上传后返回链接。命令结束时 Block 可能在后台，插件无法调用需要窗口焦点的剪贴板接口，请用本字段。
     - 菜单栏来源的命令在 Block 窗口处于前台时以 Toast 提示 `status`，否则改为系统通知。

2. **工作区 `plugin.event`**（Host -> Plugin，需 `"events:workspace"`）
   - 仅向**已启动运行中**的插件派发（5 秒超时），宿主**不会**为这类事件冷启动已停止的插件。
   - 请求 `params`：`{"event": "<事件名>", "timestamp": "2026-11-20T02:00:00Z", ...}`
     - `"note.opened"`：携带 `{"path": "<当前打开笔记相对路径>"}`（申请 `events:workspace` 即可收到当前打开笔记的相对路径，无需 `notes:read`）。
     - `"theme.changed"`：携带当前主题信息（如 `{"theme": "light"|"dark"}`）。
     - `"locale.changed"`：携带当前语言（如 `{"locale": "zh"|"en"}`）。
   - 返回 `result`：`{"handled": true}`（可选）。

### 8.5 UI 贡献宿主方法（Plugin -> Host）

1. **`host.titlebar.get_items`**（需 `"ui:titlebar"`，参数 `{}`）
   - 返回：`{"items": [{"id": "feedback", "label": "反馈", "locked": false, "hidden": false, "plugin": "", "icon": {"name": "message-square"}}]}`
   - `icon` 为该槽位在顶栏实际使用的图标：内置槽位为宿主图标名 `{"name": ...}`（`send_all` 为文字胶囊 `{"chip": "统一发送", "dot": true}`；`reveal` 目前只出现在标签页右键菜单，不在顶栏绘制），插件按钮为其声明的内联矢量（`view_box` / `stroke_paths` 等），可直接透传到 `switch.icon` 或 `icon_strip.icons`（旧宿主不返回该字段）。
   - 内置槽位 ID：`"feedback"`、`"ai"`、`"send_all"`、`"wrap"`、`"zoom"`、`"reveal"`、`"more"`（`locked: true`）；插件按钮槽位 ID 为 `"plugin:<pluginID>/<buttonID>"`。
2. **`host.titlebar.set_hidden`**（需 `"ui:titlebar"`）
   - 参数：`{"hidden": ["feedback", "plugin:my-plugin/btn"]}`（按插件隔离持久化于 `<BlockAppData>/plugins/contrib-state.json`，停用插件时自动收回其隐藏设置）。
   - **不可隐藏约束**：内置槽位 `"more"`（页面 `…` 操作按钮）永久锁定，**永远不可隐藏**（传入 `"more"` 或未知槽位 ID 会被宿主忽略）。
   - 返回：`{"ok": true, "items": [...]}`（返回更新后的完整槽位列表）。
3. **`host.titlebar.set_button`**（需 `"ui:titlebar"`）
   - 参数：`{"id": "<button_id>", "badge": "3", "active": true}`（`id` 必填且须为本插件声明的 `titlebar_buttons[].id`；`badge` 可选，会去首尾空白并截断至最多 3 个 Unicode 字符；`active` 可选布尔值）。
   - 返回：`{"ok": true}`。
4. **`host.status.set`**（需 `"ui:status"`）
   - 参数：`{"id": "<item_id>", "text": "Synced", "tooltip": "上次同步 10:00", "icon": {...}, "show_plugin_icon": true, "clear_icon": false}`（`id` 必填且须为本插件声明的 `status_items[].id`；`text` 禁止含 Emoji 并截断至 40 个字符；`tooltip` 截断至 200 个字符；`icon` / `show_plugin_icon` / `clear_icon` 均为可选）。
   - 返回：`{"ok": true}`。
5. **`host.ui.toast`**（需 `"ui:notify"`，开放给所有插件）
   - 弹出应用内全局通知（窗口底部居中统一浮条，与主应用共用同一套通知队列和视觉样式，3 秒后自动收起或手动关闭）。与系统通知（`host.system.notify`）互相独立：需要应用内全局轻提示时调用本接口，需要操作系统原生通知中心横幅时调用 `host.system.notify`。
   - 参数：`{"message": "操作完成", "tone": "info", "icon": {...}, "show_plugin_icon": true}`（`message` 必填非空白，截断至 160 个字符；`tone` 仅接受 `"success"` | `"error"` | `"warning"`，其余归一化为 `"info"`；`icon` / `show_plugin_icon` 可选，用于在提示左侧展示双色矢量图标）。
   - 返回：`{"ok": true}`。
6. **`host.ui.open_dialog` / `host.ui.close_dialog`**（需 `"ui:notify"`）
   - 参数：`{"id": "<dialog_id>"}`（`id` 须为本插件声明的 `dialogs[].id`）。
   - 返回：`open_dialog` 在宿主成功打开后返回 `{"ok": true, "dialog_token": "dlg-..."}`（无宿主 UI 时返回 `-32005`），且用户在对应 `dialog:*` 表面触发 `plugin.action` 时宿主会随参数附带当前活动弹窗的 `"dialog_token"`，插件可严格核对来源表面与一次性令牌；`close_dialog` 返回 `{"ok": true}`。
7. **`host.ui.open_panel` / `host.ui.close_panel` / `host.ui.toggle_panel`**（需 `"ui:panels"`）
   - 参数：`{"id": "<panel_id>"}`（`id` 须为本插件声明的 `panels[].id`）。
   - 返回：`open_panel` / `close_panel` 返回 `{"ok": true}`；`toggle_panel` 返回 `{"ok": true, "open": true|false}`（由宿主原子切换并回传当前开关态，避免插件自维护状态与宿主关闭按钮失步）。
8. **`host.ui.refresh`**（仅要求目标表面属于 `"settings_page"` 或本插件 `contributes` 声明的表面）
   - 参数：`{"surface": "panel:outline"}`（异步触发对应表面的后台 `plugin.render` 重绘；用户在当前笔记本中尚未打开过的表面不会渲染，首次打开时自然加载最新内容）。
   - 返回：`{"ok": true}`。
9. **`host.sidebar.set_badges` / `host.sidebar.get_badges`**（需 `"ui:sidebar"`）
   - `host.sidebar.set_badges` 参数：`{"badge_id": "poop-mark", "notebook_root": "/abs/path/to/notebook", "clear": false, "patch": false, "entries": {"welcome.md": {"text": "2", "tooltip": "含 2 处标记", "tone": "accent"}}}`（`notebook_root` 可选，省略时取当前笔记本；`entries` 最多 512 项，相对路径不可包含 `..` 或 `:`，`text` 禁 Emoji 且 <= 4 字符，`tooltip` 禁 Emoji 且 <= 120 字符，`tone` 支持 `"accent"` | `"success"` | `"warning"` | `"danger"`；`patch: true` 时传 `null` 可删除单项，`clear: true` 一键清空；按插件与笔记本持久化于 `contrib-state.json`）。
   - `host.sidebar.get_badges` 参数：`{"badge_id": "poop-mark", "notebook_root": "..."}` -> 返回 `{"entries": {...}}`。
10. **`host.ui.backdrop` / `host.ui.set_backdrop`**（需 `"ui:appearance"`）
    - `host.ui.backdrop`（参数 `{}`）：返回本插件当前持久化的编辑区矢量背景状态 `{"id": "...", "enabled": true, "pattern": "scatter", "density": "medium", "opacity": 0.09, "tint": "accent", "dark_glow": false}`。
    - `host.ui.set_backdrop`（参数：`{"id": "poop-wallpaper", "enabled": true, "pattern": "scatter", "density": "medium", "opacity": 0.10, "tint": "accent", "dark_glow": false}`，未传的样式字段保留同 `id` 已有值或清单默认值）：实时更新编辑区矢量背景并持久化于 `contrib-state.json`，停用插件时自动失效，返回归一化后的状态对象。
    - 「到处拉屎」插件在 `plugin.initialize` 读取已保存的背景设置并调用 `host.ui.set_backdrop`，外观设置中的控件变化通过 `plugin.render` 的 `values` 即时保存和应用。
11. **`host.editor.selection`**（需 `"editor:read"`，参数 `{}`）
    - 返回：`{"note": "welcome.md", "text": "选中文本", "has_focus": true}`（编辑器不可用时返回 `-32005`）。
12. **`host.editor.insert`**（需 `"editor:write"`）
    - 参数：`{"text": "插入内容", "note": "welcome.md", "mode": "replace"|"insert"}`（**`text` 必填字符串**，省略或传 `null` 直接返回 `-32602`，不会误删当前选区；按 Markdown 解析为原生块后插入，并在写入磁盘时保留块级隐藏 HTML 注释签名且立即落盘，避免编辑器未保存状态与 `host.notes.read/write` 竞争；`mode` 默认 `"replace"` 替换当前选区，传 `"insert"` 时将选区折叠到末尾后再插入、不覆盖选中正文；`note` 可选：给定时主线程联合校验运行时存活、笔记本代次与绝对路径；在 `plugin.command` 处理期间省略时默认绑定发起命令的笔记，若右键菜单命令的目标是文件夹或笔记本外文件则直接返回 `-32004` 拒绝写入，不会落入其他活动标签页。`text` 上限 4 MiB，超限返回 `-32602`，宿主没有编辑器返回 `-32005`；没有打开的笔记或插入失败返回 `-32004`）。
    - 返回：`{"ok": true}`。
13. **`host.ui.tarot_state` / `host.ui.set_tarot`**（需 `"ui:settings"`、`"ui:commands"` 或 `"ui:titlebar"` 任一权限）
    - `host.ui.tarot_state`（参数 `{}`）：返回 `{"supported": true, "enabled": false, "room": "writing", "collected": 3, "total": 22, "daily_drawn": false, "daily_note": "", "active_note": ""}`。
    - `host.ui.set_tarot`（参数 `{"enabled": true, "toggle": false, "room": "writing"|"table"|"daily"|"deck"}`，字段均可选）：切换宿主原生塔罗模式（午夜铜版）或直接进入指定房间（从设置页指定 `room` 时自动收起设置），返回最新状态对象。
14. **`host.ui.editor_background` / `host.ui.set_editor_background`**（需 `"ui:settings"`、`"ui:commands"` 或 `"ui:titlebar"` 任一权限）
    - `host.ui.editor_background`（参数 `{}`）：返回当前插件设置的编辑器背景状态 `{"mode": "off"|"dots"|"grid", "contrast": "standard"|"high"}`。
    - `host.ui.set_editor_background`（参数 `{"mode": "dots", "contrast": "standard"}`）：设置与编辑器行高对齐的点阵（`"dots"`）、方格（`"grid"`）或关闭（`"off"`）背景及对比度（`"standard"` | `"high"`，兼容 `"strong"` 归一化为 `"high"`），持久化于 `contrib-state.json` 并按插件隔离；停用插件时自动失效，返回归一化后的状态对象。

15. **`host.menubar.set`**（需 `"ui:menubar"`）
    - 参数：`{"id": "<item_id>", "title": "3", "tooltip": "上传中", "menu": [{"id": "r1", "title": "shot.png", "subtitle": "https://...", "command": "copy_link", "payload": "https://..."}, {"separator": true}], "clear_menu": false}`（`id` 必填且须为本插件声明的 `menubar_items[].id`，其余字段可选，省略的字段保持原值）。
    - `title`：图标右侧的短文字（禁止 Emoji，截断至 16 个字符，传 `""` 清除）；`tooltip`：覆盖清单里的提示（截断至 200 个字符）。
    - `menu`：动态条目，整体替换上一次的动态条目，显示在静态 `menu` 下方（最多 20 条）。`title` 必填（截断至 60 个字符），`subtitle` 可选（截断至 80 个字符）；`command` 须引用 `commands[].id`，省略时条目置灰；`payload` 为最多 4096 字节的字符串，点击时作为 `context.payload` 随命令下发。`clear_menu: true` 清空动态条目。
    - 动态内容只保存在内存中。宿主按需启动插件，Block 重开后要等插件首次运行（如 `plugin.initialize`）重新推送，动态条目才会恢复。
    - 返回：`{"ok": true}`。

<!-- ai-guide:skip-start -->
---

下一步：[6. 语言扩展规范 →](languages.md)
<!-- ai-guide:skip-end -->
