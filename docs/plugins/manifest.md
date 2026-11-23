<!-- ai-guide:skip-start -->
# 目录结构与 `manifest.json` 规范

> [← 架构与硬边界](README.md) · [文档中心](../README.md) · [协议与 UI →](protocol-and-ui.md)

---
<!-- ai-guide:skip-end -->

## 2. 目录结构与 `manifest.json` 规范

插件根目录至少包含 `manifest.json` 与入口程序（纯声明式语言扩展插件可省略入口程序，改为提供 `grammars/*.json`）；官方插件（`blockmd-plugins/*`）**必须**同时提供 `README.md`（中文）与 `README.en.md`（英文），有子进程界面的插件须完整支持 `locale="en"` 英文界面，社区插件也建议一并提供：

```text
example-sync/
├── manifest.json
├── README.md
├── README.en.md
└── main.py
```

### `manifest.json` 完整示例（请替换为你自己的 `id` 与 `repo`，严禁多写未知字段）

```json
{
  "app": "blockmd",
  "schema_version": 1,
  "id": "example-sync",
  "category": "tools",
  "name": {
    "zh": "示例同步",
    "en": "Example Sync"
  },
  "version": "1.0.0",
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
    "notes:read",
    "notes:write",
    "network",
    "events:note_saved"
  ],
  "permission_reasons": {
    "notes:read": {
      "zh": "扫描已选笔记本中的 Markdown 文件以计算增量差异。",
      "en": "Scan Markdown files in selected notebooks to compute incremental diffs."
    },
    "notes:write": {
      "zh": "将远端更新写回本地笔记本并按策略处理冲突副本。",
      "en": "Write remote updates back to local notebooks and save conflict copies."
    },
    "network": {
      "zh": "连接 WebDAV 服务器或远程 Git 仓库收发笔记变更。",
      "en": "Connect to WebDAV servers or remote Git repositories to transfer changes."
    }
  },
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
- `category`：可选字符串（声明 `contributes.languages` 时**必填 `"language"`**）。分类只看插件作用在哪里，应用商店按以下 5 个分类与顺序展示标签：
  1. `"appearance"`：外观 / Appearance（改的是界面长什么样：主题、字体、图标、界面布局）
  2. `"edit"`：编辑 / Editing（改的是一篇笔记里面：语法扩展、模板、图片、公式、图表）
  3. `"organize"`：整理 / Organize（管的是笔记和笔记之间：标签、互链、搜索、大纲、归档）
  4. `"ai"`：AI / AI（靠模型完成：润色、总结、问答、生成）
  5. `"tools"`：工具 / Tools（干的是笔记之外的事：同步备份、导入导出、发布分享、系统集成、外部服务）
  - 另有一个不显示标签的值：`"language"`（代码高亮 / Code Highlighting，平时不在列表中出现，仅在搜索或已安装时可见）。
  - **怎么选**：按插件最主要的用途，依次问「靠 AI 模型完成？→ `ai`」「改界面长什么样？→ `appearance`」「改一篇笔记里面？→ `edit`」「管笔记和笔记之间？→ `organize`」，第一个答「是」的就是分类，都不是则选 `tools`。例：AI 润色归 `ai`，图床上传归 `edit`，同步归 `tools`。
  - **谁说了算**：插件出现在哪个标签只由本字段决定，商店与宿主不做人工改判；要换分类，改 `category` 后发布新版本，商店索引下一轮采集即生效。
  - 已停用的 `"sync"` 与 `"publish"` 仍可通过校验，但统一归入 `"tools"`；新插件不要再使用。
  - **未填与校验**：未填 `category` 时默认归入 `"tools"`。`block-plugin-check` 对未填或使用停用值的插件输出 `[WARN]`；**官方插件（`blockmd-plugins/*` 或 `verified.json` 认证）必须显式填写当前分类**，否则检查失败且商店采集拒收。商店索引会自动同步 `category` 与 `file_extensions`。
- `name`：`zh` 与 `en` 均必填非空，禁止含 Emoji。
- `version`：必填，须同时满足正则 `^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$` 与标准 SemVer 合法性（`golang.org/x/mod/semver`，与商店更新比较共用同一规则；如 `"1.0.0"`、`"1.0.0-beta.1"`，不支持 `+build` 元数据、禁止前导零与空预发布段如 `2.0.0-..`）。
- `beta`：可选布尔值，默认 `false`，**正式版不要写这个字段**。写了 `"beta": true` 后，应用商店、设置侧栏和插件页标题旁会显示 `Beta` 标识，商店采集器也会同步到 `store-index.json`；转正时删掉该字段即可，宿主不做硬编码。
  - **只有同时满足以下两条才标 `beta`**：
    1. 功能还在试验，交互、设置项或存储格式随时可能大改，升级后可能需要用户重新配置；
    2. 依赖的宿主能力尚未稳定，或作者明确还没完成真机验证。
  - **以下情况不标 `beta`**：功能完整且已通过 `block-plugin-check`；只是新上架、版本号小或用户少；只是使用了新增的扩展点或权限；从宿主内置功能拆出来的插件（如复制路径）；涉及用户数据、需要长期可靠的插件（如同步）——这类插件要么达到正式质量再发布，要么不发布，不能用 `beta` 兜底。
- `author`：必填非空字符串，禁止含 Emoji。
- `repo`：可选，GitHub 仓库标识 `"owner/repo"`（匹配 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`）。上架应用商店时若填写，必须与实际 GitHub 仓库一致（不区分大小写）。
- `summary`：`zh`（<= 80 个 Unicode 字符）与 `en`（<= 140 个 Unicode 字符）均必填，禁止含 Emoji。
- `entry.command`：非空字符串数组（纯声明式语言扩展可省略或为空；若 `category: "language"` 同时声明了 `entry.command` 或 `surfaces`/`permissions`，则按交互式插件生命周期运行，不再走纯声明式自动启用），工作目录固定为插件安装目录；`command[0]` 须为 `PATH` 可执行文件名或插件目录内相对普通文件（如 `"./bin/plugin"`）。Python 脚本必须传 `"-u"`（或每次写 `stdout` 后 `flush=True`）并确保标准流使用 UTF-8。
- `permissions`：可选数组（按需声明），不可重复。权限按是否涉及用户数据或外部系统分为三层：
  - **5 类敏感权限（需用户安装/更新确认，共 12 项）**：仅当插件用到以下能力时才需声明；首次安装或更新新增其中任一项时，宿主会弹出确认卡片请用户点头：
    1. **笔记与编辑**：`"notes:read"`（列举、读取与打开笔记及性能快照）、`"notes:write"`（写入笔记及按版本号移入回收站）、`"notes:path"`（在 `plugin.command` 的 `context.file` 中接收本机绝对路径，不读内容）、`"editor:read"`（读取编辑器选区）、`"editor:write"`（向编辑器插入/替换内容）
    2. **系统凭据**：`"system:secrets"`（读写系统钥匙串/凭据管理器中该插件隔离的凭据）
    3. **剪贴板**：`"system:clipboard_read"`（读取系统剪贴板）、`"system:clipboard_write"`（写入系统剪贴板）
    4. **系统文件与外部操作**：`"system:trash"`（经原生确认框移入回收站）、`"system:open"`（打开网页链接或在文件管理器定位文件）、`"system:dialogs"`（打开系统文件/目录选择框）
    5. **网络访问（插件自述）**：`"network"`（声明插件会发起外部网络或远端命令请求；Block 不在进程层拦截联网，仅向用户如实展示）
    - 系统集成接口细则见[系统集成](protocol-and-ui.md#_3-3-系统集成)。
  - **静默事件订阅（按需声明，不弹窗确认，共 3 项）**：仅作为事件订阅开关，不会触发安装/更新权限弹窗：
    - `"events:note_saved"`：订阅笔记保存事件（`plugin.event`）
    - `"events:system"`：订阅系统唤醒、网络变化与焦点事件（`plugin.event`）
    - `"events:workspace"`：订阅工作区 `note.opened` / `theme.changed` / `locale.changed` 事件（仅投递给已运行进程）
  - **已免声明能力（默认放行，无需写入 `permissions`）**：插件私有存储 `"storage"`、全部界面扩展 `"ui:*"`（`"ui:titlebar"`、`"ui:settings"`、`"ui:commands"`、`"ui:panels"`、`"ui:status"`、`"ui:notify"`、`"ui:sidebar"`、`"ui:appearance"`、`"ui:menubar"`）、系统原生通知 `"system:notify"` 与 Dock 角标 `"system:badge"` 均已默认放行，新插件直接调用或通过 `contributes` 声明即可。旧插件清单保留这些字段仍完全兼容，`block-plugin-check` 会以 `[WARN]` 提醒可删减。
- `permission_reasons`：**可选对象**（`map<Permission, LocalizedText>`，非必填；推荐为敏感权限填写以帮助用户在安装或更新确认时理解用途）：
  - 每个键必须是合法权限枚举，且**必须已声明在 `manifest.permissions` 中**（不可为未申请的权限填写缘由）。
  - 每项须提供 `zh` 或 `en` 至少其一非空文本（启用双语门禁的官方或 `-bilingual` 插件必须同时提供纯英文 `en`），长度限制 `zh <= 80` 个 Unicode 字符、`en <= 140` 个 Unicode 字符，**严禁包含 Emoji**。
  - **展示规则**：在安装/更新权限确认卡片以及应用商店详情页权限胶囊中，仅展示上述 5 类敏感权限；若填写了自定义缘由，优先显示该插件的具体用途，未填写时宿主自动展示内置的双语权限说明。商店采集器会将 `permission_reasons` 同步写入 `store-index.json`。
- `surfaces`：可选数组；需要独立设置页时声明 `["settings_page"]`（其余设置分区、侧边面板、弹窗表面通过 `contributes` 自动派生，详见第 8 节）。语言扩展、主题/外观等无需独立设置页的插件可省略或为空数组 `[]`，安装后不占用侧栏设置页，直接在应用商店详情页管理与卸载。
- `contributes`：可选对象，向宿主顶栏、内置设置标签页、命令面板/快捷键、菜单、侧边面板、状态栏、弹窗或声明式语言语法（`languages`）注入扩展贡献，详见第 8 节与第 9 节。
- `icon`：Ocean Paper 双色矢量图标，`view_box` 固定为 `"0 0 24 24"`，`stroke_paths` 至少含 1 条合法 SVG `d` 路径，`fill_paths` 与 `accent_paths` 可选。

<!-- ai-guide:skip-start -->
---

下一步：[3. 双向 JSON-RPC 2.0、声明式 UI 与 Python 骨架 →](protocol-and-ui.md)
<!-- ai-guide:skip-end -->
