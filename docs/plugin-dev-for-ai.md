# Block 应用商店插件开发指南（AI 自包含规范 v1 · `blockmd`）

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过本地协议检查的插件。

---

## 1. 架构与硬边界

- **独立进程**。只安装信任的插件。子进程使用 Block 的系统权限，清单权限仅控制宿主 RPC。外部安装链接会先打开商店，由用户确认安装。新安装的插件默认停用，用户启用后才会启动。
  - 安装路径：`<BlockAppData>/apps/<plugin-id>/`
  - 私有存储：`<BlockAppData>/apps-data/<plugin-id>/state.json`
  - 插件通过宿主 RPC 读写笔记和插件配置。卸载移除插件程序，并保留私有配置供重新安装。
- **跨语言双向 JSON-RPC 2.0**：宿主以独立子进程启动 `manifest.json` 中的 `entry.command`（推荐 `["python3", "-u", "main.py"]`、`["node", "main.mjs"]` 或编译后可执行文件），通过 UTF-8 **按行分隔（`\n`）的 JSON-RPC 2.0** 在 `stdin` / `stdout` 双向通信，`stderr` 仅用于调试输出。
- **方案 A 原生设置挂载**：已安装插件自动挂载在「设置」左侧导航栏分割线下方（位于「应用商店」之下）。插件不写 HTML/CSS，仅返回声明式 `UIPage` JSON，宿主使用 Ocean Paper 原生控件统一渲染，并在页面顶栏自动提供「卸载」入口。
- **两项强制校验规则（违规直接拒绝加载）**：
  1. `manifest.json`、`UIPage` 与动作返回的 `status` **严禁包含任何 Emoji 字符**（图标仅通过 `manifest.icon` 双色 SVG 路径定义）。
  2. 宿主启用严格 JSON 解码（`DisallowUnknownFields`），**严禁出现本文未定义的字段名**。

---

## 2. 目录结构与 `manifest.json` 规范

插件根目录至少包含 `manifest.json` 与入口程序：

```text
block-sync/
├── manifest.json
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
- `id`：必填，匹配正则 `^[a-z0-9][a-z0-9-]{1,47}$`。
- `name`：`zh` 与 `en` 均必填非空，禁止含 Emoji。
- `version`：标准 SemVer（如 `"1.0.0"`）。
- `beta`：可选布尔值（默认 `false`，正式版省略即可）。由插件自身按需声明；仅当插件在 `manifest.json` 中显式写 `"beta": true` 时，Block 才会在应用商店、设置侧栏与插件页标题旁显示 `Beta` 标识，商店采集器也会自动同步到 `store-index.json`。插件转正时删掉该字段即可，宿主不做任何硬编码。
- `author`：必填非空字符串，禁止含 Emoji。
- `repo`：可选，GitHub 仓库标识 `"owner/repo"`（匹配 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`）。
- `summary`：`zh`（<=80 字符）与 `en`（<=140 字符）均必填，禁止含 Emoji。
- `entry.command`：非空字符串数组，工作目录固定为插件安装目录（相对可执行文件写 `"./bin/plugin"`）。Python 脚本必须传 `"-u"` 或每次写 `stdout` 后 `flush=True`。
- `permissions`：按需声明，仅允许以下枚举值且不可重复：
  - `"storage"`：读写插件私有键值存储（`host.storage.get` / `host.storage.set`）
  - `"notes:read"`：列出并读取当前笔记本内 `.md` 文件（`host.notes.list` / `host.notes.read`）
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
     注：未打开笔记本文件夹时 `notebook_root` 可能为 `""`（此时 `host.notes.list` 返回 `[]`）；`notebooks` 为宿主已注册的全部笔记本根目录数组。宿主同时提供 `downloads_dir`（默认 `~/Downloads`，环境变量 `BLOCKMD_DOWNLOADS_DIR`）：**凡涉及导出文件、备份归档或清理转移的目录，一律默认输出到 `downloads_dir`（单文件直接输出到 `downloads_dir/<文件名>`，多文件收拢到一个干净文件夹内，严禁默认塞进 `apps-data` 隐藏目录或在笔记本内新建 `.trash-*` 等隐藏目录）**。
     `ui_capabilities` 可能省略。插件可用它判断宿主支持的可选 UI 字段与多笔记本能力（如 `checklist`、`multi_notebook`、`card_collapse`、`kv_tooltip`）。
   - 返回 `result`：`{"ready": true}`。

2. **`plugin.render`**
   - 请求 `params`：`{"surface": "settings_page", "values": { ... }}`。首次打开、切换笔记本与后台刷新时可以省略 `values`。用户编辑时，`values` 可能只有改动过的字段。将这些字段合并到持久配置后生成预览。需要保存的普通表单由 `plugin.action` 提交。
   - 返回 `result` 包含 `page`。可选的 `values` 仅列出本次已持久化或主动重置的字段，宿主用它更新表单。

3. **`plugin.action`**
   - 用户点击页面按钮时触发。校验器会调用初次渲染时显示的按钮。
   - 请求 `params`：
     ```json
     {
       "surface": "settings_page",
       "action_id": "save_config",
       "values": {
         "protocol": "local_mirror",
         "mirror_dir": "/Users/name/Sync"
       }
     }
     ```
     `values` 包含当前设置页全部表单控件（`segmented`、`select`、`input`、`folder`、`secret`、`switch`）以 `id` 为键的最新值。建议将其合并（merge）持久化到 `host.storage.set` 中。
   - 返回 `result`（字段均可选；`open_path` 会由宿主用系统默认浏览器/应用打开本地文件，`reveal_path` 会在访达/资源管理器中定位显示该本地文件或目录，切勿在插件内自行调用 `open`/`xdg-open` 子进程；`status` 严禁含 Emoji）：
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

4. **`plugin.event`**（需声明 `"events:note_saved"` 权限）
   - 请求 `params`：`{"event": "note.saved", "notebook_root": "/abs/path/to/notebook", "path": "welcome.md", "timestamp": "2026-11-18T10:00:00Z"}`
   - 成功时返回 `{"handled": true}`。失败时返回 JSON-RPC 错误，宿主可保留该笔记的重试入口。笔记发生实际变更时，宿主会通知订阅该事件的其他插件。

5. **`plugin.shutdown`**
   - 请求 `params`：`{}`
   - 返回 `result`：`{"ok": true}` 后进程正常退出。

---

### 3.2 插件调用宿主能力（Plugin -> Host）

插件在处理任意 `plugin.*` 请求期间，可向 `stdout` 发送 JSON-RPC 请求调用 `host.*`，并从 `stdin` 读取匹配 `id` 的响应后再返回外层 `plugin.*` 结果：

1. **`host.storage.get`**（需 `"storage"`）
   - 单键：`params: {"key": "config"}` -> 命中 `{"value": ..., "found": true}`，未命中 `{"value": null, "found": false}`
   - 全量：`params: {}` -> `{"data": {...}}`
2. **`host.storage.set`**（需 `"storage"`）
   - `params: {"key": "config", "value": {...}}` -> `{"ok": true}`
3. **`host.notes.active`**（需 `"notes:read"`）
   - `params: {}` -> `{"path": "welcome.md"}`（返回用户当前正在编辑的笔记相对路径；未打开笔记时返回 `""`）。
4. **`host.notebooks.list`**（需 `"notes:read"`）
   - `params: {}` -> `{"notebooks": [{"path": "/abs/path/to/notebook", "name": "notebook", "active": true}]}`（返回用户在 Block 中打开/注册的全部笔记本，建议配合 `checklist` 控件让用户按笔记本勾选同步范围）。
5. **`host.notes.list`**（需 `"notes:read"`）
   - `params: {"notebook_root": "/abs/path/to/notebook"}`（`notebook_root` 可省略，默认当前活动笔记本）-> `{"notes": [{"path": "welcome.md", "size": 42, "mod_time": "2026-11-18T10:00:00Z"}]}`
6. **`host.notes.read`**（需 `"notes:read"`）
   - `params: {"notebook_root": "/abs/path/to/notebook", "path": "welcome.md"}`（`notebook_root` 可省略）→ `{"path": "welcome.md", "content": "# 欢迎...", "revision": "<SHA-256>"}`
7. **`host.notes.write`**（需 `"notes:write"`）
   - `params: {"notebook_root": "/abs/path/to/notebook", "path": "notes/synced.md", "content": "# 内容", "expected_revision": "<SHA-256>"}` → `{"ok": true, "changed": true}`。
   - `notebook_root` 可省略。文件须位于已注册的笔记本内。对于先读后改的操作，把读取时的 `revision` 作为 `expected_revision` 原样提供。传空字符串表示只创建新文件。旧插件可省略该字段。
   - 遇到版本冲突或编辑器中有未保存改动，返回 `-32006`。先重新读取，再应用局部修改。宿主使用原子写入并刷新干净的编辑器标签页。
8. **`host.perf.snapshot`**（需 `"notes:read"`）
   - `params: {"refresh_disk": false}` -> 返回宿主 CPU、内存与分类磁盘占用快照（供性能监控类插件使用）。

---

## 4. 声明式 UI Schema（`UIPage`）精确定义

同一 `UIPage` 内：所有 `card.id` 唯一、所有表单控件 `item.id` 唯一、所有按钮 `button.id`（含 `header_actions` 与 `actions` 项）唯一。宿主已在顶栏自动提供插件启用开关（`TogglePill`）与「卸载」按钮，插件无需自己声明启用总开关或卸载按钮。

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
- `status`：可选只读次级状态短文本（仅用于展示真实执行结果或最近运行时间，闲置或未配置时传 `""`；严禁写 `"已就绪"`、`"xxx · 已就绪"` 等占位文案）。
- `refresh_interval_ms`：可选定时刷新间隔（`500`–`60000` 毫秒，`0` 或省略表示不自动轮询）。
- `header_actions`：可选顶栏动作按钮数组；每项含 `id`（必填唯一）、`label`（必填）、`variant`（仅限 `"primary"` | `"secondary"` | `"danger"`）。
- `cards`：必填非空数组；每项含 `id`（必填唯一）、`title`（可选）、`meta`（可选右侧副信息）、`badge`（可选标题旁小徽章）、`collapsible` / `default_collapsed`（可选折叠配置卡片）、`items`（必填非空数组）。

### 14 种 `UIItem` 控件（按 `kind` 区分）

交互设计准则

- 选择本地文件夹时直接使用 `folder`，点击即打开系统选择器；若需要选择文件可附加 `accept_extensions`（如 `[".zip"]`）。远端路径和分支等可编辑文本使用 `input`，并提供合适的默认值。`select` 用于单项下拉选择，`checklist` 用于多选列表（如勾选多个笔记本）。
- 布尔开关（`switch`）、分段（`segmented`）、下拉选单（`select`）、多选列表（`checklist`）与本地文件夹（`folder`）这类离散控件在用户切换后应立即在 `plugin.render` 中持久化生效，不要强制用户再额外点击「保存」；仅在包含远端地址、密钥等多项文本输入时提供「测试连接 / 保存」按钮。
- 涉及多笔记本同步或批量处理的插件，应通过 `host.notebooks.list` 列出全部笔记本并交由 `checklist` 勾选，只对勾选的笔记本执行操作。
- 任意 `UIItem` 可通过可选字段 `layout` 控制排布（`""` 默认横向标签、`"stacked"` 上下堆叠、`"full"` 通栏分段）。

任意 `UIItem` 均支持可选字段 **`visible_when`**，用于按其他表单控件当前值**零延迟动态显隐对应配置项**：
```json
"visible_when": { "control_id": "protocol", "equals": "webdav" }
```
或匹配多个枚举值：
```json
"visible_when": { "control_id": "provider", "in": ["r2", "s3"] }
```

1. **`segmented`（分段切换，适合 2–3 个短选项）**：`id`、`label` 必填，`options` 至少 2 项，`value`（`string`）必须命中某个 `options[].value`：
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
2. **`select`（下拉选单）**：`id`、`label` 必填，`options` 至少 1 项，`value`（`string`）必须命中某个 `options[].value`。
3. **`input`（单行文本输入框）**：`id`、`label` 必填，`value` 必须为 `string`，`placeholder` 可选：
   ```json
   {
     "kind": "input",
     "id": "endpoint_url",
     "label": "服务器地址",
     "value": "",
     "placeholder": "https://dav.example.com/dav"
   }
   ```
4. **`folder`（本地文件夹）**：`id`、`label`、`value` 必填，`value` 传完整路径，未选择时传空字符串。宿主负责打开系统选择器、显示文件夹名称和完整路径的悬停提示。选择后，完整路径以 `id` 为键进入表单 `values`。
   ```json
   { "kind": "folder", "id": "mirror_dir", "label": "同步文件夹", "value": "" }
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
8. **`metrics`（多列数据指标宫格）**：不含 `id`/`label`，仅含非空 `metrics` 数组（每项 `label` 与 `value` 均必填非空，宿主自动渲染为 2–4 列等宽指标卡片）：
   ```json
   {
     "kind": "metrics",
     "metrics": [
       { "label": "笔记篇数", "value": "128" },
       { "label": "附件体积", "value": "14.2 MB" }
     ]
   }
   ```
9. **`kv`（只读状态键值行）**：`rows` 为非空数组，每行填写 `label` 与 `value`。如果 `plugin.initialize` 的 `ui_capabilities` 包含 `kv_tooltip`，可在行内加 `tooltip` 字符串，由 Block 的通用组件在悬停时显示。兼容旧宿主时省略该字段。内容只放补充信息，避免放入密码或访问令牌。
   ```json
   {
     "kind": "kv",
     "rows": [
       { "label": "当前笔记本", "value": "团队笔记", "tooltip": "/Users/name/Documents/团队笔记" }
     ]
   }
   ```
10. **`preview`（实时视觉预览卡片）**：不含 `id`/`label`，必填 `preview` 对象：
    - `variant`：`"article"`（主题化文章排版预览）| `"cards"`（多卡片切分预览，如小红书分页）| `"diff"`（处理前/处理后对照预览）| `"plan"`（目标路径与待执行清单预览）
    - `palette`：可选 `"ocean"`（默认）| `"paper"`（浅色纸感）| `"ink"`（暗色墨夜）| `"warm"`（暖纸衬线）
    - `title`：必填标题。`badge`：可选的简短状态。`subtitle`：可选的简短副标题，例如输出文件名。
    - `body`：多行预览正文（`"article"` 支持 `# ` 一级标题、`## ` 二级标题、`> ` 引用块、`[1] ` 脚注行）
    - `before` / `after`：用于 `"diff"` 变体的转换前/转换后对照文本
    - `cards`：用于 `"cards"` 变体的卡片数组 `[{"badge": "封面", "title": "...", "body": "..."}]`
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
11. **`log_list`（运行记录 / 队列列表）**：`empty_text` 必填非空，`label` 可选，`entries` 可为空数组（每项 `title` 必填，`sub_text`、`time`、`status`、`tone`、`ratio`、`tooltip` 均可选）：
    ```json
    {
      "kind": "log_list",
      "empty_text": "暂无同步记录",
      "entries": [
        { "title": "welcome.md", "time": "12:30", "status": "成功", "tone": "success" }
      ]
    }
    ```
12. **`checklist`（多选勾选列表，推荐用于多笔记本勾选）**：`id` 必填，`label` 可选，`value` 为已选 `string[]`，`options` 每项含 `value`、`label` 及可选 `detail` / `badge` / `tooltip`，宿主自动提供「全选 / 取消全选」：
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
13. **`group`（并排表单分组）**：`fields` 至少包含 2 个表单项（支持 `input`、`secret`、`segmented`、`select`、`folder`、`switch`），宿主将其自动并排展示。
14. **`treemap`（磁盘/空间占比面积图）**：包含 `tiles` 非空数组，以及可选 `summary`、`total_bytes`（供空间诊断类插件使用）。

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

以下命令使用临时笔记本和插件数据目录，检查清单、安装、初始化、设置页、初次渲染可见的按钮、保存事件投递和进程退出。

```bash
CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check /绝对路径/到你的插件目录
```

终端输出 `[PASS] Plugin "<id>" passed all Block App Store checks.` 且退出码为 `0` 表示协议检查通过。上传、同步和迁移仍需使用配置好的临时笔记本验证结果。

---

## 7. 发布到应用商店（`blockmd-plugin` 自动采集）

Block 应用商店采用无审批自动采集机制，发布步骤如下：

1. 将插件代码推送到公开 GitHub 仓库，确保仓库根目录（或 Release 压缩包顶层目录）包含通过第 6 节验收的 `manifest.json`（含 `"app": "blockmd"`）。
2. 在 GitHub 仓库主页右侧 **About -> Topics** 添加标签：**`blockmd-plugin`**。
3. 发布标准 **GitHub Release**（压缩包内 `manifest.version` 必须与 Release Tag 一致）：
   - **官方推荐自动版本号**：直接复用 `blockmd-plugins/block-sync` 的 `.github/workflows/release.yml`（`Daily Release`）。仓库 `manifest.json` 仅保留主次版本基线（如 `"1.2.0"`），日常改动直接推送 `main`，无需手动改版本号或打 Tag；工作流每天 `00:00 UTC`（或手动触发 `workflow_dispatch`）检测代码变更，按 `<major>.<minor>.<提交总数>` 动态写入产物包 `manifest.json` 并发布 Release，不向 `main` 回写提交。
   - **手动发版**：也可推送 `vX.Y.Z` Tag 触发同工作流打包对应版本。
4. 商店采集器定时扫描 `topic:blockmd-plugin`，自动校验 `manifest.json`、计算 `sha256` 并更新 `store-index.json`；用户也可在「应用商店」直接输入 `owner/repo` 或本地目录路径即时安装调试。
