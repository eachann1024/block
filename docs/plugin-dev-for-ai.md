# Block 应用商店插件开发指南（AI 自包含规范 v1 · `blockmd`）

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过本地协议检查的插件。

---

## 1. 架构与硬边界

- **独立进程与安装模型**：只安装信任的插件。子进程使用 Block 的系统权限，清单 `permissions` 仅控制宿主 RPC。
  - 安装路径：`<BlockAppData>/apps/<plugin-id>/`
  - 私有存储：`<BlockAppData>/apps-data/<plugin-id>/state.json`（停用状态与目录缓存位于 `<BlockAppData>/apps-cache/`）
  - 外部链接（`block://`、`blockmd://` 的 `store` / `install`，支持 `repo` / `url` / `id` 参数）只打开应用商店或对应插件详情页，不自动安装。
  - 新安装的插件默认停用，用户手动启用后才会启动；同 `id` 升级保留原有启用状态。在应用商店或手动安装中由用户主动点击安装/更新，允许跨来源或新增权限替换同 `id` 插件（替换前先停止旧进程，并以 `.previous` 备份保护原子替换）；非交互调用默认拒绝跨来源或新增权限替换。卸载移除插件程序，并保留私有配置供重新安装。
  - **保留 ID**：`block-diagnostics` 已被 Block 内置「性能与占用」窗口占用，应用商店会过滤且拒绝安装或启用该 ID。
- **跨语言双向 JSON-RPC 2.0**：宿主以独立子进程启动 `manifest.json` 中的 `entry.command`（推荐 `["python3", "-u", "main.py"]`、`["node", "main.mjs"]` 或编译后可执行文件），通过 UTF-8 **按行分隔（`\n`）的 JSON-RPC 2.0** 在 `stdin` / `stdout` 双向通信，`stderr` 仅用于调试（宿主用 64 KB 环形缓冲区保留尾部）。
  - `entry.command[0]` 必须是 `PATH` 中的可执行文件名，或插件目录内的相对普通文件路径（如 `"./bin/plugin"`）；禁止绝对路径、`..`、冒号或符号链接。
  - **环境变量白名单**：宿主仅传入 `PATH`、`HOME`、`TMPDIR`、`PYTHONUNBUFFERED=1`、`BLOCK_PLUGIN_ID`、`BLOCK_DATA_DIR`、`BLOCKMD_PLUGIN_ID`、`BLOCKMD_DATA_DIR`、`BLOCKMD_DOWNLOADS_DIR`，不继承其他宿主环境变量。
  - **超时与资源硬上限**：macOS/Linux 在独立进程组（`Setpgid`）运行，超时、取消任务或关闭时整组终止。超时：首次加载 15s、编辑预览刷新 8s、定时轮询 5s、`plugin.action` 120s（顶栏提供「取消任务」）、`plugin.event` 45s、`plugin.shutdown` 2s。上限：单行 JSON-RPC <= 4 MB、并发 `host.*` 调用 <= 8（超出将中止进程）、`state.json` <= 8 MB、单篇笔记读写 <= 16 MB；安装归档下载 <= 64 MB、单文件 <= 32 MB、解包总计 <= 256 MB（tar 解压流 <= 320 MB）、路径数 <= 8192 且仅允许普通文件与目录（不支持符号链接）。
- **原生设置挂载与商店详情页**：已安装插件自动挂载在「设置」左侧导航栏分割线下方（位于「应用商店」之下）。插件不写 HTML/CSS，仅返回声明式 `UIPage` JSON，宿主使用 Ocean Paper 原生控件统一渲染，并在顶栏自动提供启用开关（`TogglePill`）、运行状态/取消任务与「卸载」入口。应用商店支持「全部 / 已安装 / 未安装」筛选，点击卡片会在商店内以只读编辑器渲染插件根目录说明文档（中文优先：中文界面读取 `README.md`，英文界面优先读取 `README.en.md` 并在缺失时回退 `README.md`；优先读本地安装包以支持离线，未安装时回退 GitHub 仓库文件，上限 512 KB UTF-8，支持相对路径图片与 GIF）。
- **两项强制校验规则（违规直接拒绝加载）**：
  1. **禁止 Emoji**：`manifest.json` 的 `name`、`author`、`summary` 以及 `UIPage` 的静态界面文案（标题、标签、按钮、占位符、`empty_text`、`treemap` 瓦片标签等）**严禁包含 Emoji**（`U+1F300–U+1FAFF`、`U+2600–U+27BF`、`U+FE00–U+FE0F`），图标仅通过 `manifest.icon` 双色 SVG 路径定义。用户笔记内容、路径等动态数据字段（如 `preview` 正文、`kv.value`、`log_list` 条目、`page.status`）放行用户原有字符以避免渲染失败，但插件自写文案仍应避免 Emoji。
  2. **严格 JSON 解码**：`manifest.json`（上限 256 KB，须为单个 JSON 对象）与 `UIPage` 均启用 `DisallowUnknownFields`，**严禁出现本文未定义的字段名**。
- **官方插件强制双语要求（发布门禁）**：所有官方插件（`blockmd-plugins/*`）**必须**：
  1. 完整支持英文界面：根据 `plugin.initialize` 传入的 `locale`（`en*` 为英文，其余为中文），确保英文模式下全部 `UIPage` 静态文案、状态栏与动作反馈均为自然英文，不得残留中文字符或中文标点；
  2. 提供中英文两版说明文档（中文优先方案 B）：根目录同时提供 `README.md`（中文版）与 `README.en.md`（英文版，不含中文），并随安装包一同打包进 Release 归档；
  3. 发版前必须通过 `block-plugin-check` 及插件仓库 `.github/workflows/release.yml` 的双语文档与英文界面检查，未通过禁止发布。社区第三方插件同样强烈建议遵循此规范。

---

## 2. 目录结构与 `manifest.json` 规范

插件根目录至少包含 `manifest.json` 与入口程序，官方插件强制提供 `README.md`（中文）与 `README.en.md`（英文）供应用商店详情页按语言展示（第三方插件同样推荐提供双语文档）：

```text
block-sync/
├── manifest.json
├── README.md
├── README.en.md
└── main.py
```

### `manifest.json` 完整定义（严禁多写未知字段）

```json
{
  "app": "blockmd",
  "schema_version": 1,
  "id": "block-sync",
  "name": {
    "zh": "同步",
    "en": "Sync"
  },
  "version": "1.0.0",
  "beta": true,
  "author": "Block Community",
  "repo": "blockmd-plugins/block-sync",
  "summary": {
    "zh": "支持本地目录镜像、WebDAV 与 Git 仓库的多端增量同步",
    "en": "Incremental notebook sync via local mirror, WebDAV, and Git"
  },
  "entry": {
    "command": ["python3", "-u", "main.py"]
  },
  "permissions": [
    "storage",
    "notes:read",
    "notes:write",
    "network",
    "events:note_saved"
  ],
  "surfaces": [
    "settings_page"
  ],
  "icon": {
    "view_box": "0 0 24 24",
    "fill_paths": ["M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16Z"],
    "stroke_paths": ["M7.5 10.5A5 5 0 0 1 16.2 8.5", "M16.5 13.5A5 5 0 0 1 7.8 15.5"],
    "accent_paths": ["M16.5 5.8V8.8H13.5", "M7.5 18.2V15.2H10.5"]
  }
}
```

### 字段约束
- `app`：必填固定字符串 **`"blockmd"`**（用于插件生态识别与自动采集验真）。
- `schema_version`：必填固定整数 `1`。
- `id`：必填，匹配正则 `^[a-z0-9][a-z0-9-]{1,47}$`（不可使用保留 ID `block-diagnostics`）。
- `name`：`zh` 与 `en` 均必填非空，禁止含 Emoji。
- `version`：标准 SemVer，支持可选预发布后缀（如 `"1.0.0"`、`"1.0.0-beta.1"`）。
- `beta`：可选布尔值（默认 `false`，正式版省略即可）。仅当插件在 `manifest.json` 中显式声明 `"beta": true` 时，Block 才会在应用商店、设置侧栏与插件页标题旁显示 `Beta` 标识，商店采集器也会自动同步到 `store-index.json`。转正时删掉该字段即可，宿主不做硬编码。
- `author`：必填非空字符串，禁止含 Emoji。
- `repo`：可选，GitHub 仓库标识 `"owner/repo"`（匹配 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`）。上架应用商店时若填写，必须与实际 GitHub 仓库一致（不区分大小写）。
- `summary`：`zh`（<=80 字符）与 `en`（<=140 字符）均必填，禁止含 Emoji。
- `entry.command`：非空字符串数组，工作目录固定为插件安装目录；`command[0]` 须为 `PATH` 可执行文件名或插件目录内相对普通文件（如 `"./bin/plugin"`）。Python 脚本必须传 `"-u"` 或每次写 `stdout` 后 `flush=True`。
- `permissions`：按需声明，仅允许以下枚举值且不可重复：
  - `"storage"`：读写插件私有键值存储（`host.storage.get` / `host.storage.set`）
  - `"notes:read"`：读取当前活动笔记、全部已注册笔记本、笔记列表/内容与宿主性能快照（`host.notes.active` / `host.notebooks.list` / `host.notes.list` / `host.notes.read` / `host.perf.snapshot`，以及 `plugin.initialize` 中的 `notebooks`）
  - `"notes:write"`：在已注册的笔记本内写入 Markdown 文件（`host.notes.write`）
  - `"network"`：声明插件会发起外部网络或远端命令请求（如 WebDAV / Git / S3）
  - `"events:note_saved"`：订阅宿主笔记保存事件（`plugin.event`）
- `surfaces`：必填固定为 `["settings_page"]`。
- `icon`：Ocean Paper 双色矢量图标，`view_box` 固定为 `"0 0 24 24"`，`stroke_paths` 至少含 1 条 SVG `d` 路径，`fill_paths` 与 `accent_paths` 可选。

---

## 3. 双向 JSON-RPC 2.0 通信协议

所有请求与响应均为单行 UTF-8 JSON（末尾 `\n`）。

### 3.1 宿主调用插件（Host -> Plugin）

1. **`plugin.initialize`**
   - 请求 `params`：
     ```json
     {
       "plugin_id": "block-sync",
       "data_dir": "/abs/path/to/apps-data/block-sync",
       "downloads_dir": "/Users/name/Downloads",
       "notebook_root": "/abs/path/to/notebook",
       "notebooks": ["/abs/path/to/notebook", "/abs/path/to/other-notebook"],
       "locale": "zh",
       "host_version": "1.0.696",
       "ui_capabilities": [
         "kv_tooltip", "treemap", "host_perf", "checklist", "form_layout",
         "multi_notebook", "card_collapse", "metric_tone", "log_columns", "file_picker",
         "action_feedback", "note_revision"
       ]
     }
     ```
     - `notebook_root` 仅在声明 `notes:read`、`notes:write` 或 `events:note_saved` 任一权限且宿主已打开笔记本时传入，否则为 `""`；用户切换活动笔记本时，宿主会关闭旧进程并为新笔记本重新启动并初始化。
     - `notebooks` 仅在声明 `notes:read` 时返回已注册的全部笔记本根目录数组，否则为 `[]`。
     - `downloads_dir`（默认 `~/Downloads`，环境变量 `BLOCKMD_DOWNLOADS_DIR`）：**凡涉及导出文件、备份归档或清理转移的目录，一律默认输出到 `downloads_dir`**（单文件直接输出到 `downloads_dir/<文件名>`，多文件收拢到一个干净文件夹内，严禁默认塞进 `apps-data` 隐藏目录或在笔记本内新建 `.trash-*` 等隐藏目录）。
     - `ui_capabilities` 可能在旧宿主中省略，用于按能力渐进启用特性：
       `kv_tooltip`（`kv` 行提示）、`treemap`（面积图控件）、`host_perf`（`host.perf.snapshot`）、`checklist`（多选勾选列表）、`form_layout`（`layout` 与 `group`）、`multi_notebook`（多笔记本列表与读写）、`card_collapse`（卡片折叠）、`metric_tone`（`metrics[].tone` 柔和色板）、`log_columns`（`log_list` 对齐列、`sub_text`/`ratio`/`tone`/`tooltip` 与 `→` 映射模式）、`file_picker`（`folder.accept_extensions`）、`action_feedback`（`plugin.action` 的 `ok`/`tone` 与内联/按钮反馈）、`note_revision`（`revision`/`expected_revision` 乐观并发）。
   - 返回 `result`：`{"ready": true}`。

2. **`plugin.render`**
   - 请求 `params`：`{"surface": "settings_page", "values": { ... }}`。
     - 首次打开、切换笔记本/当前笔记/笔记本列表、后台定时轮询及 `note.saved` 成功后刷新时省略 `values`。
     - 用户在界面编辑控件时（离散控件约 25ms 防抖，文本输入约 220ms 防抖），`values` 仅包含用户尚未同步的改动字段。将这些字段合并到持久配置后生成预览；需要显式提交的表单由 `plugin.action` 保存。
   - 返回 `result`：包含必填 `page`（严格解码）与可选 `values`。若本次在 `plugin.render` 中已持久化或主动重置了某些字段，将其写入 `result.values`，宿主会同步表单并清除对应字段的未同步标记。

3. **`plugin.action`**
   - 用户点击页面按钮时触发。校验器会调用初次渲染时可见的按钮。
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
     `values` 包含当前设置页全部表单控件（`segmented`、`select`、`input`、`folder`、`secret`、`switch`、`checklist`，含 `group` 内子控件）以 `id` 为键的最新值。建议合并持久化到 `host.storage.set`。
   - 返回 `result`（字段均可选）：
     ```json
     {
       "ok": true,
       "tone": "success",
       "values": { "protocol": "local_mirror" },
       "page": { "...": "更新后的完整 UIPage 对象" },
       "status": "已同步 2 篇笔记",
       "open_path": "/Users/name/Downloads/欢迎使用 Block.html",
       "reveal_path": "/Users/name/Downloads/欢迎使用 Block.html"
     }
     ```
     - **动作反馈（`ok` / `tone` / `status`）**：`tone` 支持 `"success"`、`"warning"`（别名 `"warn"`）、`"danger"`（别名 `"error"`）、`"info"`；未填 `tone` 时按 `ok` 判定（`true` 为 `success`，`false` 为 `danger`），均省略时宿主按 `status` 关键词推断。卡片内按钮返回 `danger`/`warning` 会在按钮上方显示提示条，`success`/`info` 在按钮旁显示单行状态；`header_actions` 在顶栏展示状态。`success` 态按钮会短暂切换为绿色勾选态（2.4s）；`action_id` 为 `test_conn`、以 `test_` 开头或以 `_test` 结尾的测试按钮会保持成功态直到用户修改表单。若返回 `page` 且 `page.status` 为空，宿主会自动填入 `result.status`。
     - **打开与定位输出文件（`open_path` / `reveal_path`）**：切勿在插件内自行调用 `open`/`xdg-open` 子进程。两者都必须是 **`downloads_dir` 内的绝对路径**（普通文件或目录，禁止符号链接越界），否则会被宿主静默忽略。`open_path` 禁止打开 `.app` 目录，且普通文件仅允许扩展名 `.html`、`.htm`、`.pdf`、`.txt`、`.md`、`.markdown`、`.csv`、`.tsv`、`.png`、`.jpg`、`.jpeg`、`.webp`、`.gif`、`.docx`、`.xlsx`、`.pptx`；其他格式（如 `.zip`）请使用 `reveal_path` 在系统文件管理器中定位显示。

4. **`plugin.event`**（需声明 `"events:note_saved"` 权限）
   - 请求 `params`：`{"event": "note.saved", "notebook_root": "/abs/path/to/notebook", "path": "welcome.md", "timestamp": "2026-11-18T10:00:00Z"}`
   - 成功时返回 `{"handled": true}`；若设置页已打开且空闲，宿主会自动再调一次 `plugin.render` 刷新界面。
   - 同一笔记高频保存时宿主会自动合并排队；返回 JSON-RPC 错误时，侧栏会标记错误状态并在插件页保留重试入口。其他插件通过 `host.notes.write` 实际修改笔记时也会派发给订阅者（不会回环通知写入者自身）。

5. **`plugin.shutdown`**
   - 请求 `params`：`{}`
   - 返回 `result`：`{"ok": true}` 后进程在 2 秒内正常退出。

---

### 3.2 插件调用宿主能力（Plugin -> Host）

插件在处理任意 `plugin.*` 请求期间，可向 `stdout` 发送 JSON-RPC 请求调用 `host.*`，并从 `stdin` 读取匹配 `id` 的响应后再返回外层 `plugin.*` 结果（同时在途 `host.*` 调用不超过 8 个）。

常见错误码：`-32001` 缺少权限、`-32002` 存储失败或超 8 MB、`-32003` 列举笔记失败、`-32004` 笔记读写失败、`-32005` 性能快照不可用、`-32006` 笔记版本冲突或编辑器有未保存修改、`-32601` 未知方法、`-32602` 参数无效。

笔记读写共同边界：`notebook_root` 省略时默认当前活动笔记本，传入时须精确命中宿主已注册笔记本；`path` 须为该笔记本内的**可见 Markdown 相对路径**（`.md`/`.markdown`，禁止 `.` 开头的目录或文件名、禁止 `..`、`:` 与符号链接，单文件 <= 16 MB）。

1. **`host.storage.get`**（需 `"storage"`）
   - 单键：`params: {"key": "config"}` -> 命中 `{"value": ..., "found": true}`，未命中 `{"value": null, "found": false}`
   - 全量：`params: {}` -> `{"data": {...}}`
2. **`host.storage.set`**（需 `"storage"`）
   - `params: {"key": "config", "value": {...}}` -> `{"ok": true}`
3. **`host.notes.active`**（需 `"notes:read"`）
   - `params: {}` -> `{"path": "welcome.md"}`（返回当前活动笔记本内正在编辑的笔记相对路径；未打开时返回 `""`）。
4. **`host.notebooks.list`**（需 `"notes:read"`）
   - `params: {}` -> `{"notebooks": [{"path": "/abs/path/to/notebook", "name": "notebook", "active": true}]}`（返回宿主已注册的全部笔记本，建议配合 `checklist` 控件让用户勾选范围）。
5. **`host.notes.list`**（需 `"notes:read"`）
   - `params: {"notebook_root": "/abs/path/to/notebook"}`（`notebook_root` 可省略）-> `{"notes": [{"path": "welcome.md", "size": 42, "mod_time": "2026-11-18T10:00:00Z"}]}`
6. **`host.notes.read`**（需 `"notes:read"`）
   - `params: {"notebook_root": "/abs/path/to/notebook", "path": "welcome.md"}`（`notebook_root` 可省略）-> `{"path": "welcome.md", "content": "# 欢迎...", "revision": "<SHA-256>"}`
7. **`host.notes.write`**（需 `"notes:write"`）
   - `params: {"notebook_root": "/abs/path/to/notebook", "path": "notes/synced.md", "content": "# 内容", "expected_revision": "<SHA-256>"}` -> `{"ok": true, "changed": true}`。
   - 若文件已存在且内容逐字节相同，直接返回 `{"ok": true, "changed": false}`（不写盘、不记版本历史、不触发事件）。
   - 先读后改时请原样传入读取到的 `revision` 作为 `expected_revision`；传 `""` 表示仅在文件不存在时创建；旧插件可省略该字段。
   - 遇到版本冲突、编辑器存在未保存修改或处于虚拟预览时返回 `-32006`，请重新读取后再合并修改。实际写入时宿主使用原子落盘、记录「插件保存」历史快照并刷新干净的编辑器标签页。
8. **`host.perf.snapshot`**（需 `"notes:read"`）
   - `params: {"refresh_disk": false}`（传 `true` 触发重扫磁盘）-> 返回宿主资源与磁盘分类占用快照：
     ```json
     {
       "cpu_percent": 1.2,
       "rss_bytes": 125829120,
       "total_bytes": 52428800,
       "last_scan": "2026-11-20T01:00:00Z",
       "scan_error": "",
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

---

## 4. 声明式 UI Schema（`UIPage`）精确定义

同一 `UIPage` 内：所有 `card.id` 唯一、所有表单控件 `item.id` 唯一、所有按钮 `button.id`（含 `header_actions` 与 `actions` 项）唯一。宿主已在顶栏自动提供插件启用开关（`TogglePill`）与「卸载」按钮，插件无需自己声明。

### `UIPage` 顶层结构
```json
{
  "title": "同步",
  "status": "上次同步 14:20",
  "header_actions": [
    { "id": "sync_now", "label": "立即同步", "variant": "primary" }
  ],
  "cards": [
    {
      "id": "target-card",
      "title": "存储目标",
      "items": [ ... ]
    }
  ]
}
```
- `title`：必填非空字符串。
- `status`：可选只读次级状态短文本（仅展示真实执行结果或最近运行时间，闲置或未配置时传 `""`；严禁写 `"已就绪"`、`"xxx · 已就绪"` 等占位文案）。
- `refresh_interval_ms`：可选定时刷新间隔（`500`–`60000` 毫秒，`0` 或省略表示不自动轮询；仅在插件页当前展示且空闲时生效）。
- `header_actions`：可选顶栏动作按钮数组；每项含 `id`（必填唯一）、`label`（必填）、`variant`（仅限 `"primary"` | `"secondary"` | `"danger"`）。
- `cards`：必填非空数组；每项含 `id`（必填唯一）、`title`（可选）、`meta`（可选右侧副信息）、`badge`（可选右侧胶囊徽章）、`collapsible` / `default_collapsed`（可选折叠配置卡片，宿主按笔记本与插件记住展开状态）、`items`（必填非空数组；若卡片内所有项均被 `visible_when` 隐藏，整个卡片自动隐藏）。

### 14 种 `UIItem` 控件（按 `kind` 区分）

**交互与布局准则**

- 选择本地文件夹使用 `folder`，需要同时允许选文件时附加 `accept_extensions`（如 `[".zip"]`）；远端路径和分支等文本使用 `input` 并给合理默认值；单项下拉用 `select`，多选列表（如勾选多个笔记本）用 `checklist`。
- 离散控件（`switch`、`segmented`、`select`、`checklist`、`folder`）切换后建议立即在 `plugin.render` 中持久化并通过 `result.values` 回传，不要强制用户再点「保存」；仅在包含远端地址、密钥等多项文本输入时提供「测试连接 / 保存」按钮。
- 涉及多笔记本同步或批量处理的插件，应通过 `host.notebooks.list` 列出全部笔记本并交由 `checklist` 勾选，只对勾选的笔记本执行操作。
- 任意 `UIItem` 支持可选字段 `layout`（`""` 默认横向行、`"stacked"` 上下堆叠、`"full"` 通栏分段，仅 `segmented` 支持 `"full"`，且 `"full"` 时可省略 `label`）。
- 柔和色板键（用于 `metrics[].tone`、`log_list.entries[].tone` 与 `treemap.tiles[].color`）：`"assets"`（绿）、`"config"`（蓝）、`"notes"`（靛紫）、`"history"`（紫）、`"recovery"`（青）、`"logs"`（红）、`"ai_sessions"`（琥珀），省略或未知值回退为中性底色。

任意 `UIItem`（含 `group.fields` 子项）均支持可选字段 **`visible_when`**，按其他控件当前值**零延迟动态显隐**：
```json
"visible_when": { "control_id": "protocol", "equals": "webdav" }
```
或匹配多个值：
```json
"visible_when": { "control_id": "provider", "in": ["r2", "s3"] }
```

1. **`segmented`（分段切换，适合 2–3 个短选项）**：`id` 必填，`label` 在 `layout != "full"` 时必填，`options` 至少 2 项，`value`（`string`）必须命中某个 `options[].value`，可选 `layout`（`""` | `"stacked"` | `"full"`）：
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
2. **`select`（下拉选单）**：`id`、`label` 必填，`options` 至少 1 项，`value`（`string`）必须命中某个 `options[].value`，`placeholder` 可选。
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
4. **`folder`（本地文件夹 / 可选文件选择器）**：`id`、`label`、`value` 必填，`value` 传完整路径或 `""`；可选 `layout`（`""` | `"stacked"`）与 `accept_extensions`（字符串数组，如 `[".zip"]`；提供后宿主额外渲染文件选择按钮）。选定路径以 `id` 为键进入表单 `values`：
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
6. **`switch`（布尔开关）**：`id`、`label` 必填，`value` 必须为 `bool`：
   ```json
   {
     "kind": "switch",
     "id": "auto_sync",
     "label": "保存笔记后自动同步",
     "value": true
   }
   ```
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
8. **`metrics`（多列数据指标宫格）**：不含 `id`/`label`，仅含非空 `metrics` 数组（每项 `label`、`value` 必填非空，`tone` 可选，取值见柔和色板键；宿主自动渲染为 2–4 列等宽卡片）：
   ```json
   {
     "kind": "metrics",
     "metrics": [
       { "label": "笔记篇数", "value": "128", "tone": "notes" },
       { "label": "附件体积", "value": "14.2 MB", "tone": "assets" }
     ]
   }
   ```
9. **`kv`（只读状态键值行）**：`rows` 为非空数组，每行填写 `label` 与 `value`。若 `ui_capabilities` 包含 `kv_tooltip`，可加 `tooltip` 在悬停时展示补充信息（勿放密码或令牌）：
   ```json
   {
     "kind": "kv",
     "rows": [
       { "label": "当前笔记本", "value": "团队笔记", "tooltip": "/Users/name/Documents/团队笔记" }
     ]
   }
   ```
10. **`preview`（实时视觉预览卡片）**：不含 `id`/`label`，必填 `preview` 对象：
    - `variant`：`"article"`（主题化文章排版预览）| `"cards"`（多卡片切分预览）| `"diff"`（处理前/处理后对照预览）| `"plan"`（默认，目标路径与待执行清单预览）
    - `palette`：可选 `"ocean"`（默认）| `"paper"`（浅色纸感）| `"ink"`（暗色墨夜）| `"warm"`（暖纸衬线）
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
    - `status` / `time`：右侧自动对齐的等宽列；若**全部条目**均传 `"status": "→"`，宿主自动切换为 `title → time` 对称双栏映射样式
    - `tone`：状态色（`"success"` | `"warning"`/`"warn"` | `"danger"`/`"error"` | `"info"`/`"accent"` 或柔和色板键），用于前导状态点或 `ratio` 占比条着色
    - `ratio`：`0`–`1` 浮点数；大于 `0` 时在行背景绘制占比进度条并隐藏前导状态点
    - `tooltip`：整行悬停提示
    ```json
    {
      "kind": "log_list",
      "empty_text": "暂无同步记录",
      "entries": [
        { "title": "welcome.md", "sub_text": "Work", "time": "12:30", "status": "成功", "tone": "success" }
      ]
    }
    ```
12. **`checklist`（多选勾选列表，推荐用于多笔记本勾选）**：`id` 必填，`label`、`empty_text` 可选，`value` 为已选 `string[]`，`options` 每项含 `value`（必填唯一）、`label`（必填）及可选 `detail` / `badge` / `tooltip`；多于 1 项时宿主自动提供「全选 / 取消全选」：
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
13. **`group`（并排表单分组）**：`fields` 至少包含 2 个表单项（仅允许 `input`、`secret`、`segmented`、`select`、`folder`、`switch`），宿主自动并排展示；未显式设 `layout` 的子项默认按 `"stacked"` 排布，各子项可独立声明 `visible_when`。
14. **`treemap`（磁盘/空间占比面积图）**：`tiles` 为必填非空数组，`id`、`total_bytes`（省略时取各瓦片 `bytes` 之和）、`summary` 可选：
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
        }
      ]
    }
    ```
    其中 `summary` 若存在，`total_value` 必填；每个 `tile` 的 `id`（唯一）与 `label` 必填，`color` 默认取 `tile.id`（柔和色板键）；每条 `entries` 的 `name` 必填，`path`/`count` 可选。

---

## 5. Python 标准库双向通信骨架

```python
#!/usr/bin/env python3
import json
import sys

class HostBridge:
    def __init__(self):
        self._seq = 1000

    def send(self, obj):
        sys.stdout.write(json.dumps(obj, ensure_ascii=False) + "\n")
        sys.stdout.flush()

    def call_host(self, method, params=None):
        self._seq += 1
        req_id = f"p-{self._seq}"
        self.send({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params or {}})
        while True:
            line = sys.stdin.readline()
            if not line:
                raise RuntimeError("stdin closed while awaiting host response")
            msg = json.loads(line)
            if str(msg.get("id")) == req_id:
                if "error" in msg and msg["error"]:
                    raise RuntimeError(msg["error"].get("message", "host error"))
                return msg.get("result", {})

    def serve(self, handler):
        while True:
            line = sys.stdin.readline()
            if not line:
                break
            line = line.strip()
            if not line:
                continue
            req = json.loads(line)
            method = req.get("method")
            req_id = req.get("id")
            try:
                result = handler(self, method, req.get("params") or {})
                self.send({"jsonrpc": "2.0", "id": req_id, "result": result})
                if method == "plugin.shutdown":
                    break
            except Exception as exc:
                self.send({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32000, "message": str(exc)}})
```

---

## 6. 本地全链路验收命令

以下命令会在临时沙盒笔记本与插件数据目录中，依次检查 `manifest.json`、目录安装、初始化、设置页渲染、顶层及 `group` 内 `segmented`/`select`/`switch` 选项组合渲染、初次渲染可见的按钮（并验证返回的 `open_path` / `reveal_path` 实际存在）、`note.saved` 事件投递和 `plugin.shutdown` 退出：

```bash
CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check /绝对路径/到你的插件目录
```

- 当插件 `manifest.repo` 为官方仓库（`blockmd-plugins/*`）时，校验器会**自动启用双语强制检查**（第三方插件可追加 `-bilingual` 主动开启）：
  1. 校验根目录同时存在 `README.md`（中文非空且含中文）与 `README.en.md`（英文非空且不含中文字符/中文标点）；
  2. 分别以 `locale="zh"` 与 `locale="en"` 启动插件并遍历所有控件选项组合与动作，校验英文模式下全部界面文案与反馈无中文残留，且中英文渲染结果不一致。

终端输出 `[PASS] Plugin "<id>" passed all Block App Store checks.` 且退出码为 `0` 表示协议检查通过。上传、同步和迁移等真实外部流程仍需使用配置好的临时笔记本验证结果。

---

## 7. 发布到应用商店（`blockmd-plugin` 自动采集）

Block 应用商店采用无审批自动采集机制，发布步骤如下：

1. 将插件代码推送到公开、非 Fork、未归档的 GitHub 仓库，确保仓库根目录（或 Release 压缩包唯一顶层目录）仅包含一份通过第 6 节验收的 `manifest.json`（含 `"app": "blockmd"`；若填写 `repo` 须与仓库 `owner/repo` 一致）。官方插件（`blockmd-plugins/*`）**必须**同时放置 `README.md`（中文）与 `README.en.md`（英文）并支持 `locale="en"` 英文界面；社区插件建议一并提供。
2. 在 GitHub 仓库主页右侧 **About -> Topics** 添加标签：**`blockmd-plugin`**。
3. 发布标准 **GitHub Release**（优先上传 `.tar.gz`/`.tgz`/`.zip` 附件，否则采集器回退 Release 源码包或默认分支源码包；归档内 `manifest.version` 须与商店索引保持一致）：
   - **官方推荐自动版本号与发布门禁**：直接复用 `blockmd-plugins/block-sync` 的 `.github/workflows/release.yml`（`Daily Release`）。仓库 `manifest.json` 仅保留主次版本基线（如 `"1.2.0"`），日常改动直接推送 `main`，无需手动改版本号或打 Tag；工作流每天 `00:00 UTC`（或手动触发 `workflow_dispatch`）检测代码或双语 README 变更，**打包前强制检查 `README.md` / `README.en.md` 双语文档与 `locale="en"` 英文界面（不通过则中止发布）**，通过后按 `<major>.<minor>.<提交总数>` 动态写入产物包 `manifest.json` 并发布 Release，不向 `main` 回写提交。
   - **手动发版**：也可推送 `vX.Y.Z` Tag 触发同工作流打包对应版本。
4. 商店采集器定时扫描 `topic:blockmd-plugin`，校验 `manifest.json` 与 `id` 唯一性（若多个仓库冲突使用同一 `id` 会同时跳过）、计算 `sha256` 并更新 `store-index.json`；商店安装时会核对 `sha256` 及 `id`/`version`/`repo`/`permissions`。开发调试期间，可在「设置 → 应用商店 → 手动安装」输入本地目录路径（或点击「本地目录」选择）、`owner/repo`、`github:owner/repo`、GitHub 仓库链接、HTTPS 压缩包 URL 或商店插件 `id` 即时安装。
