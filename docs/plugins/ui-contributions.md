<!-- ai-guide:skip-start -->
# UI Contributions（宿主界面扩展贡献）

> [← 协议、UI 与骨架](protocol-and-ui.md) · [文档中心](../README.md) · [语言扩展 →](languages.md)

---
<!-- ai-guide:skip-end -->

## 7. UI contributions（宿主界面扩展贡献）

插件可通过 `manifest.json` 的 `contributes` 字段在宿主设置页之外挂载顶栏按钮、顶栏布局管理、内置设置标签页分区、命令/快捷键、页面/编辑器/侧栏/标签页右键菜单、左侧栏分区、文件树行角标、文件树自定义图标包、目录忽略项、编辑区矢量/纸纹背景、块装饰器、主题皮肤、离线 WebView 舞台、右侧边栏面板、状态栏条目、弹窗与系统菜单栏图标。默认放行能力和显式声明要求见 [清单与权限](manifest.md)。

### 7.1 多表面（Surfaces）渲染与状态隔离

- 需要独立设置页时在 `manifest.surfaces` 中填写 `["settings_page"]`（无独立设置页的插件可省略）；`settings_sections`、`panels`、`sidebar_sections`、`dialogs` 会自动启用下列前缀表面：
  - `"settings_section:<id>"`
  - `"panel:<id>"`
  - `"sidebar_section:<id>"`
  - `"dialog:<id>"`
- 宿主调用 `plugin.render` 与 `plugin.action` 时会在 `params.surface` 中传入对应的表面标识，插件按 `surface` 返回各自独立的 `UIPage` 与 `ActionResult`。
- 宿主按 `(plugin_id, notebook_root, surface)` 分别隔离加载态、忙碌/反馈状态、表单值、定时刷新（`refresh_interval_ms`）与笔记本切换感知；停用或卸载插件时会销毁其全部表面视图。

### 7.2 `manifest.contributes` 字段定义

通用校验规则：
- 所有贡献项 `id` 必须匹配正则 `^[a-z][a-z0-9_-]{0,31}$`，且在同一类别内不可重复。
- 所有 `LocalizedText`（`title`、`tooltip` 等）均须同时提供非空 `zh` 与 `en`，且禁止包含 Emoji；所有 `icon`（`DuoIconSpec`）须满足 `view_box == "0 0 24 24"` 且至少包含 1 条 `stroke_paths`；三层路径合计最多 16 条、路径数据合计最多 4096 字节。
- 数量上限：`titlebar_buttons` 3、`settings_sections` 8、`commands` 64、`page_menu` 16、`editor_menu` 16、`sidebar_menu` 16、`tab_menu` 16、`panels` 8、`sidebar_sections` 4、`file_badges` 4、`file_icons` 1、`backdrops` 8、`block_decorators` 8、`themes` 8、`stages` 8、`status_items` 4、`dialogs` 16、`menubar_items` 2。

1. **`titlebar_buttons`**（最多 **3** 个）：在顶栏右侧挂载图标按钮。顶栏有空位时所有插件按钮都直接显示，仅在空间不足时把放不下的按钮收起；用户可在「更多插件」菜单（点星标或在选中/悬停行按 `P`）或右键/聚焦顶栏插件图标（按 `P` / `Shift+F10`）固定常用按钮（最多固定 3 个，优先展示；槽位满时优先回收已停用/卸载项，有效项满 3 个时替换最早固定项并提示），其余按声明顺序收进顶栏的「更多插件」菜单（带计数角标、插件图标与活跃/角标提示）。菜单行文字取 `tooltip`（缺省为 `id`），因此务必填写可读的 `tooltip`。
   - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（必填 `LocalizedText`）。
   - `command`（可选）与 `panel`（可选）：**必须且只能二选一**，分别引用同清单 `commands[].id` 或 `panels[].id`（点击时执行命令或切换面板）。
1.1. **`titlebar_manager`**（`contributes` 顶层可选布尔值，须在 `manifest.permissions` 中声明 `"ui:titlebar"`）：设为 `true` 时允许该插件调用 `host.titlebar.set_hidden` 隐藏或恢复顶栏槽位。
2. **`settings_sections`**：在内置设置标签页末尾追加一张声明式设置分区卡片（单卡片标题与分区标题一致时宿主自动去重）。
   - **允许但不建议**：插件设置应放在插件自己的设置页（`manifest.surfaces: ["settings_page"]`），用户在插件页里就能找到全部选项；只有确实属于宿主某个内置页的单项（极少数）才考虑用本扩展点，且不要把同一组设置在两处重复展示。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - `tab`（必填）：仅允许 `"ai"` | `"appearance"` | `"folders"` | `"shortcuts"`。
   - 对应渲染表面：`"settings_section:<id>"`。
3. **`commands`**：注册到命令面板并支持默认快捷键。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - `shortcut`（可选字符串）：例如 `"cmd-alt-p"`。`cmd` 在 macOS 表示 Command，在 Windows/Linux 表示 Ctrl。宿主优先处理内置组合，例如 `cmd-f`、`cmd-,`、`cmd-o`、`cmd-s`、`cmd-shift-p`，以及 Windows/Linux 的 `ctrl-f4`、`f1`、`f2`、`f3`、`shift-f3` 和 `f11`。保留键以 `internal/ui/shortcuts.go` 的 `IsHostReservedShortcut` 为准。
   - **宿主统一快捷键托管**：只要插件声明了 `commands`，宿主会自动将其纳入「设置 / 快捷键」（按插件自动分组，支持按「全部 / 内置 / 插件」筛选），并在该插件独立设置页底部自动注入「快捷键」分组；用户可直接点击录键、按 `Backspace`/`Delete` 清空或一键恢复默认，自定义结果会自动同步到快捷键分发、命令面板以及侧栏/页面/标签页右键菜单，插件无需也不应自行绘制快捷键设置 UI。
4. **`page_menu`**：向页面右上角 `…` 菜单追加条目。Markdown 笔记、代码/HTML/SVG 源码与图片预览等所有页面都会显示。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填，须引用已声明的 `commands[].id`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
5. **`editor_menu`**：向编辑器右键菜单追加条目（渲染原生彩色 SVG 图标）。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填，须引用已声明的 `commands[].id`）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
6. **`sidebar_menu`**（`command` 引用已声明的 `commands[].id`）：向左侧栏文件树右键菜单追加条目。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - `targets`（可选字符串数组，仅 `sidebar_menu` 可用）：`"note"` | `"folder"`，不可重复；省略时为 `["note"]`（只在笔记行显示）。声明 `["note", "folder"]` 可同时出现在文件夹行。
   - 触发时 `context.source` 为 `"sidebar_menu"`；右键笔记时 `context.note` 为其相对路径，右键文件夹时改为 `context.folder`（文件夹相对路径，`note` 为空）。
6.1. **`tab_menu`**（`command` 引用已声明的 `commands[].id`）：向顶栏标签页右键菜单顶部追加条目。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`command`（必填）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
   - 触发时 `context.source` 为 `"tab_menu"`，目标是被右键的标签（不一定是当前活动标签）。目标在当前笔记本内时 `context.note` 为其相对路径；笔记本外打开的文件 `note` 为空，只能通过 `context.file`（需 `"notes:path"`）拿到路径。
7. **`panels`**：注册右侧边栏面板（标题栏自动渲染 `icon`）。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）。
   - 对应渲染表面：`"panel:<id>"`。
8. **`sidebar_sections`**（最多 **4** 个）：在左侧栏文件树下方、笔记本切换器上方追加紧凑可折叠声明式卡片。
   - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）、`collapsible`（可选 bool）、`default_collapsed`（可选 bool）。
   - 对应渲染表面：`"sidebar_section:<id>"`。
9. **`file_badges`**（最多 **4** 个）：声明左侧栏文件树笔记行尾角标图标，运行时通过 `host.sidebar.set_badges` 按笔记相对路径推送（单行最多展示 2 个角标，停用或卸载插件立即清除）。
   - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（可选 `LocalizedText`）。
9.1. **`file_icons`**（最多 **1** 个）：声明笔记本、文件与文件夹自定义图标包及原生交互选择器；停用或卸载插件时立即恢复默认图标。
   - `id`（必填）、`pack`（必填，插件目录内相对 `.json` 路径，<= 512 KiB）、`title`（可选 `LocalizedText`）、`picker`（可选 bool，启用点击侧栏/笔记本切换器/顶栏图标打开选择器）、`sync_tabs`（可选 bool，同步在顶栏标签页显示自定义图标）。
   - 图标包 JSON 顶层字段：`schema_version`（固定 `1`）、`categories`（1–16 项，含 `id`、`name`、`icon`）与 `icons`（1–256 项，含 `id`、`category`、`name`、可选 `keywords`，且 `ref` 宿主内置图标名与 `spec` 内联 `DuoIconSpec` 二选一）。
9.2. **`ignored_dirs`** 需要显式声明 `"ui:sidebar"`。列表最多含 32 个目录名，每个 1–128 字节，不能是 `.` 或 `..`，也不能含 `/`、`\` 或 NUL。名字按原样精确匹配。启用的插件可在所有笔记本中将匹配目录及其内容从文件树、工作区扫描和搜索中排除，「显示隐藏文件」开启时同样生效。停用或卸载插件会移除它的忽略项并刷新文件树。
10. **`backdrops`**（最多 **8** 个）：声明编辑区自定义矢量背景或统一纸纹背景，运行时通过 `host.ui.set_backdrop` 启用、切换或调节（宿主自动钳制透明度在 `0.03`–`0.25` 以保证正文清晰可读，停用或卸载插件立即消失）。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）。
    - `pattern`（可选：`"scatter"` | `"grid"` | `"watermark"` | `"corners"` | `"paper_dots"` | `"paper_grid"`，默认 `"scatter"`，其中 `paper_dots` 与 `paper_grid` 为与编辑器行高对齐的纸纹背景）、`density`（可选：`"sparse"` | `"medium"` | `"dense"`，默认 `"medium"`）、`contrast`（可选：`"standard"` | `"high"`，默认 `"standard"`，用于纸纹对比度）、`opacity`（可选：`0.01`–`0.35`，渲染钳制 `0.03`–`0.25`，默认 `0.09`）、`tint`（可选：`"accent"` | `"muted"` | `"warm"` | `"glow"`，默认 `"accent"`）、`dark_glow`（可选 bool，深色主题下提亮为夜光质感）。
11. **`status_items`**：注册底部状态栏条目。
    - `id`（必填）、`text`（可选 `LocalizedText`，作为 `host.status.set` 覆盖前的初始文案；解析后为空白时不显示）、`command`（可选，非空时须引用 `commands[].id`）、`icon`（可选 `DuoIconSpec`）。
12. **`dialogs`**：注册模态弹窗（由 `host.ui.open_dialog` 打开，标题栏渲染图标）。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`width`（可选整数；`0` 或省略默认 `460`，显式指定时须在 `320`–`720` 之间）、`icon`（可选 `DuoIconSpec`，缺省回退 `manifest.icon`）。
    - 对应渲染表面：`"dialog:<id>"`。
13. **`menubar_items`**（最多 **2** 个）：在系统菜单栏放一个图标，Block 窗口不在前台也能用，适合截图上传、快速记录这类随手操作。插件启用后图标出现，停用或卸载后消失。
    - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（必填 `LocalizedText`）。菜单栏图标按系统模板图只画 `stroke_paths` 与 `accent_paths` 的线条，`fill_paths` 不绘制。
    - 点击行为：`command`（点击直接执行，须引用 `commands[].id`）、`menu`（点击弹出菜单，最多 16 条）、`drop`（接收拖放）。`command` 与 `menu` 不能同时声明；三者全省略时校验失败。插件声明了 `settings_page` 且图标有菜单时，菜单末尾自动追加「插件设置…」。
    - `menu[]` 条目：`{"id", "title": LocalizedText, "command", "input"}` 或分隔线 `{"separator": true}`（分隔线不能带其他字段）。`input` 仅接受 `"clipboard"`（须声明 `"system:clipboard_read"` 权限）：点击时宿主读取剪贴板，按 `drop` 的规则过滤后随命令下发（未声明 `drop` 时接收文件、图片与文本）；剪贴板里没有可接收的内容时不执行命令并提示用户。
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
14. **`block_decorators`**（最多 **8** 个，须在 `manifest.permissions` 中声明 `"ui:appearance"` 或 `"editor:write"`）：按块级隐藏 HTML 注释签名匹配引用/段落/提示块，为其应用通用 Callout 样式与空块占位提示。
    - `id`（必填）、`signature_prefix`（必填，匹配 `^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,63}$`）、`default_callout`（必填：`"note"` | `"tip"` | `"important"` | `"warning"` | `"caution"`）。
    - `block_kind`（可选：`"quote"` | `"paragraph"` | `"callout"`）、`legacy_tag_prefix`（可选，兼容旧括号前缀）、`callout_by_kind`（可选 `map<string, callout>`）、`empty_placeholder`（可选 `LocalizedText`）。
15. **`themes`**（最多 **8** 个，须在 `manifest.permissions` 中声明 `"ui:appearance"`）：声明外部 JSON 主题皮肤，通过 `host.ui.set_theme_skin` 切换。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`file`（必填，插件目录内相对 `.json` 路径，<= 512 KiB；JSON 须含 `id`、至少定义 `light`/`dark`/`surfaces` 颜色之一，可选 `fonts`、`chrome_dark`、`quick_note`、`ai_indicator`）。
16. **`stages`**（最多 **8** 个，须在 `manifest.permissions` 中声明 `"ui:appearance"`、`"ui:commands"` 或 `"ui:panels"`）：声明全离线插件专属 WebView 舞台（本地资源位于 `web/`，最多 128 个普通文件、单文件校验上限 4 MiB / 服务上限 16 MiB），通过 `host.stage.open` 展示。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`entry`（必填，须为 `web/` 下相对路径，如 `"web/index.html"`）、`full_window`（可选 bool）。
    - **前端运行时与四层外网隔离**：舞台页面运行在只读 `block-plugin://<plugin-id>/...` 协议上，宿主预注入 `window.blockView`（`getInitState()`、`invoke(action, payload)`、`onEvent(fn)`、`close()`，接收 `"stage.state"` / `"theme.changed"` / `"locale.changed"` 及 `host.stage.emit` 事件）并实施四层外网隔离：
      1. 静态校验拒绝 `web/` 内任何文件出现 `http://` 或 `https://` 字面量；
      2. WebView `ContentRules` 拦截所有 `^https?://` 与 `^wss?://` 请求（仅豁免内部 `http://*.block-plugin.localhost/`）；
      3. `OnWillNavigate` 与 `SetWindowOpenHandler` 锁定仅允许当前 `block-plugin://<plugin-id>/` 同源导航且禁止弹窗；
      4. 严格 CSP（`connect-src 'none'; frame-src 'none'; object-src 'none'; script-src 'self'`）并禁用 DevTools。

### 7.3 `plugin.command` 与工作区 `plugin.event`

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
   - 返回 `result`：`{"ok": true, "tone": "info", "status": "已执行", "body": "可选正文", "action": {"id": "open_settings", "label": "查看"}, "show_plugin_icon": true, "copy_text": "https://..."}`（字段均可选；`status` 非空时宿主会在右上角弹出对应 `tone` 的通知横幅，`tone` 支持 `"info"` / `"success"` / `"warning"`(`"warn"`) / `"error"`(`"danger"`)，默认 `"info"` 并自动附带插件图标，传 `"show_plugin_icon": false` 改用内置类型图标）。
     - `copy_text`（须声明 `"system:clipboard_write"` 权限）：宿主把这段文本写入系统剪贴板，适合上传后返回链接。命令结束时 Block 可能在后台，插件无法调用需要窗口焦点的剪贴板接口，请用本字段。
     - 菜单栏来源的命令在 Block 窗口处于前台时以 Toast 提示 `status`，否则改为系统通知。
     - 菜单栏来源的命令执行期间，图标旁显示 `…`；结束后 `status` 会在图标旁短暂显示约 4 秒（超长截断，悬停可看全文），系统通知关闭时用户也能看到反馈。命令无 `status` 时直接恢复插件自己的标题。

2. **工作区 `plugin.event`**（Host -> Plugin，需 `"events:workspace"`）
   - 仅向**已启动运行中**的插件派发（5 秒超时），宿主**不会**为这类事件冷启动已停止的插件。
   - 请求 `params`：`{"event": "<事件名>", "timestamp": "2026-11-20T02:00:00Z", ...}`
     - `"note.opened"`：携带 `{"path": "<当前打开笔记相对路径>"}`（申请 `events:workspace` 即可收到当前打开笔记的相对路径，无需 `notes:read`）。
     - `"theme.changed"`：携带当前主题信息（如 `{"theme": "light"|"dark"}`）。
     - `"locale.changed"`：携带当前语言（如 `{"locale": "zh"|"en"}`）。
   - 返回 `result`：`{"handled": true}`（可选）。

### 7.4 通用 UI 贡献宿主方法（Plugin -> Host）

1. **`host.titlebar.get_items`**（无需声明权限，参数 `{}`）
   - 返回：`{"items": [{"id": "feedback", "label": "反馈", "locked": false, "hidden": false, "plugin": "", "icon": {"name": "message-square"}}]}`
   - `icon` 为该槽位在顶栏实际使用的图标：内置槽位为宿主图标名 `{"name": ...}`（`send_all` 为文字胶囊 `{"chip": "统一发送", "dot": true}`；`reveal` 目前只出现在标签页右键菜单，不在顶栏绘制），插件按钮为其声明的内联矢量（`view_box` / `stroke_paths` 等），可直接透传到 `switch.icon` 或 `icon_strip.icons`（旧宿主不返回该字段）。
   - 内置槽位 ID：`"feedback"`、`"ai"`、`"send_all"`、`"wrap"`、`"zoom"`、`"reveal"`、`"more"`（`locked: true`）；插件按钮槽位 ID 为 `"plugin:<pluginID>/<buttonID>"`。
2. **`host.titlebar.set_hidden`**（需在清单声明 `contributes.titlebar_manager: true`）
   - 参数：`{"hidden": ["feedback", "plugin:my-plugin/btn"]}`（按插件隔离持久化于 `<BlockAppData>/plugins/contrib-state.json`，停用插件时自动收回其隐藏设置）。
   - **不可隐藏约束**：内置槽位 `"more"`（页面 `…` 操作按钮）永久锁定，**永远不可隐藏**（传入 `"more"` 或未知槽位 ID 会被宿主忽略）。
   - 返回：`{"ok": true, "items": [...]}`（返回更新后的完整槽位列表）。
3. **`host.titlebar.set_button`**（无需声明权限）
   - 参数：`{"id": "<button_id>", "badge": "3", "active": true}`（`id` 必填且须为本插件声明的 `titlebar_buttons[].id`；`badge` 可选，会去首尾空白并截断至最多 3 个 Unicode 字符；`active` 可选布尔值）。
   - 返回：`{"ok": true}`。
4. **`host.status.set`**（无需声明权限）
   - 参数：`{"id": "<item_id>", "text": "Synced", "tooltip": "上次同步 10:00", "icon": {...}, "show_plugin_icon": true, "clear_icon": false}`（`id` 必填且须为本插件声明的 `status_items[].id`；`text` 禁止含 Emoji 并截断至 40 个字符；`tooltip` 截断至 200 个字符；`icon` / `show_plugin_icon` / `clear_icon` 均为可选）。
   - 返回：`{"ok": true}`。
5. **`host.ui.toast`**（无需声明权限，开放给所有插件）
   - 在窗口右上角（顶栏下方、固定宽 360px、最多同时叠放 3 条、总队列 6 条、单插件上限 4 条且相同内容自动合并去重）弹出应用内全局通知横幅，与主应用共用同一套视觉与队列。需要操作系统原生通知中心横幅时请使用 `host.system.notify`。
   - **最简调用（只传一句话）**：
     ```json
     {"message": "已复制路径"}
     ```
   - **完整调用（除 `message` 外全部可选）**：
     ```json
     {
       "message": "同步失败",
       "body": "网络超时，30 秒后自动重试。",
       "tone": "error",
       "action": {"id": "open_sync", "label": "查看", "payload": {"tab": "log"}}
     }
     ```
   - 字段与行为：
     - `message`：必填非空白（截断至 160 字符）。仅传 `message` 且一行能放下时呈紧凑单行居中样式，较长时最多显示 2 行；同时传 `body` 或 `action` 时 `message` 作为加粗单行标题，`body`（可选，截断至 1000 字符）作为常规正文最多显示 4 行、超出截断并在悬停时提示全文。
     - `tone`：可选，支持 `"info"`（默认）、`"success"`、`"warning"` / `"warn"`、`"error"` / `"danger"`。
     - **图标保证**：每条横幅必定带图标——优先使用显式传入的 `icon`；否则默认自动使用插件 `manifest.icon`（无需再传 `show_plugin_icon: true`）；显式传 `"show_plugin_icon": false` 时改用宿主内置类型图标（✓ / ! / × / i）。
     - **停留与交互**：`info` / `success` 4 秒自动收起，`warning` 6 秒自动收起，`error` 及带 `action` 的横幅不自动消失（须手动关闭）；鼠标悬停时暂停倒计时。
     - `action`：可选单个按钮（`id` 匹配 `^[a-z][a-z0-9_-]{0,31}$`，`label` 非空、禁 Emoji、截断至 24 字符，可选 `payload` 对象）。用户点击后关闭横幅并回调 `plugin.action`（`surface: "toast"`，`action_id: action.id`，`values: action.payload`）。
   - 返回：`{"ok": true, "shown": true|false, "queued": true|false}`（`shown: true` 表示已立即上屏，`queued: true` 表示已进入等待队列，两者均为 `false` 表示因队列限流被丢弃）。
5.1. **`host.ui.open_settings`**（无需声明权限；需先检查 `ui_capabilities` 包含 `"open_settings"` 且插件声明了 `"settings_page"`）
   - 参数：`{}`。直接打开当前插件的设置页，适合在 `surface: "toast"` 的动作回调中引导用户查看详情或配置。
   - 返回：`{"ok": true}`（无宿主 UI 时返回 `-32005`）。
6. **`host.ui.open_dialog` / `host.ui.close_dialog`**（无需声明权限）
   - 参数：`{"id": "<dialog_id>"}`（`id` 须为本插件声明的 `dialogs[].id`）。
   - 返回：`open_dialog` 在宿主成功打开后返回 `{"ok": true, "dialog_token": "dlg-..."}`（无宿主 UI 时返回 `-32005`），且用户在对应 `dialog:*` 表面触发 `plugin.action` 时宿主会随参数附带当前活动弹窗实例的 `"dialog_token"`（同一打开实例内的多次动作复用同一个令牌），插件可核对来源表面与当前弹窗实例；`close_dialog` 返回 `{"ok": true}`。
7. **`host.ui.open_panel` / `host.ui.close_panel` / `host.ui.toggle_panel`**（无需声明权限）
   - 参数：`{"id": "<panel_id>"}`（`id` 须为本插件声明的 `panels[].id`）。
   - 返回：`open_panel` / `close_panel` 返回 `{"ok": true}`；`toggle_panel` 返回 `{"ok": true, "open": true|false}`（由宿主原子切换并回传当前开关态，避免插件自维护状态与宿主关闭按钮失步）。
8. **`host.ui.refresh`**（无需声明权限，仅要求目标表面属于 `"settings_page"` 或本插件 `contributes` 声明的表面）
   - 参数：`{"surface": "panel:outline"}`（异步触发对应表面的后台 `plugin.render` 重绘；用户在当前笔记本中尚未打开过的表面不会渲染，首次打开时自然加载最新内容）。
   - 返回：`{"ok": true}`；若调用时的笔记本上下文与当前打开笔记本不匹配，可能返回 `{"ok": true, "skipped": true}`。
9. **`host.sidebar.set_badges` / `host.sidebar.get_badges`**（无需声明权限）
   - `host.sidebar.set_badges` 参数：`{"badge_id": "review-mark", "notebook_root": "/abs/path/to/notebook", "clear": false, "patch": false, "entries": {"welcome.md": {"text": "2", "tooltip": "含 2 处标记", "tone": "accent"}}}`（`notebook_root` 可选，省略时取当前笔记本；`entries` 最多 512 项，相对路径不可包含 `..` 或 `:`，`text` 禁 Emoji 且 <= 4 字符，`tooltip` 禁 Emoji 且 <= 120 字符，`tone` 支持 `"accent"` | `"success"` | `"warning"` | `"danger"`；`patch: true` 时传 `null` 可删除单项，`clear: true` 一键清空；按插件与笔记本持久化于 `contrib-state.json`）。
   - `host.sidebar.get_badges` 参数：`{"badge_id": "review-mark", "notebook_root": "..."}` -> 返回 `{"entries": {...}}`。
9.1. **`host.sidebar.set_file_icons` / `host.sidebar.get_file_icons`**（无需声明权限，需声明 `contributes.file_icons`）
   - `host.sidebar.set_file_icons` 参数：`{"notebook_root": "/abs/path/to/notebook", "clear": false, "patch": false, "entries": {".": {"icon": "book", "tint": "blue"}, "welcome.md": {"icon": "wave", "tint": "blue"}}, "prefs": {"picker": true, "sync_tabs": true}}`（`notebook_root` 省略时取当前笔记本；`"."` 表示笔记本自身图标，其余为笔记本内相对路径；`entries` 最多 2048 项，`icon` 须存在于图标包，`tint` 支持 `"none"` | `"blue"` | `"teal"` | `"green"` | `"amber"` | `"red"` | `"purple"`；`patch: true` 时传 `null` 删除单项，`clear: true` 清空当前笔记本映射；`prefs` 覆盖选择器与标签页同步开关；文件重命名/移入回收站时宿主自动同步路径）。
   - `host.sidebar.get_file_icons` 参数：`{"notebook_root": "..."}` -> 返回 `{"notebook_root": "...", "entries": {...}, "recent": [...], "prefs": {"picker": true, "sync_tabs": true}}`。
9.2. **`host.sidebar.set_ignored_dirs` / `host.sidebar.get_ignored_dirs`** 需要显式声明 `"ui:sidebar"`。参数和返回值见 [双向 JSON-RPC 2.0 协议](protocol-and-ui.md)。
10. **`host.editor.selection`**（需 `"editor:read"`，参数 `{}`）
    - 返回：`{"note": "welcome.md", "text": "选中文本", "has_focus": true}`（编辑器不可用时返回 `-32005`）。
11. **`host.editor.insert`**（需 `"editor:write"`）
    - 参数：`{"text": "插入内容", "note": "welcome.md", "mode": "replace"|"insert"}`（**`text` 必填字符串**，省略或传 `null` 直接返回 `-32602`，不会误删当前选区；按 Markdown 解析为原生块后插入，并在写入磁盘时保留块级隐藏 HTML 注释签名且立即落盘，避免编辑器未保存状态与 `host.notes.read/write` 竞争；`mode` 默认 `"replace"` 替换当前选区，传 `"insert"` 时将选区折叠到末尾后再插入、不覆盖选中正文；`note` 可选：给定时主线程联合校验运行时存活、笔记本代次与绝对路径；在 `plugin.command` 处理期间省略时默认绑定发起命令的笔记，若右键菜单命令的目标是文件夹或笔记本外文件则直接返回 `-32004` 拒绝写入，不会落入其他活动标签页。`text` 上限 4 MiB，超限返回 `-32602`，宿主没有编辑器返回 `-32005`；没有打开的笔记或插入失败返回 `-32004`）。
    - 返回：`{"ok": true}`。
12. **`host.menubar.set`**（无需声明权限）
    - 参数：`{"id": "<item_id>", "title": "3", "tooltip": "上传中", "menu": [{"id": "r1", "title": "shot.png", "subtitle": "https://...", "command": "copy_link", "payload": "https://..."}, {"separator": true}], "clear_menu": false}`（`id` 必填且须为本插件声明的 `menubar_items[].id`，其余字段可选，省略的字段保持原值）。
    - `title`：图标右侧的短文字（禁止 Emoji，截断至 16 个字符，传 `""` 清除）；`tooltip`：覆盖清单里的提示（截断至 200 个字符）。
    - `menu`：动态条目，整体替换上一次的动态条目，显示在静态 `menu` 下方（最多 20 条）。`title` 必填（截断至 60 个字符），`subtitle` 可选（截断至 80 个字符）；`command` 须引用 `commands[].id`，省略时条目置灰；`payload` 为最多 4096 字节的字符串，点击时作为 `context.payload` 随命令下发。`clear_menu: true` 清空动态条目。
    - 动态内容只保存在内存中。宿主按需启动插件，Block 重开后要等插件首次运行（如 `plugin.initialize`）重新推送，动态条目才会恢复。
    - 返回：`{"ok": true}`。
13. **`host.ui.set_background_error` / `host.ui.clear_background_error`**（无需声明权限）
    - `host.ui.set_background_error`：在设置侧栏插件项与顶栏错误胶囊展示后台告警或错误（每插件最多 32 个 `key`，默认 `"default"`）：`{"key", "label", "detail", "retry_action", "retry_label", "retry_payload", "severity": "danger"|"warning"}`（`label` 必填且 <= 120 字，禁 Emoji）-> `{"ok": true, "key"}`。
    - `host.ui.clear_background_error`：`{"key"}`（省略 `key` 清空本插件全部后台错误）-> `{"ok": true}`。
14. **`host.ui.theme_skin_state` / `host.ui.set_theme_skin`**（需声明 `contributes.themes` 与 `"ui:appearance"`）
    - `host.ui.theme_skin_state`（`{}`）：返回 `{"id", "enabled", "skin_id"}`。
    - `host.ui.set_theme_skin`（`{"id", "enabled": true|false}`）：应用或关闭声明的主题皮肤并持久化于 `contrib-state.json`，同时停用其他插件皮肤。
15. **`host.stage.*`**（需声明 `contributes.stages` 与对应界面权限）
    - `host.stage.open`：`{"id", "route", "state"}`（`route` 兼容别名 `room`；`state` 接受任意合法 JSON，<= 2 MiB）-> `{"ok": true, "open": true, "stage": {...}}`。
    - `host.stage.close`：`{"id"}` -> `{"ok": true, "closed": true|false}`。
    - `host.stage.state`（`{}`）：返回 `{"open": true|false, "stage_id", "route", "state"}`。
    - `host.stage.emit`：`{"id", "event", "payload"}`（`payload` 上限 2 MiB）-> `{"ok": true}`（向打开中的舞台 WebView 推送事件）。

### 7.5 编辑区背景能力（`host.ui.backdrop` / `host.ui.set_backdrop`）

以下接口用于控制声明在 `contributes.backdrops` 中的编辑区矢量或纸纹背景（无需额外声明权限，按需调用）：

- **`host.ui.backdrop`**（参数 `{}`）：返回本插件当前持久化的背景状态 `{"id": "...", "enabled": true, "pattern": "scatter"|"grid"|"watermark"|"corners"|"paper_dots"|"paper_grid", "density": "sparse"|"medium"|"dense", "contrast": "standard"|"high", "opacity": 0.09, "tint": "accent", "dark_glow": false}`。
- **`host.ui.set_backdrop`**（参数：`{"id": "...", "enabled": true, "pattern": "paper_dots", "contrast": "standard", "density": "medium", "opacity": 0.10, "tint": "accent", "dark_glow": false}`，未传的样式字段保留同 `id` 已有值或清单默认值）：实时更新编辑区矢量或行高对齐纸纹背景并持久化于 `contrib-state.json`，停用插件时自动失效，返回归一化后的状态对象。

<!-- ai-guide:skip-start -->
---

下一步：[5. 语言扩展 →](languages.md)
<!-- ai-guide:skip-end -->
