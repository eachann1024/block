# Block 应用商店插件开发指南（AI 自包含规范 v1 · `blockmd`）

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过全链路自动验收的插件。

---

## 1. 架构与硬边界

- **无侵入进程外沙箱**：
  - 安装路径：`<BlockAppData>/apps/<plugin-id>/`
  - 私有存储：`<BlockAppData>/apps-data/<plugin-id>/state.json`
  - 插件严禁读写宿主 `state.json` 或 AI 配置文件，卸载应用即可干净移除。
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
- `author`：必填非空字符串，禁止含 Emoji。
- `repo`：可选，GitHub 仓库标识 `"owner/repo"`（匹配 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`）。
- `summary`：`zh`（<=80 字符）与 `en`（<=140 字符）均必填，禁止含 Emoji。
- `entry.command`：非空字符串数组，工作目录固定为插件安装目录（相对可执行文件写 `"./bin/plugin"`）。Python 脚本必须传 `"-u"` 或每次写 `stdout` 后 `flush=True`。
- `permissions`：按需声明，仅允许以下枚举值且不可重复：
  - `"storage"`：读写插件私有键值存储（`host.storage.get` / `host.storage.set`）
  - `"notes:read"`：列出并读取当前笔记本内 `.md` 文件（`host.notes.list` / `host.notes.read`）
  - `"notes:write"`：在当前笔记本内写入 `.md` 文件（`host.notes.write`）
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
       "locale": "zh",
       "host_version": "1.0.379",
       "ui_capabilities": ["kv_tooltip"]
     }
     ```
     注：未打开笔记本文件夹时 `notebook_root` 可能为 `""`（此时 `host.notes.list` 返回 `[]`）。宿主同时提供 `downloads_dir`（默认 `~/Downloads`，环境变量 `BLOCKMD_DOWNLOADS_DIR`）：**凡涉及导出文件、备份归档或清理转移的目录，一律默认输出到 `downloads_dir`（单文件直接输出到 `downloads_dir/<文件名>`，多文件收拢到一个干净文件夹内，严禁默认塞进 `apps-data` 隐藏目录或在笔记本内新建 `.trash-*` 等隐藏目录）**。
     `ui_capabilities` 可能省略。插件可用它判断宿主支持的可选 UI 字段，例如 `kv_tooltip`。
   - 返回 `result`：`{"ready": true}`。

2. **`plugin.render`**
   - 请求 `params`：`{"surface": "settings_page", "values": { ... }}`（首次打开时 `values` 可能省略；用户在界面上切换任意 `segmented`、`select`、`switch`、`folder` 或编辑输入框时，宿主会携带当前未保存的最新 `values` 实时调用 `plugin.render` 以驱动实时预览刷新）。插件在渲染时应将 `params.get("values")` 覆盖合并到已存配置之上生成动态预览与选项（仅在 `plugin.action` 时再落盘修改）。
   - 返回 `result`：`{"page": <UIPage 对象>}`

3. **`plugin.action`**
   - 用户点击页面按钮时触发（校验器会自动遍历页面中所有按钮 `id` 逐个触发验收）：
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
       "values": { "protocol": "local_mirror" },
       "page": { "...": "更新后的完整 UIPage 对象" },
       "status": "已同步 2 篇笔记",
       "open_path": "/Users/name/Downloads/欢迎使用 Block.html",
       "reveal_path": "/Users/name/Downloads/欢迎使用 Block.html"
     }
     ```

4. **`plugin.event`**（需声明 `"events:note_saved"` 权限）
   - 请求 `params`：`{"event": "note.saved", "path": "welcome.md", "timestamp": "2026-11-18T10:00:00Z"}`
   - 返回 `result`：`{"handled": true}`

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
4. **`host.notes.list`**（需 `"notes:read"`）
   - `params: {}` -> `{"notes": [{"path": "welcome.md", "size": 42, "mod_time": "2026-11-18T10:00:00Z"}]}`
5. **`host.notes.read`**（需 `"notes:read"`）
   - `params: {"path": "welcome.md"}` -> `{"path": "welcome.md", "content": "# 欢迎..."}`
6. **`host.notes.write`**（需 `"notes:write"`）
   - `params: {"path": "notes/synced.md", "content": "# 内容"}` -> `{"ok": true}`（仅限相对路径，任何 `..` 或绝对路径越界均被宿主拦截）。

---

## 4. 声明式 UI Schema（`UIPage`）精确定义

同一 `UIPage` 内：所有 `card.id` 唯一、所有表单控件 `item.id` 唯一、所有按钮 `button.id`（含 `header_actions` 与 `actions` 项）唯一。宿主已自带「卸载」按钮，插件无需自己声明卸载按钮。

### `UIPage` 顶层结构
```json
{
  "title": "同步",
  "status": "本地镜像 · 已就绪",
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
- `status`：可选状态胶囊短文本。
- `header_actions`：可选顶栏动作按钮数组；每项含 `id`（必填唯一）、`label`（必填）、`variant`（仅限 `"primary"` | `"secondary"` | `"danger"`）。
- `cards`：必填非空数组；每项含 `id`（必填唯一）、`title`（必填）、`items`（必填非空数组）。

### 11 种 `UIItem` 控件（按 `kind` 区分）

交互设计准则

- 选择本地文件夹时直接使用 `folder`，点击即打开系统选择器。远端路径和分支等可编辑文本使用 `input`，并提供合适的默认值。`select` 用于真实的多项选择，例如笔记列表。
- 页面只展示完成当前任务所需的设置和状态。路径优先显示名称，完整路径放进宿主提供的悬停提示。预览仅在需要核对结果或确认变更时展示。

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
11. **`log_list`（运行记录列表）**：不含 `id`/`label`，`empty_text` 必填非空，`entries` 可为空数组（每项 `title` 与 `time` 必填非空，`status` 可选）：
    ```json
    {
      "kind": "log_list",
      "empty_text": "暂无同步记录",
      "entries": [
        { "title": "welcome.md", "time": "12:30:00", "status": "已同步" }
      ]
    }
    ```

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

在插件开发完成后执行以下命令，自动在隔离沙箱内完成 `manifest.json` 校验、商店安装模拟、`plugin.initialize`、`settings_page` 严格校验、所有按钮 `plugin.action` 触发、`note.saved` 事件投递与进程退出验收：

```bash
CGO_ENABLED=0 go -C /Users/eachann/Work/block-go run ./cmd/block-plugin-check /绝对路径/到你的插件目录
```

终端输出 `[PASS] Plugin "<id>" passed all Block App Store checks.` 且退出码为 `0` 即验收通过。

---

## 7. 发布到应用商店（`blockmd-plugin` 自动采集）

Block 应用商店采用无审批自动采集机制，发布步骤如下：

1. 将插件代码推送到公开 GitHub 仓库，确保仓库根目录（或 Release 压缩包顶层目录）包含通过第 6 节验收的 `manifest.json`（含 `"app": "blockmd"`）。
2. 在 GitHub 仓库主页右侧 **About -> Topics** 添加标签：**`blockmd-plugin`**。
3. 发布标准 **GitHub Release**（压缩包内 `manifest.version` 必须与 Release Tag 一致）：
   - **官方推荐自动版本号**：直接复用 `blockmd-plugins/block-sync` 的 `.github/workflows/release.yml`（`Daily Release`）。仓库 `manifest.json` 仅保留主次版本基线（如 `"1.2.0"`），日常改动直接推送 `main`，无需手动改版本号或打 Tag；工作流每天 `00:00 UTC`（或手动触发 `workflow_dispatch`）检测代码变更，按 `<major>.<minor>.<提交总数>` 动态写入产物包 `manifest.json` 并发布 Release，不向 `main` 回写提交。
   - **手动发版**：也可推送 `vX.Y.Z` Tag 触发同工作流打包对应版本。
4. 商店采集器定时扫描 `topic:blockmd-plugin`，自动校验 `manifest.json`、计算 `sha256` 并更新 `store-index.json`；用户也可在「应用商店」直接输入 `owner/repo` 或本地目录路径即时安装调试。
