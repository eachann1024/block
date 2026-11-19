# Block 应用商店插件开发指南（AI 自包含规范 v1 · `blockmd`）

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过本地协议检查的插件。

---

## 1. 架构与硬边界

- **独立进程与安装模型**：只安装信任的插件。子进程使用 Block 的系统权限，清单 `permissions` 仅控制宿主 RPC。
  - 安装路径：`<BlockAppData>/apps/<plugin-id>/`
  - 私有存储：`<BlockAppData>/apps-data/<plugin-id>/state.json`（停用状态与目录缓存位于 `<BlockAppData>/apps-cache/`）
  - 外部深度链接（`block://`、`blockmd://` 的 `store` / `install`，查询参数按 `repo` → `url` → `id` 优先级匹配当前目录中的 `id`、`repo` 或精确 `download_url`）仅打开应用商店或对应详情页，未命中停留在商店列表，不自动安装。
  - 新安装的插件默认停用，用户手动启用后才会启动；同 `id` 升级保留原有启用状态。商店卡片的「更新」按钮仅在目录版本高于已装版本时出现；手动安装同 `id` 不要求版本递增。
  - 替换机制：用户在应用商店或手动安装中主动点击安装/更新，宿主会先校验候选包与现有包清单、停止旧进程，再通过两步目录重命名配合 `.previous` 备份完成可恢复替换，允许跨来源或新增权限；非交互接口默认拒绝跨来源、旧包无来源记录或新增权限的替换。桌面卸载移除插件程序，并保留私有配置供重新安装。
  - **保留 ID**：`block-diagnostics` 对应 Block 内置「性能与占用」窗口，宿主拒绝安装与启用该 ID、设置侧栏不挂载，远程/缓存索引合并时也会过滤（离线无缓存回退内置索引或历史旧安装仍可能显示条目，但无法安装或启用）。
- **跨语言双向 JSON-RPC 2.0**：宿主以独立子进程启动 `manifest.json` 中的 `entry.command`（推荐 `["python3", "-u", "main.py"]`、`["node", "main.mjs"]` 或编译后可执行文件），通过 UTF-8 **按行分隔（`\n`）的 JSON-RPC 2.0** 在 `stdin` / `stdout` 双向通信，`stderr` 仅用于调试（宿主用 64 KiB 环形缓冲区保留尾部）。
  - `entry.command[0]` 必须是 `PATH` 可执行文件名（走 `LookPath`），或插件目录内解析后的相对普通文件（如 `"./bin/plugin"`，禁止符号链接、冒号或越出插件目录）。
  - **环境变量白名单**：宿主仅显式传入 `PATH`、`HOME`、`TMPDIR`、`PYTHONUNBUFFERED=1`、`BLOCK_PLUGIN_ID`、`BLOCK_DATA_DIR`、`BLOCKMD_PLUGIN_ID`、`BLOCKMD_DATA_DIR`、`BLOCKMD_DOWNLOADS_DIR`，不继承其他宿主环境变量。
  - **超时与进程终止**：首载 15s、编辑预览 8s、定时轮询 5s 由初始化与渲染共享（含同插件 RPC 锁等待）；`plugin.action` 120s 含可取消的排队与表单补刷（顶栏提供「取消任务」）；`plugin.event` 45s 在拿到插件 RPC 锁后起算，由初始化、事件与后续刷新共享；宿主回写 `host.*` 响应超时 5s；`plugin.shutdown` 给 2s 优雅退出窗口，超时强杀后再最多等 1s。macOS/Linux 在独立进程组（`Setpgid`）运行并在超时/强杀时终止整组，其他平台终止直接子进程。
  - **体积与打包上限**：
    - 插件发往宿主（`stdout`）的单行 JSON-RPC 上限 **4 MiB**（含 JSON 封装、转义与换行；宿主发往插件 `stdin` 无此扫描上限）。
    - 同时在途 `host.*` 调用上限 **8** 个（超出将中止插件进程）。
    - 私有存储 `state.json` 序列化上限 **8 MiB**（单次 `host.storage.set` 行仍受 4 MiB 上限约束）。
    - 笔记读取及写入前读取旧文件上限 **16 MiB**；`host.notes.write` 传入的新正文同时受 4 MiB 单行传输上限约束。
    - 安装归档下载 <= **64 MiB**、单文件 <= **32 MiB**、解包或本地目录复制总计 <= **256 MiB**（tar 解压流 <= **320 MiB**）、路径/条目数 <= **8192**（含推导父目录）、单路径 <= 1024 字节且 `< 32` 级目录、禁止 `\` 与 `:`、仅允许普通文件与目录（不支持符号链接）。
- **原生设置挂载与商店详情页**：已安装插件自动挂载在「设置」左侧导航栏分割线下方（位于「应用商店」之下），设置顶栏显示 `manifest.name`，并自动提供启用开关（`TogglePill`）、运行状态/取消任务与「卸载」入口。应用商店支持「全部 / 已安装 / 未安装」筛选；点击卡片在商店内以只读编辑器渲染插件根目录 README（本地支持大小写不敏感的 `README.md`、`README.markdown`、`README.txt`、`README`，上限 512 KiB UTF-8；缺失、为空或读取失败且存在有效仓库时回退 GitHub README）。详情页支持相对路径图片、SVG、GIF（超动画预算可降为静态）与受限视频（最多 24 项媒体，其中视频最多 2 项，单张图片 <= 6 MiB，单个视频 <= 24 MiB）。
- **两项强制校验规则（违规直接拒绝加载）**：
  1. **禁止 Emoji**：`manifest.json` 的 `name`、`author`、`summary` 以及 `UIPage` 的静态界面文案（`page.title`、`card.title`、控件 `label`、按钮 `label`、`placeholder`、`empty_text`、`kv.rows[].label`、`metrics[].label`、`checklist.options[].badge`、`treemap` 瓦片 `label`/`detail` 与 `summary.total_label`、`preview.badge`/`cards[].badge`）**严禁包含 Emoji**（`U+1F300–U+1FAFF`、`U+2600–U+27BF`、`U+FE00–U+FE0F`），图标仅通过 `manifest.icon` 双色 SVG 路径定义。用户笔记内容、路径等动态字段（如 `page.status`、`card.meta`、`options[].label`/`detail`、`kv.rows[].value`、`metrics[].value`、`preview` 标题与正文、`log_list.entries`）放行用户原有字符，但插件自写文案仍应避免 Emoji。
  2. **严格 JSON 解码**：`manifest.json`（上限 256 KiB，须为单个 JSON 对象）与 `UIPage` 启用 `DisallowUnknownFields`，**严禁出现未定义字段名**（外层 JSON-RPC 包与 `host.*` 参数使用普通解码；`UIItem` 为共享结构，已知字段仅在消费它的 `kind` 上生效）。

---

## 2. 目录结构与 `manifest.json` 规范

插件根目录至少包含 `manifest.json` 与入口程序，推荐同时提供 `README.md` 供应用商店详情页展示：

```text
example-sync/
├── manifest.json
├── README.md
└── main.py
```

### `manifest.json` 完整示例（请替换为你自己的 `id` 与 `repo`，严禁多写未知字段）

```json
{
  "app": "blockmd",
  "schema_version": 1,
  "id": "example-sync",
  "name": {
    "zh": "示例同步",
    "en": "Example Sync"
  },
  "version": "1.0.0",
  "beta": true,
  "author": "Your Name",
  "repo": "your-org/example-sync",
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
- `version`：必填，匹配正则 `^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`（如 `"1.0.0"`、`"1.0.0-beta.1"`，不支持 `+build` 元数据；建议使用规范 SemVer，不要加前导零）。
- `beta`：可选布尔值（默认 `false`，正式版省略即可）。仅当插件在 `manifest.json` 中显式声明 `"beta": true` 时，Block 才会在应用商店、设置侧栏与插件页标题旁显示 `Beta` 标识，商店采集器也会自动同步到 `store-index.json`。转正时删掉该字段即可，宿主不做硬编码。
- `author`：必填非空字符串，禁止含 Emoji。
- `repo`：可选，GitHub 仓库标识 `"owner/repo"`（匹配 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`）。上架应用商店时若填写，必须与实际 GitHub 仓库一致（不区分大小写）。
- `summary`：`zh`（<= 80 个 Unicode 字符）与 `en`（<= 140 个 Unicode 字符）均必填，禁止含 Emoji。
- `entry.command`：非空字符串数组，工作目录固定为插件安装目录；`command[0]` 须为 `PATH` 可执行文件名或插件目录内相对普通文件（如 `"./bin/plugin"`）。Python 脚本必须传 `"-u"` 或每次写 `stdout` 后 `flush=True`。
- `permissions`：可选数组（按需声明），仅允许以下枚举值且不可重复：
  - `"storage"`：读写插件私有键值存储（`host.storage.get` / `host.storage.set`）
  - `"notes:read"`：读取当前活动笔记、全部已注册笔记本、笔记列表/内容与宿主性能快照（`host.notes.active` / `host.notebooks.list` / `host.notes.list` / `host.notes.read` / `host.perf.snapshot`，以及 `plugin.initialize` 中的 `notebooks`）
  - `"notes:write"`：在已注册的笔记本内写入 Markdown 文件（`host.notes.write`）
  - `"network"`：声明插件会发起外部网络或远端命令请求（如 WebDAV / Git / S3）
  - `"events:note_saved"`：订阅宿主笔记保存事件（`plugin.event`）
- `surfaces`：必填固定为 `["settings_page"]`。
- `icon`：Ocean Paper 双色矢量图标，`view_box` 固定为 `"0 0 24 24"`，`stroke_paths` 至少含 1 条合法 SVG `d` 路径，`fill_paths` 与 `accent_paths` 可选。

---

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
       "host_version": "1.0.696",
       "ui_capabilities": [
         "kv_tooltip", "treemap", "host_perf", "checklist", "form_layout",
         "multi_notebook", "card_collapse", "metric_tone", "log_columns", "file_picker",
         "action_feedback", "note_revision"
       ]
     }
     ```
     - **运行时上下文**：`notebook_root` 仅在声明 `notes:read`、`notes:write` 或 `events:note_saved` 任一权限且目标笔记本非空时传入，否则为 `""`；`notebooks` 仅在声明 `notes:read` 时填充，否则为 `[]`。切换桌面笔记本不会立刻重启全部插件，仅在下次调用该插件且目标根目录与当前运行时上下文不同（含后台 `note.saved` 事件）时，宿主才关闭旧进程并按新 `notebook_root` 重新初始化。
     - `downloads_dir`（默认 `~/Downloads`，环境变量 `BLOCKMD_DOWNLOADS_DIR`）：**凡涉及导出文件、备份归档或清理转移的目录，一律默认输出到 `downloads_dir`**（单文件直接输出到 `downloads_dir/<文件名>`，多文件收拢到一个干净文件夹内，严禁默认塞进 `apps-data` 隐藏目录或在笔记本内新建 `.trash-*` 等隐藏目录）。
     - `ui_capabilities` 可能在旧宿主中省略，用于渐进启用特性：
       `kv_tooltip`（`kv` 行提示）、`treemap`（空间分布图控件）、`host_perf`（`host.perf.snapshot`；注意本地 CLI 校验器尚未注入性能回调，调用会返回 `-32005`，插件应做降级处理）、`checklist`（多选勾选列表）、`form_layout`（`layout` 与 `group`）、`multi_notebook`（多笔记本列表与读写）、`card_collapse`（卡片折叠）、`metric_tone`（`metrics[].tone` 柔和色板）、`log_columns`（`log_list` 对齐列、`sub_text`/`ratio`/`tone`/`tooltip` 与 `→` 映射模式）、`file_picker`（`folder.accept_extensions`）、`action_feedback`（`plugin.action` 的 `ok`/`tone` 与内联/按钮反馈）、`note_revision`（`revision`/`expected_revision` 乐观并发）。
   - 返回 `result`：`{"ready": true}`。

2. **`plugin.render`**
   - 请求 `params`：`{"surface": "settings_page", "values": { ... }}`。
     - 首次打开、切换笔记本/当前笔记/笔记本列表、动作前表单补刷、定时轮询及 `note.saved` 成功后刷新时省略 `values`。
     - 用户在界面编辑控件时（离散控件约 25ms 防抖，文本输入约 220ms 防抖），`values` 仅包含用户尚未同步的改动字段。将这些字段合并到持久配置后生成预览；需要显式提交的表单由 `plugin.action` 保存。
   - 返回 `result`：包含必填 `page`（严格解码）与可选 `values`。
     - **值合并规则**：仅首次加载、编辑预览刷新与动作前表单补刷会消费 `result.values`（更新表单并清除对应字段的未同步标记）；定时轮询（`refresh_interval_ms`）与 `note.saved` 成功后的刷新只更新 `page`、忽略 `result.values`。编辑预览中 `page` 的控件默认值仅补全缓存里尚不存在的新键。

3. **`plugin.action`**
   - 用户点击页面按钮时触发。注意：本地校验器会调用初始 `UIPage` 中的**全部** `header_actions` 与卡片顶层 `actions` 按钮（不判断 `visible_when` 与卡片折叠状态），需避免默认配置下触发非预期外部副作用。
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
     - **打开与定位输出文件（`open_path` / `reveal_path`）**：切勿在插件内自行调用 `open`/`xdg-open` 子进程。两者都必须是 **`downloads_dir` 内已存在的绝对路径**（普通文件或目录，且路径沿途不能包含任何符号链接组件，即使目标仍在 `downloads_dir` 内），否则会被宿主静默忽略。`open_path` 禁止打开 `.app` 目录，且普通文件仅允许 `.html`、`.htm`、`.pdf`、`.txt`、`.md`、`.markdown`、`.csv`、`.tsv`、`.png`、`.jpg`、`.jpeg`、`.webp`、`.gif`、`.docx`、`.xlsx`、`.pptx`；其余格式（如 `.zip`）请使用 `reveal_path`（Linux 下对文件打开其父目录）。即便 `ok: false`，宿主仍会处理非空路径，因此失败动作应省略这两个字段。

4. **`plugin.event`**（需声明 `"events:note_saved"` 权限）
   - 请求 `params`：`{"event": "note.saved", "notebook_root": "/abs/path/to/notebook", "path": "welcome.md", "timestamp": "2026-11-18T10:00:00Z"}`
   - 仅投递给已启用的订阅插件，由已接入该钩子的落盘入口（块编辑保存与其他插件通过 `host.notes.write` 产生的实际变更，不含写入者自身，不包含源码编辑保存分支）触发；`timestamp` 为每次派发/重试时的 UTC 时间。插件应以事件参数自带的 `notebook_root` 与 `path` 为准。
   - 宿主按 `(plugin_id, notebook_root, 绝对路径)` 合并排队（处理期间同文件再次保存会再排一次，跨文件无全局顺序保证）。返回任意非错误响应（即便 `{"handled": false}`）均视为成功；业务失败须返回 JSON-RPC `error`，宿主才会在侧栏标错并在空闲、同路径未重新排队且无更高优先错误时展示重试入口（可能来自另一笔记本）。成功后若该笔记本已有缓存页且无忙碌动作，宿主会自动触发 `plugin.render` 刷新（即使当前未停留在插件页）。

5. **`plugin.shutdown`**
   - 请求 `params`：`{}`
   - 推荐返回 `{"ok": true}`（宿主不校验响应内容），随后在 2 秒内正常退出。

---

### 3.2 插件调用宿主能力（Plugin -> Host）

插件在处理任意 `plugin.*` 请求期间，可向 `stdout` 发送 JSON-RPC 请求调用 `host.*`，并从 `stdin` 读取匹配 `id` 的响应后再返回外层 `plugin.*` 结果（同时在途不超过 8 个）。

常见错误码：`-32001` 缺少权限、`-32002` 存储失败或超 8 MiB、`-32003` 列举笔记失败（含未注册笔记本）、`-32004` 笔记文件打开/读写失败、`-32005` 性能快照回调不可用或返回错误、`-32006` 编辑器脏状态/虚拟预览或版本冲突、`-32601` 未知方法、`-32602` 参数无效（含 `read`/`write` 未注册笔记本或非法路径）。

笔记路径边界：支持扩展名 `.md`、`.markdown`、`.mdown`、`.mkd`、`.mkdn`、`.mdwn`、`.mdtxt`、`.mdtext`、`.mdx`（不区分大小写）。显式 `notebook_root` 会去空白并转绝对路径后与已注册笔记本精确匹配；省略时使用当前运行时上下文根。`host.notes.list` 跳过 `.` 开头项与非普通文件；`host.notes.read`/`write` 先对相对路径做 `Clean` 归一化，再要求位于笔记本内、无 `:`、无 `.` 开头路径段、无符号链接且旧文件/读取上限 <= 16 MiB。

1. **`host.storage.get`**（需 `"storage"`）
   - 参数解码失败按空对象处理。单键：`{"key": "config"}` -> 命中 `{"value": ..., "found": true}`，未命中 `{"value": null, "found": false}`；全量：`{}` -> `{"data": {...}}`。
2. **`host.storage.set`**（需 `"storage"`）
   - `{"key": "config", "value": {...}}` -> `{"ok": true}`。`key` 须为非空白字符串（否则 `-32602`）；省略 `value` 会存成 `null`。
3. **`host.notes.active`**（需 `"notes:read"`，忽略参数）
   - `{}` -> `{"path": "welcome.md"}`（返回宿主缓存的活动标签相对路径，可能为空、非 Markdown 或尚未更新）。
4. **`host.notebooks.list`**（需 `"notes:read"`，忽略参数）
   - `{}` -> `{"notebooks": [{"path": "/abs/path/to/notebook", "name": "notebook", "active": true}]}`（`active` 反映当前运行时上下文根，建议配合 `checklist` 控件勾选范围）。
5. **`host.notes.list`**（需 `"notes:read"`）
   - `{"notebook_root": "/abs/path/to/notebook"}`（可选；解码失败按省略处理；省略且运行时根为空时返回 `[]`；传未注册笔记本报 `-32003`）-> `{"notes": [{"path": "welcome.md", "size": 42, "mod_time": "2026-11-18T10:00:00Z"}]}`
6. **`host.notes.read`**（需 `"notes:read"`）
   - `{"notebook_root": "/abs/path/to/notebook", "path": "welcome.md"}`（`notebook_root` 可选，省略且运行时根为空报 `-32602`；`path` 必填）-> `{"path": "welcome.md", "content": "# 欢迎...", "revision": "<64位小写SHA-256>"}`
7. **`host.notes.write`**（需 `"notes:write"`）
   - `{"notebook_root": "/abs/path/to/notebook", "path": "notes/synced.md", "content": "# 内容", "expected_revision": "<64位小写SHA-256>"}` -> `{"ok": true, "changed": true}`。
   - **必须始终显式传入字符串 `content`**（省略或 `null` 会按空字符串写入）；`content` 需满足单行 4 MiB 传输预算。
   - **冲突与幂等判定顺序**：
     1. 桌面端先检查编辑器会话：若目标笔记存在未保存修改或处于虚拟预览，直接返回 `-32006`（即使目标字节相同；此类冲突需用户先保存或退出预览）。
     2. 读取磁盘旧文件并逐字节比较：若文件已存在且内容完全相同，直接返回 `{"ok": true, "changed": false}`（即使 `expected_revision` 为 `""` 或旧哈希，也不报错、不写盘、不记历史、不触发事件）。
     3. 核对 `expected_revision`：省略或传 `null` 跳过校验；传 `""` 要求文件尚不存在；传读取时的 64 位小写 SHA-256 哈希则要求磁盘当前内容匹配，否则返回 `-32006`（不提供对外部写入进程的原子 CAS）。
   - 实际写入时原子落盘、尽力记录「插件保存」版本历史、刷新干净的编辑器标签页并向其他订阅插件派发 `note.saved`。响应不含新 `revision`，连续修改前请重新 `host.notes.read`。
8. **`host.perf.snapshot`**（需 `"notes:read"`）
   - `{"refresh_disk": false}`（可选，解码失败按 `false`；首次调用即使传 `false` 也会触发磁盘扫描）-> 返回宿主资源与 7 类磁盘占用摘要（分类 ID：`notes`、`assets`、`history`、`recovery`、`ai_sessions`、`logs`、`config`；每类 `entries` 最多前 6 条摘要；`scan_error`、`file_count`、`entries` 与明细 `path`/`count` 可省略；磁盘扫描异常写在成功响应的 `scan_error` 中，仅回调不可用或失败时返回 `-32005`）：
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

---

## 4. 声明式 UI Schema（`UIPage`）精确定义

同一 `UIPage` 内：所有 `card.id` 唯一、所有表单控件 `item.id` 唯一、所有按钮 `button.id`（含 `header_actions` 与 `actions` 项）唯一。宿主已在顶栏自动提供插件启用开关（`TogglePill`）与「卸载」按钮，插件无需自己声明。

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
- `title`：必填非空字符串（桌面设置顶栏展示 `manifest.name`，但 `UIPage.title` 仍须提供）。
- `status`：可选只读次级状态短文本（仅展示真实执行结果或最近运行时间，闲置或未配置时传 `""`；严禁写 `"已就绪"` 等占位文案）。
- `refresh_interval_ms`：可选定时刷新间隔（`500`–`60000` 毫秒，`0` 或省略表示不自动轮询；仅在最近绘制时间窗口内且空闲时触发）。
- `header_actions`：可选顶栏动作按钮数组；每项含 `id`（必填唯一）、`label`（必填）、`variant`（仅限 `"primary"` | `"secondary"` | `"danger"`）。
- `cards`：必填非空数组；每项含 `id`（必填唯一）、`title`（可选）、`meta`（可选右侧副信息）、`badge`（可选右侧胶囊徽章）、`collapsible` / `default_collapsed`（可选折叠配置卡片，展开状态保存在当前控制器内存中）、`items`（必填非空数组；若卡片内所有顶层项均被 `visible_when` 隐藏，整个卡片自动隐藏）。

### 14 种 `UIItem` 控件（按 `kind` 区分）

**交互与布局准则**

- 选择本地文件夹使用 `folder`，需要同时允许选文件时附加 `accept_extensions`（如 `[".zip"]`）；远端路径和分支等文本使用 `input` 并给合理默认值；单项下拉用 `select`，多选列表（如勾选多个笔记本）用 `checklist`。
- 离散控件（`switch`、`segmented`、`select`、`checklist`、`folder`）切换后建议立即在 `plugin.render` 中持久化并通过 `result.values` 回传，不要强制用户再点「保存」；仅在包含远端地址、密钥等多项文本输入时提供「测试连接 / 保存」按钮。
- 涉及多笔记本同步或批量处理的插件，应通过 `host.notebooks.list` 列出全部笔记本并交由 `checklist` 勾选，只对勾选的笔记本执行操作。
- `layout` 合法值为 `""` | `"stacked"` | `"full"`：`"stacked"` 由 `segmented`/`folder`/`input`/`secret` 消费，`"full"` 仅 `segmented` 消费（且此时可省略 `label`）。
- 柔和色板键（用于 `metrics[].tone`、`log_list` 占比条与 `treemap.tiles[].color`）：`"assets"`（绿）、`"config"`（蓝）、`"notes"`（靛紫）、`"history"`（紫）、`"recovery"`（青）、`"logs"`（红）、`"ai_sessions"`（琥珀），省略或未知值回退中性底色。

任意 `UIItem`（含 `group.fields` 子项）均支持可选字段 **`visible_when`**，按其他控件当前值**零延迟动态显隐**（`control_id` 必填，至少提供 `equals` 或非空 `in`；`in` 为字符串数组且非空时优先于 `equals`；`equals: true` 与 `equals: "true"` 按 JSON 类型严格区分；未知 `control_id` 视为隐藏；若需按条件隐藏整组避免卡片只剩空标题，请将 `visible_when` 写在 `group` 上）：
```json
"visible_when": { "control_id": "protocol", "equals": "webdav" }
```
或匹配多个字符串值：
```json
"visible_when": { "control_id": "provider", "in": ["r2", "s3"] }
```

1. **`segmented`（分段切换，适合 2–3 个短选项）**：`id` 必填，`label` 在 `layout != "full"` 时必填，`options` 至少 2 项（每项 `value` 与 `label` 必填非空），`value`（`string`）必须命中某个 `options[].value`，可选 `layout`（`""` | `"stacked"` | `"full"`）：
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
    - `status` / `time`：右侧自动对齐的等宽列；若**全部条目**均传 `"status": "→"`，宿主切换为 `title → time` 双栏映射样式（隐藏 `sub_text` 与前导状态点，保留占比条与 `tooltip`）
    - `tone`：前导状态点支持语义色（`"success"` | `"warning"`/`"warn"` | `"danger"`/`"error"` | `"info"`/`"accent"`）与柔和色板键；`ratio` 占比条仅使用柔和色板键（传 `"success"` 等语义色会回退中性底色）
    - `ratio`：约定传 `0`–`1` 浮点数（`<= 0` 不画条，`> 0` 渲染时夹到 `4%`–`100%` 并隐藏前导状态点）
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
12. **`checklist`（多选勾选列表，推荐用于多笔记本勾选）**：`id` 必填，`label`、`empty_text` 可选，`value` 为已选 `string[]`（省略或 `null` 归一化为 `[]`，不会在解码时过滤旧选项值），`options` 每项含 `value`（必填唯一）、`label`（必填）及可选 `detail` / `badge` / `tooltip`；多于 1 项时宿主自动提供「全选 / 取消全选」。注意 `checklist` 只能放在卡片顶层，不能放入 `group`：
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
14. **`treemap`（磁盘/空间分布图，固定 Bento 布局按字节排序，非严格面积比例）**：`tiles` 为必填非空数组，`id`（同页有多个图时须显式传不同 `id` 以免共享选中状态）、`total_bytes`（非负整数 `uint64`，`0` 或省略时取各瓦片 `bytes` 之和）、`summary` 可选：
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

---

## 5. Python 标准库双向通信骨架

```python
#!/usr/bin/env python3
import json
import sys


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

## 6. 本地协议检查命令

以下命令在临时笔记本与插件数据目录中执行本地协议检查（全程 RPC 共享 20 秒超时）：依次校验 `manifest.json`、目录安装、初始化、初始页渲染、对卡片顶层 `segmented`/`select`/`switch` 逐项单值切换渲染（不做多控件组合，不遍历 `group` 子项或 `checklist`）、调用初始页全部 `header_actions` 与顶层 `actions` 按钮（不判断 `visible_when` 与折叠状态；对返回的 `open_path`/`reveal_path` 仅以 `os.Stat` 验证存在性，`ok: false` 仍可通过）、直接发送 `note.saved` 事件（不走桌面队列）以及 `plugin.shutdown` 回收。

```bash
CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check /绝对路径/到你的插件目录
```

终端输出 `[PASS] Plugin "<id>" passed all Block App Store checks.` 且退出码为 `0` 表示本地协议检查通过。注意：CLI 仅提供单个临时笔记本、不校验桌面默认停用与编辑器脏状态写保护，且未注入 `GetPerfSnapshot`（调用 `host.perf.snapshot` 会收到 `-32005`）；上传、同步、迁移与真实桌面交互仍需在 Block 桌面端使用临时笔记本进一步验证。

---

## 7. 发布到应用商店（`blockmd-plugin` 自动采集）

Block 应用商店采用无审批自动采集机制，发布步骤如下：

1. 将插件代码推送到公开、非 Fork、未归档的 GitHub 仓库，确保仓库根目录（或归档内根/唯一非隐藏顶层目录）包含通过第 6 节检查的 `manifest.json`（含 `"app": "blockmd"`；若填写 `repo` 须与仓库 `owner/repo` 一致），并建议放置 `README.md` 供商店详情页渲染。
2. 在 GitHub 仓库主页右侧 **About -> Topics** 添加标签：**`blockmd-plugin`**。
3. 发布标准 **GitHub Release**（建议保持 Release Tag 与包内 `manifest.version` 一致）：
   - **官方推荐自动版本号**：可复用 `blockmd-plugins/block-sync` 的 `.github/workflows/release.yml`（`Daily Release`）。仓库 `manifest.json` 仅保留主次版本基线（如 `"1.2.0"`），日常改动直接推送 `main`；工作流每天 `00:00 UTC`（或手动 `workflow_dispatch`）检测变更，以 `<major>.<minor>.<提交总数>` 生成候选版本（若不高于已有最高版本则递增）动态写入产物包并发布 Release，不向 `main` 回写提交。
   - **手动发版**：也可推送 `vX.Y.Z` Tag 触发同工作流打包对应版本。
4. **采集与校验规则**：
   - 采集器抓取 `topic:blockmd-plugin fork:false archived:false` 按 stars 降序的前 100 个仓库（无分页）；选包顺序为最新 Release 中 API 返回的首个 `.tar.gz`/`.tgz`/`.zip` 附件 → Release `tarball_url` → `zipball_url` → 默认分支源码包。
   - 采集器下载归档并校验 `manifest.json`（以归档内 `manifest.version` 为准，不校验 Tag，不运行 `block-plugin-check`），先应用 blocklist 与官方认证 ID↔仓库 1:1 绑定校验，再对其余跨仓库重复 `id` 同时剔除；通过后计算 `sha256` 写入 `store-index.json`。
   - **不同安装来源的校验力度**：从应用商店或手动输入商店插件 `id` 安装时，强制 HTTPS（<= 4096 字节、无凭据与 fragment、限制重定向主机）、核对 `sha256` 及包内清单的 `id`/`version`/`permissions` 集合与（非空时）`repo`；手动输入 `owner/repo`、`github:owner/repo`、`https://github.com/owner/repo`（拉取 `HEAD` 源码包）、直接 HTTPS 压缩包 URL 或本地目录路径（支持 `~/`、`file://` 或点击「本地目录」）会校验清单与打包上限，但不做商店 `sha256`/索引比对。
