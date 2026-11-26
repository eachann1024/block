# Block 应用商店插件开发指南（AI 自包含规范 v1 · `blockmd`）

> 本文由 `bun run docs:sync` 自动从 [`docs/plugins/`](https://github.com/eachann1024/block/tree/main/docs/plugins) 各章节拼装生成，与 GitHub 仓库文档及在线文档站 [docs.block.cooool.top/plugins](https://docs.block.cooool.top/plugins/) 保持同源同步。

本文是开发、校验与发布 **Block** 应用商店插件的**唯一权威规范**。AI 或开发者无需阅读 Block 宿主源码，仅凭本文即可从零构建可安装、渲染原生 Ocean Paper 设置页、并通过本地协议检查的插件。

---

## 1. 开发前必读

- **上架边界**：外部插件**无需向主仓提 PR 或人工审批**。放在独立公开、非 Fork、未归档 GitHub 仓库，添加 `blockmd-plugin` Topic 并发布正式 Release（非 Draft / Prerelease），即可参与商店自动采集（采集器仅抓取 stars 降序前 100 名仓库；详见 [检查与上架](https://docs.block.cooool.top/plugins/check-and-store) 的「发布到应用商店（`blockmd-plugin` 自动采集）」）。任意阶段均可直接在桌面端通过本地目录、`owner/repo` 或 HTTPS 压缩包安装使用。

### 四类开发路径决策表

| 开发路径 | 适用场景 | 交付结构 | 关键说明 |
| --- | --- | --- | --- |
| 功能 / 社区插件 | 同步、图床、导出、AI 面板、宿主 UI 扩展等交互功能 | 独立仓库 + `manifest.json` + `entry.command`（JSON-RPC 2.0 子进程） | 加 `blockmd-plugin` Topic 发正式 Release 参与自动采集 |
| 纯语法高亮语言扩展 | 为小众语言提供代码块与源码编辑器高亮 | 独立仓库 + `manifest.json`（`category: "language"`）+ `grammars/*.json` | 零子进程、零权限，安装自动启用，详见 [语言扩展](https://docs.block.cooool.top/plugins/languages) |
| `blockmd-plugins/*` 官方认证 | 官方维护且在 `store/verified.json` 1:1 绑定的插件 | 独立仓库 + `manifest.json` + 入口或语法 | 强制要求中英双语 `README` 与 `locale="en"` 英文界面门禁 |
| 主仓 `plugins/` 随包内置（历史特例） | 目前仅 `tag-manager`（`block-tag-manager`）随主应用二进制内置 | 主仓 `plugins/` + `builtin://` 种子注册 | 仅作历史特例保留，**外部插件禁止仿照提 PR 到主仓 `plugins/`** |

### 动手前 4 条架构与校验前提

1. **无 WebView / HTML，纯声明式 JSON UI**：交互式插件通过 `stdin`/`stdout` 行分隔 JSON-RPC 2.0 返回 `UIPage` 由宿主原生渲染（启停开关、顶栏控制与卸载菜单由宿主托管，插件无需自绘；详见下文「原生设置挂载、商店展示与 README 渲染」）。
2. **独立子进程与运行环境门槛**：`entry.command[0]` 须为目标机 `PATH` 解释器（如 `python3`/`node`）或包内相对普通可执行文件（禁符号链接与越界）；宿主仅传白名单环境变量（Windows 不传 `USERPROFILE`/`APPDATA`），读写须用 `data_dir`/`downloads_dir`，避免 `Path.home()`。
3. **两项强校验直接拒载**：静态文案**严禁 Emoji**（图标仅用 `manifest.icon` 双色 SVG）；`manifest.json`（`<= 256 KiB`、禁重复键）与 `UIPage` 启用 `DisallowUnknownFields`，**严禁未定义字段**（精确字段清单见下文「进程通信、环境变量与强制校验细则」）。
4. **按需声明敏感权限与默认停用**：仅按需声明 5 类 12 项敏感权限（见 [清单与权限](https://docs.block.cooool.top/plugins/manifest)）；新装交互式插件默认停用，首次请求或新增敏感权限须经用户弹窗确认后才落盘。

### 5 步最短落地与本地调试

1. 创建独立公开 GitHub 仓库，按目标路径准备结构（交互式插件含 `manifest.json` 与入口程序，纯语言扩展含 `manifest.json` 与 `grammars/*.json`）。
2. 按 [清单与权限](https://docs.block.cooool.top/plugins/manifest) 的「字段约束」编写 `manifest.json`（必填 `app: "blockmd"`、显式 `category`、按需权限、`icon` 与可选 `permission_reasons`）。
3. 按 [协议、UI 与骨架](https://docs.block.cooool.top/plugins/protocol-and-ui) 实现双向 JSON-RPC 2.0 通信与 `UIPage`；如需宿主扩展点或自定义语法，分别按 [宿主界面扩展](https://docs.block.cooool.top/plugins/ui-contributions) 与 [语言扩展](https://docs.block.cooool.top/plugins/languages) 配置 `contributes`。
4. 运行本地协议检查，并在 Block 桌面端通过「设置 → 应用商店 → 本地目录」实测（详细门禁见 [检查与上架](https://docs.block.cooool.top/plugins/check-and-store)）：
   ```bash
   CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check /绝对路径/到你的插件目录
   ```
5. 推送代码到公开仓库，添加 `blockmd-plugin` Topic 并发布正式 GitHub Release，参与商店自动采集（或直接分享仓库名 / 压缩包链接安装）。

---

## 2. 架构与宿主硬边界

### 2.1 进程通信、环境变量与强制校验细则

- **跨语言双向 JSON-RPC 2.0**：宿主以独立子进程启动 `manifest.json` 中的 `entry.command`（推荐 `["python3", "-u", "main.py"]`、`["node", "main.mjs"]` 或编译后可执行文件），通过 UTF-8 **按行分隔（`\n`）的 JSON-RPC 2.0** 在 `stdin` / `stdout` 双向通信，`stderr` 仅用于调试（宿主用 64 KiB 环形缓冲区保留尾部）。切换桌面笔记本或界面语言（`zh` / `en`）不会立即重启全部后台进程：仅在下次调用该插件且目标笔记本或语言发生变化时，宿主才关闭旧进程并按新上下文重新初始化（当前打开的活跃表面会随切换触发重渲染，从而立即按新上下文重启）。
- **入口命令校验**：`entry.command[0]` 必须是 `PATH` 可执行文件名（走 `LookPath`），或插件目录内解析后的相对普通文件（如 `"./bin/plugin"`，禁止符号链接、冒号或越出插件目录）。
- **环境变量白名单**：宿主仅显式传入 `PATH`、`HOME`、`TMPDIR`、`PYTHONUNBUFFERED=1`、`BLOCK_PLUGIN_ID`、`BLOCK_DATA_DIR`、`BLOCKMD_PLUGIN_ID`、`BLOCKMD_DATA_DIR`、`BLOCKMD_DOWNLOADS_DIR`，不继承其他宿主环境变量。Windows 上不传入 `USERPROFILE` / `APPDATA` 等目录变量，插件应直接使用 `plugin.initialize` 传入的 `data_dir` / `downloads_dir` 或上述环境变量，避免依赖 `Path.home()`。
- **两项强制校验规则（违规直接拒绝加载）**：
  1. **禁止 Emoji**：`manifest.json` 的 `name`、`author`、`summary`、`permission_reasons`、`contributes` 静态文案，以及 `UIPage` 的静态界面文案（`page.title`、`card.title`、控件 `label`、按钮 `label`、`placeholder`、`empty_text`、`kv.rows[].label`、`metrics[].label`、`checklist.options[].badge`、`treemap` 瓦片 `label`/`detail` 与 `summary.total_label`、`preview.badge`/`cards[].badge`）**严禁包含 Emoji**（覆盖标准与扩展 Emoji 区块、变体选择符 `U+FE00–U+FE0F`、区域指示符/标签序列、ZWJ/键帽组合及常见 Emoji 符号如 `⭐`/`⌚`/`‼` 等），图标仅通过 `manifest.icon` 双色 SVG 路径定义。用户笔记内容、路径等动态字段（如 `page.status`、`card.meta`、`options[].label`/`detail`、`kv.rows[].value`、`metrics[].value`、`preview` 标题与正文、`log_list.entries`）放行用户原有字符，但插件自写文案仍应避免 Emoji。
  2. **严格 JSON 解码**：`manifest.json`（上限 256 KiB，须为单个 JSON 对象，且禁止重复键）与 `UIPage` 启用 `DisallowUnknownFields`，**严禁出现未定义字段名**（外层 JSON-RPC 包与 `host.*` 参数使用普通解码；`UIItem` 为共享结构，已知字段仅在消费它的 `kind` 上生效）。

### 2.2 超时与体积上限

- **超时与进程终止**：
  - 首载 15s 和编辑预览 8s 在取得插件 RPC 锁后起算。定时轮询 5s 包含锁等待。各自的预算由初始化和渲染共享。
  - `plugin.command` 单次 30s（含锁等待与按需初始化）；`events:workspace`（`note.opened` / `theme.changed` / `locale.changed`）单次 5s（仅投递给已运行进程，不冷启动）。
  - `plugin.action` 120s 含可取消的排队等待与表单补刷（顶栏运行胶囊提供 `■` 取消按钮）。
  - 保存事件取得插件 RPC 锁后有 45s，由初始化、事件与后续刷新共享。系统事件取得锁后有 15s 初始化预算，事件本身有 45s。`block-sync` 的唤醒、网络补同步和定时同步事件有 120s。宿主每分钟向 `block-sync` 发送一次定时同步事件，由插件按设置的间隔决定是否同步。
  - 宿主回写 `host.*` 响应的 5s 超时在宿主方法执行完成后起算。
  - `plugin.shutdown` 给 2s 优雅退出窗口，超时强杀后再最多等 1s。macOS/Linux 在独立进程组（`Setpgid`）运行并在超时/强杀时终止整组，其他平台终止直接子进程。
  - 本地校验器 `block-plugin-check` 为 `plugin.initialize` 单独执行与桌面一致的 15s 预算，单语言整轮总超时 20s。
- **体积与打包上限**：
  - 插件发往宿主（`stdout`）的单行 JSON-RPC 扫描上限 **98 MiB**（覆盖 16 MiB 笔记在最坏 6× JSON 转义下的体积与 RPC 信封；宿主发往插件 `stdin` 无此扫描上限）。
  - 同时在途 `host.*` 调用上限 **8** 个（超出将中止插件进程）。
  - 私有存储 `state.json` 序列化上限 **8 MiB**。
  - 笔记读取与 `host.notes.write` 单文件上限 **16 MiB**；`host.editor.insert` 单次插入文本上限 **4 MiB**。
  - 安装归档下载 <= **64 MiB**、单文件 <= **32 MiB**、解包或本地目录复制总计 <= **256 MiB**（tar 解压流 <= **320 MiB**）、路径/条目数 <= **8192**（含推导父目录）、单路径 <= 1024 字节且 `< 32` 级目录、禁止 `\` 与 `:`、仅允许普通文件与目录（不支持符号链接）。

### 2.3 原生设置挂载、商店展示与 README 渲染

- **侧栏与顶栏控制**：声明了 `settings_page` 的已安装插件统一收拢在「设置」左侧导航栏「已安装」分组下（位于「应用商店」之下），带运行状态指示点；未声明独立设置页的插件（如语言扩展、主题类）不占用侧栏设置页，点击应用商店整张卡片进入详情页即可直接卸载。插件设置页头部与设置窗口标题栏统一采用 Ocean Paper 拨弦开关（Pluck Switch / `PluckDivider`）作为主控开关，并自动提供单行合并状态位、主操作位/实时进度胶囊与「⋯」更多菜单（含次要操作、查看说明/应用商店、重新加载与卸载），插件无需自行绘制启用开关或卸载按钮。
- **分类、筛选与搜索**：应用商店固定提供 5 个分类标签（外观 / 编辑 / 整理 / AI / 工具），代码高亮扩展仅在搜索或已安装时可见，支持按安装状态（全部 / 已安装 / 未安装）与来源筛选，支持中英文名称与摘要联合模糊搜索并高亮命中字符；目录拉取在主源不可达时自动回退 jsDelivr CDN。
- **三级信任标识**：应用商店按来源区分三类状态——`Block 官方`（`blockmd-plugins/*` 且在 `verified.json` 1:1 绑定，展示官方徽章）、`已审核`（`eachann1024/*` 信任作者源）、`未审核`（其余社区仓库，展示审阅风险提示）。
- **权限胶囊悬停说明**：商店详情页在元信息行下方以胶囊形式展示该插件申请的 5 类敏感权限（免声明能力与静默事件订阅不展示），鼠标悬停每个权限胶囊时弹出结构化信息卡片：若插件在 `manifest.permission_reasons` 中填写了自定义缘由，优先展示该插件的具体用途并附带系统权限范围说明；未填写时自动展示系统内置的双语权限说明。
- **详情页 README 渲染**：点击卡片在商店内以无嵌套滚动的只读编辑器直排渲染插件说明文档（中文优先，正文上限 512 KiB UTF-8），支持 GitHub 风格 `<picture>` 深浅色自适应图：
  - 本地已安装包按语言查根目录普通文件（不区分大小写）：中文依次查 `README.zh.md` → `README.md` → `README.markdown` → `README.txt` → `README`；英文依次查 `README.en.md` → `README.en.markdown` → `README.en.txt` → `README.md` → `README.markdown` → `README.txt` → `README`。
  - 本地缺失、为空或读取失败且存在有效仓库时优先回退 GitHub API（英文先查 `contents/README.en.md`，404 时再回退 `/readme`；中文直接查 `/readme`），当 GitHub API 不可达或限流时自动回退 jsDelivr CDN（`README.en.md` / `README.md`）；切换界面语言或完成安装/更新时自动刷新详情页说明。
  - 详情页支持相对路径图片、SVG、GIF（超动画预算可降为静态）与受限视频（最多 24 项媒体，其中视频最多 2 项，单张图片 <= 6 MiB，单个视频 <= 24 MiB）。

### 2.4 安装路径、安全替换、深度链接与保留 ID

- **独立进程与存储路径**：只安装信任的插件。子进程使用 Block 的系统权限，清单 `permissions` 仅控制宿主 RPC。
  - 安装路径：`<BlockAppData>/apps/<plugin-id>/`
  - 私有存储：`<BlockAppData>/apps-data/<plugin-id>/state.json`（停用状态与目录缓存位于 `<BlockAppData>/apps-cache/`）
- **生命周期与安全替换**：新安装的交互式插件默认停用，用户手动启用后才会启动；纯声明式语言扩展首次安装自动启用；同 `id` 升级保留原有启用状态。商店卡片的「更新」按钮仅在目录版本高于已装版本时出现；手动安装同 `id` 不要求版本递增。
  - 首次安装请求敏感权限（笔记与编辑、系统凭据、剪贴板、系统文件与外部操作、网络访问）的插件，或更新时新增敏感权限，宿主会先弹出权限确认卡片，用户点击确认后才落盘；「全部更新」会先更新无新增敏感权限的插件，将新增敏感权限的更新留待用户逐项确认。宿主校验候选包与现有包清单、停止旧进程，再通过两步目录重命名配合 `.previous` 备份完成可恢复替换；非交互接口默认拒绝跨来源、旧包无来源记录或新增权限的替换。桌面卸载移除插件程序，并保留私有配置供重新安装。
- **外部深度链接（`block://`、`blockmd://` 的 `store` / `install`）**：按 `repo` → `url` → `id` 顺序取**第一个非空**查询参数，仅用该单一值匹配当前目录中的 `id`、`repo` 或精确 `download_url`（未命中时不会回退尝试后续参数，直接停留在商店列表），不自动安装。
- **保留 ID**：`block-diagnostics` 对应 Block 内置「性能与占用」窗口，宿主拒绝安装与启用该 ID、设置侧栏不挂载，远程采集器、缓存合并与离线内置索引回退均会过滤该 ID。

---

## 3. 目录结构与 `manifest.json` 规范

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
    "zh": "把笔记同步到网盘或 Git 仓库，换设备也能接着看。",
    "en": "Sync your notes to WebDAV or Git so they stay up to date across devices."
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
      "zh": "查看已选笔记本里哪些笔记有改动，准备同步。",
      "en": "Check which notes in selected notebooks have changed for syncing."
    },
    "notes:write": {
      "zh": "把其他设备同步过来的新内容存进本地笔记本。",
      "en": "Save updated notes synced from other devices into your local notebooks."
    },
    "network": {
      "zh": "连接你设置的网盘或 Git 仓库，上传和拉取笔记。",
      "en": "Connect to your WebDAV server or Git repository to send and fetch notes."
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

#### 必填与核心架构字段
- `app`：必填固定字符串 **`"blockmd"`**（用于插件生态识别与自动采集验真）。
- `schema_version`：必填固定整数 `1`。
- `id`：必填，匹配正则 `^[a-z0-9][a-z0-9-]{1,47}$`（不可使用保留 ID `block-diagnostics`）。
- `version`：必填，须同时满足正则 `^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$` 与标准 SemVer 合法性（`golang.org/x/mod/semver`，与商店更新比较共用同一规则；如 `"1.0.0"`、`"1.0.0-beta.1"`，不支持 `+build` 元数据、禁止前导零与空预发布段如 `2.0.0-..`）。
- `name`：`zh` 与 `en` 均必填非空，禁止含 Emoji。
- `author`：必填非空字符串，禁止含 Emoji。
- `summary`：`zh`（<= 80 个 Unicode 字符）与 `en`（<= 140 个 Unicode 字符）均必填，禁止含 Emoji。
  - **插件介绍写作原则**（适用于 `summary`、`permission_reasons` 与 `README`）：面向普通用户说大白话，聚焦能解决什么问题，可类比常见应用（如 Obsidian、Bear）。
  - **禁止写入**内部实现细节（依赖版本、接口名、文件格式细节）与防御性免责声明。
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
- `entry.command`：非空字符串数组（纯声明式语言扩展可省略或为空；若 `category: "language"` 同时声明了 `entry.command` 或 `surfaces`/`permissions`，则按交互式插件生命周期运行，不再走纯声明式自动启用），工作目录固定为插件安装目录；`command[0]` 须为 `PATH` 可执行文件名或插件目录内相对普通文件（如 `"./bin/plugin"`）。Python 脚本必须传 `"-u"`（或每次写 `stdout` 后 `flush=True`）并确保标准流使用 UTF-8。
- `surfaces`：可选数组；需要独立设置页时声明 `["settings_page"]`（其余设置分区、侧边面板、弹窗表面通过 `contributes` 自动派生，详见 [宿主界面扩展](https://docs.block.cooool.top/plugins/ui-contributions) 的「多表面（Surfaces）渲染与状态隔离」）。语言扩展、主题/外观等无需独立设置页的插件可省略或为空数组 `[]`，安装后不占用侧栏设置页，直接在应用商店详情页管理与卸载。
- `permissions`：可选数组（按需声明），不可重复。有效权限共 **27 项**（5 类 12 项敏感权限 + 3 项静默事件 + 12 项默认放行），按是否涉及用户数据或外部系统分为三层：
  - **5 类敏感权限（需用户安装/更新确认，共 12 项）**：仅当插件用到以下能力时才需声明；首次安装或更新新增其中任一项时，宿主会弹出确认卡片请用户点头：
    1. **笔记与编辑**：`"notes:read"`（列举、读取与打开笔记及性能快照）、`"notes:write"`（写入笔记及按版本号移入回收站）、`"notes:path"`（在 `plugin.command` 的 `context.file` 中接收本机绝对路径，不读内容）、`"editor:read"`（读取编辑器选区）、`"editor:write"`（向编辑器插入/替换内容）
    2. **系统凭据**：`"system:secrets"`（读写系统钥匙串/凭据管理器中该插件隔离的凭据）
    3. **剪贴板**：`"system:clipboard_read"`（读取系统剪贴板）、`"system:clipboard_write"`（写入系统剪贴板）
    4. **系统文件与外部操作**：`"system:trash"`（经原生确认框移入回收站）、`"system:open"`（打开网页链接或在文件管理器定位文件）、`"system:dialogs"`（打开系统文件/目录选择框）
    5. **网络访问（插件自述）**：`"network"`（声明插件会发起外部网络或远端命令请求；Block 不在进程层拦截联网，仅向用户如实展示）
    - 系统集成接口细则见 [协议、UI 与骨架](https://docs.block.cooool.top/plugins/protocol-and-ui) 的「系统集成」小节。
  - **静默事件订阅（按需声明，不弹窗确认，共 3 项）**：仅作为事件订阅开关，不会触发安装/更新权限弹窗：
    - `"events:note_saved"`：订阅笔记保存事件（`plugin.event`）
    - `"events:system"`：订阅系统唤醒、网络变化与焦点事件（`plugin.event`）
    - `"events:workspace"`：订阅工作区 `note.opened` / `theme.changed` / `locale.changed` 事件（仅投递给已运行进程）
  - **已免声明能力（默认放行，无需写入 `permissions`，共 12 项）**：插件私有存储 `"storage"`、全部 9 项界面扩展 `"ui:*"`（`"ui:titlebar"`、`"ui:settings"`、`"ui:commands"`、`"ui:panels"`、`"ui:status"`、`"ui:notify"`、`"ui:sidebar"`、`"ui:appearance"`、`"ui:menubar"`）、系统原生通知 `"system:notify"` 与 Dock 角标 `"system:badge"` 均已默认放行，新插件直接调用或通过 `contributes` 声明即可。旧插件清单保留这些字段仍完全兼容，`block-plugin-check` 会以 `[WARN]` 提醒可删减。
- `contributes`：可选对象，向宿主顶栏、内置设置标签页、命令面板/快捷键、菜单、侧边面板、状态栏、弹窗或声明式语言语法（`languages`）注入扩展贡献，详见 [宿主界面扩展](https://docs.block.cooool.top/plugins/ui-contributions) 与 [语言扩展](https://docs.block.cooool.top/plugins/languages)。
- `icon`：Ocean Paper 双色矢量图标，`view_box` 固定为 `"0 0 24 24"`，`stroke_paths` 至少含 1 条合法 SVG `d` 路径，`fill_paths` 与 `accent_paths` 可选。

#### 低频可选字段
- `repo`：可选，GitHub 仓库标识 `"owner/repo"`（匹配 `^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`）。上架应用商店时若填写，必须与实际 GitHub 仓库一致（不区分大小写）。
- `permission_reasons`：**可选对象**（`map<Permission, LocalizedText>`，非必填；推荐为敏感权限填写以帮助用户在安装或更新确认时理解用途）：
  - 每个键必须是合法权限枚举，且**必须已声明在 `manifest.permissions` 中**（不可为未申请的权限填写缘由）。
  - 每项须提供 `zh` 或 `en` 至少其一非空文本（启用双语门禁的官方或 `-bilingual` 插件必须同时提供纯英文 `en`），长度限制 `zh <= 80` 个 Unicode 字符、`en <= 140` 个 Unicode 字符，**严禁包含 Emoji**。
  - **展示规则**：在安装/更新权限确认卡片以及应用商店详情页权限胶囊中，仅展示上述 5 类敏感权限；若填写了自定义缘由，优先显示该插件的具体用途，未填写时宿主自动展示内置的双语权限说明。商店采集器会将 `permission_reasons` 同步写入 `store-index.json`。
- `beta`：可选布尔值，默认 `false`，**正式版不要写这个字段**。写了 `"beta": true` 后，应用商店、设置侧栏和插件页标题旁会显示 `Beta` 标识，商店采集器也会同步到 `store-index.json`；转正时删掉该字段即可，宿主不做硬编码。
  - **只有同时满足以下两条才标 `beta`**：
    1. 功能还在试验，交互、设置项或存储格式随时可能大改，升级后可能需要用户重新配置；
    2. 依赖的宿主能力尚未稳定，或作者明确还没完成真机验证。
  - **以下情况不标 `beta`**：功能完整且已通过 `block-plugin-check`；只是新上架、版本号小或用户少；只是使用了新增的扩展点或权限；从宿主内置功能拆出来的插件（如复制路径）；涉及用户数据、需要长期可靠的插件（如同步）——这类插件要么达到正式质量再发布，要么不发布，不能用 `beta` 兜底。

---

## 4. 双向 JSON-RPC 2.0 通信协议

所有请求与响应均为单行 UTF-8 JSON（末尾 `\n`）。

### 4.1 宿主调用插件（Host -> Plugin）

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
         "kv_tooltip", "treemap", "checklist_add_folder", "action_feedback",
         "action_progress", "note_revision", "sidebar_sections", "open_settings"
       ]
     }
     ```
     - **运行时上下文**：`notebook_root` 仅在声明 `notes:read`、`notes:write` 或 `events:note_saved` 任一权限且目标笔记本非空时传入，否则为 `""`；`notebooks` 仅在声明 `notes:read` 时填充，否则为 `[]`。切换桌面笔记本不会立刻重启全部插件，仅在下次调用该插件且目标根目录与当前运行时上下文不同（含后台 `note.saved` 事件）时，宿主才关闭旧进程并按新 `notebook_root` 重新初始化。
     - `downloads_dir`（默认 `~/Downloads`，环境变量 `BLOCKMD_DOWNLOADS_DIR`）：**凡涉及导出文件、备份归档或清理转移的目录，一律默认输出到 `downloads_dir`**（单文件直接输出到 `downloads_dir/<文件名>`，多文件收拢到一个干净文件夹内，严禁默认塞进 `apps-data` 隐藏目录或在笔记本内新建 `.trash-*` 等隐藏目录）。
     - `ui_capabilities`：渐进能力声明数组，旧宿主可能省略部分键；全部 34 项能力键说明见下文「兼容性与平台实现说明」。
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
   - 返回 `result`（字段均可选；`page` 若返回须为完整合法 `UIPage` 对象，见下文「声明式 UI Schema（`UIPage`）精确定义」）：
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

4. **`plugin.event`**（笔记保存/删除事件需声明 `"events:note_saved"`，系统事件见下文「系统集成」）
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

### 4.2 插件调用宿主能力（Plugin -> Host）

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

### 4.3 系统集成

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

`events:system` 通过 `plugin.event` 接收 `system.wake`、`network.changed`、`app.focus`。事件包含 `event` 和 UTC `timestamp`，唤醒和网络变化包含系统报告的 `online` 布尔值。需要笔记权限或保存事件权限才能收到 `notebook_root`。宿主可启动已启用的订阅插件，并按插件串行处理，排队中的同类事件只保留最新一条。`network.changed` 涵盖系统报告的状态变化和检测到的网卡地址变化。服务器可达性由插件自行判断。（macOS / Windows / Linux 平台实现及安全回收细节见下文「兼容性与平台实现说明」。）

### 4.4 兼容性与平台实现说明

- **全部 `ui_capabilities` 能力键**（旧宿主可能省略，用于渐进启用特性）：
  `kv_tooltip`（`kv` 行提示）、`treemap`（空间分布图控件）、`host_perf`（`host.perf.snapshot`；注意本地 CLI 校验器尚未注入性能回调，调用会返回 `-32005`，插件应做降级处理）、`checklist`（多选勾选列表）、`checklist_add_folder`（`checklist.add_folder` 加号添加文件夹）、`form_layout`（`layout` 与 `group`）、`multi_notebook`（多笔记本列表与读写）、`card_collapse`（卡片折叠）、`metric_tone`（`metrics[].tone` 柔和色板）、`log_columns`（`log_list` 对齐列、`sub_text`/`ratio`/`tone`/`tooltip` 与 `→` 映射模式）、`file_picker`（`folder.accept_extensions`）、`action_feedback`（`plugin.action` 的 `ok`/`tone` 与内联/按钮反馈）、`action_progress`（`host.action.progress`）、`note_revision`（`revision`/`expected_revision` 乐观并发）、`sidebar_sections`（左侧栏插件分区）、`file_badges`（文件树行尾角标）、`sidebar_menu`（左侧栏文件树右键菜单）、`backdrops`（编辑区自定义矢量背景）、`status_icon`（底部状态栏图标）、`toast_icon`（Toast 图标）、`notes_open`（`host.notes.open` 跳转打开笔记）、`log_actions`（`log_list` 条目点击触发 `action_id`）、`panel_toggle`（`host.ui.toggle_panel` 原子切换侧栏面板）、`editor_insert_mode`（`host.editor.insert` 支持 `"mode": "insert"|"replace"` 非破坏插入）、`dialog_token`（`host.ui.open_dialog` 返回宿主确认句柄 `dialog_token`）、`item_icons`（`switch` / `kv` 行前置图标 `icon` 与 `icon_strip` 图标条）、`tab_menu`（标签页右键菜单 `contributes.tab_menu` 与 `sidebar_menu.targets` 文件夹行）、`file_path`（`plugin.command` 的 `context.file` / `context.folder`，`file` 需 `"notes:path"`）、`log_timeline`（`log_list` 的 `"variant": "timeline"` 垂直时间轴模式，支持折叠静默区间 `kind: "span"`、`badge`/`span`/`details` 与聚合标签 `tags`）、`progress_bar`（`progress` 内联进度条控件及 `host.action.progress` 的独立通道 `channel` / `tone` / `running`）、`segmented_status`（`segmented` 选项的状态圆点 `tone`、悬停提示 `tooltip` 与通道实时状态绑定 `channel`）、`toast_banner`（右上角全局通知横幅支持 `body`/`action`、`shown` 返回值与默认插件图标）、`open_settings`（`host.ui.open_settings` 打开插件设置页）、`store_changelog`（`host.store.changelog` 查询应用商店插件更新日志接口）、`menubar`（系统菜单栏图标 `contributes.menubar_items` 与 `host.menubar.set`）、`menubar_drop`（菜单栏图标接收拖放，目前仅 macOS）。
- **各平台系统集成细节**：
  macOS 使用通知中心、钥匙串和 Dock。macOS 通知的点击目标可跨 Block 重启保留七天，点击时重新检查安装状态和权限。Windows 使用系统通知、凭据管理器和任务栏叠加角标，通知点击依赖系统横幅仍然有效。Linux 使用桌面通知服务、Secret Service 和兼容 Unity Launcher 协议的 Dock，唤醒事件依赖 logind。Linux 需要通知服务支持 `actions` 才能点击通知，`notify_click:false` 时仍可能显示通知。Linux 系统凭据需在系统凭据管理器中先解锁。单次运行最多保留最近 128 条插件原生通知。系统选择框已打开时，原生后端可能需要用户关闭窗口，宿主会丢弃已过期动作的选择结果。缺少相应服务时，插件应根据能力查询或调用错误调整界面和行为。
- **安全回收实现机制**：
  回收确认前，宿主先保存正在编辑的内容。确认后，宿主记录恢复位置，将文件移到同卷的暂存目录并复核文件身份。Block 数据目录与原文件分处不同卷时，暂存目录建在原文件旁。Windows 还需要文件所在卷支持硬链接。条件不满足时保留原文件。回收失败时优先恢复原路径，原路径已被占用时保留暂存文件。系统「还原」会将文件放回暂存目录。Block 会保留原路径记录，启动时提示可恢复文件及其原路径。凭据权限约束宿主 RPC，独立插件进程仍以当前系统用户运行，其直接访问由操作系统控制。

---

## 5. 声明式 UI Schema（`UIPage`）精确定义

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

## 6. Python 标准库双向通信骨架

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

---

## 7. UI contributions（宿主界面扩展贡献）

插件可通过 `manifest.json` 的 `contributes` 字段在宿主设置页之外挂载顶栏按钮、内置设置标签页分区、命令/快捷键、页面/编辑器/侧栏/标签页右键菜单、左侧栏分区、文件树行角标、文件树自定义图标包、编辑区自定义矢量背景、右侧边栏面板、状态栏条目、弹窗与系统菜单栏图标。权限与免声明能力详见 [清单与权限](https://docs.block.cooool.top/plugins/manifest) 的「字段约束」：所有 `ui:*` 界面扩展均已默认放行，仅编辑器读写（`editor:read`、`editor:write`）、文件路径（`notes:path`）与工作区事件订阅（`events:workspace`）需要按需声明。

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
- 数量上限：`titlebar_buttons` 3、`settings_sections` 8、`commands` 64、`page_menu` 16、`editor_menu` 16、`sidebar_menu` 16、`tab_menu` 16、`panels` 8、`sidebar_sections` 4、`file_badges` 4、`file_icons` 1、`backdrops` 8、`status_items` 4、`dialogs` 16、`menubar_items` 2。

1. **`titlebar_buttons`**（最多 **3** 个）：在顶栏右侧挂载图标按钮。顶栏有空位时所有插件按钮都直接显示，仅在空间不足时把放不下的按钮收起；用户可在「更多插件」菜单（点星标或在选中/悬停行按 `P`）或右键/聚焦顶栏插件图标（按 `P` / `Shift+F10`）固定常用按钮（最多固定 3 个，优先展示；槽位满时优先回收已停用/卸载项，有效项满 3 个时替换最早固定项并提示），其余按声明顺序收进顶栏的「更多插件」菜单（带计数角标、插件图标与活跃/角标提示）。菜单行文字取 `tooltip`（缺省为 `id`），因此务必填写可读的 `tooltip`。
   - `id`（必填）、`icon`（必填 `DuoIconSpec`）、`tooltip`（必填 `LocalizedText`）。
   - `command`（可选）与 `panel`（可选）：**必须且只能二选一**，分别引用同清单 `commands[].id` 或 `panels[].id`（点击时执行命令或切换面板）。
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
10. **`backdrops`**（最多 **8** 个）：声明编辑区自定义矢量背景图案，运行时通过 `host.ui.set_backdrop` 启用、切换或调节（宿主自动钳制透明度在 `0.03`–`0.25` 以保证正文清晰可读，停用或卸载插件立即消失）。
    - `id`（必填）、`title`（必填 `LocalizedText`）、`icon`（必填 `DuoIconSpec`）。
    - `pattern`（可选：`"scatter"` | `"grid"` | `"watermark"` | `"corners"`，默认 `"scatter"`）、`density`（可选：`"sparse"` | `"medium"` | `"dense"`，默认 `"medium"`）、`opacity`（可选：`0.01`–`0.35`，渲染钳制 `0.03`–`0.25`，默认 `0.09`）、`tint`（可选：`"accent"` | `"muted"` | `"warm"` | `"glow"`，默认 `"accent"`）、`dark_glow`（可选 bool，深色主题下提亮为夜光质感）。
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
2. **`host.titlebar.set_hidden`**（无需声明权限）
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
   - 返回：`open_dialog` 在宿主成功打开后返回 `{"ok": true, "dialog_token": "dlg-..."}`（无宿主 UI 时返回 `-32005`），且用户在对应 `dialog:*` 表面触发 `plugin.action` 时宿主会随参数附带当前活动弹窗的 `"dialog_token"`，插件可严格核对来源表面与一次性令牌；`close_dialog` 返回 `{"ok": true}`。
7. **`host.ui.open_panel` / `host.ui.close_panel` / `host.ui.toggle_panel`**（无需声明权限）
   - 参数：`{"id": "<panel_id>"}`（`id` 须为本插件声明的 `panels[].id`）。
   - 返回：`open_panel` / `close_panel` 返回 `{"ok": true}`；`toggle_panel` 返回 `{"ok": true, "open": true|false}`（由宿主原子切换并回传当前开关态，避免插件自维护状态与宿主关闭按钮失步）。
8. **`host.ui.refresh`**（无需声明权限，仅要求目标表面属于 `"settings_page"` 或本插件 `contributes` 声明的表面）
   - 参数：`{"surface": "panel:outline"}`（异步触发对应表面的后台 `plugin.render` 重绘；用户在当前笔记本中尚未打开过的表面不会渲染，首次打开时自然加载最新内容）。
   - 返回：`{"ok": true}`。
9. **`host.sidebar.set_badges` / `host.sidebar.get_badges`**（无需声明权限）
   - `host.sidebar.set_badges` 参数：`{"badge_id": "poop-mark", "notebook_root": "/abs/path/to/notebook", "clear": false, "patch": false, "entries": {"welcome.md": {"text": "2", "tooltip": "含 2 处标记", "tone": "accent"}}}`（`notebook_root` 可选，省略时取当前笔记本；`entries` 最多 512 项，相对路径不可包含 `..` 或 `:`，`text` 禁 Emoji 且 <= 4 字符，`tooltip` 禁 Emoji 且 <= 120 字符，`tone` 支持 `"accent"` | `"success"` | `"warning"` | `"danger"`；`patch: true` 时传 `null` 可删除单项，`clear: true` 一键清空；按插件与笔记本持久化于 `contrib-state.json`）。
   - `host.sidebar.get_badges` 参数：`{"badge_id": "poop-mark", "notebook_root": "..."}` -> 返回 `{"entries": {...}}`。
9.1. **`host.sidebar.set_file_icons` / `host.sidebar.get_file_icons`**（无需声明权限，需声明 `contributes.file_icons`）
   - `host.sidebar.set_file_icons` 参数：`{"notebook_root": "/abs/path/to/notebook", "clear": false, "patch": false, "entries": {".": {"icon": "book", "tint": "blue"}, "welcome.md": {"icon": "wave", "tint": "blue"}}, "prefs": {"picker": true, "sync_tabs": true}}`（`notebook_root` 省略时取当前笔记本；`"."` 表示笔记本自身图标，其余为笔记本内相对路径；`entries` 最多 2048 项，`icon` 须存在于图标包，`tint` 支持 `"none"` | `"blue"` | `"teal"` | `"green"` | `"amber"` | `"red"` | `"purple"`；`patch: true` 时传 `null` 删除单项，`clear: true` 清空当前笔记本映射；`prefs` 覆盖选择器与标签页同步开关；文件重命名/移入回收站时宿主自动同步路径）。
   - `host.sidebar.get_file_icons` 参数：`{"notebook_root": "..."}` -> 返回 `{"notebook_root": "...", "entries": {...}, "recent": [...], "prefs": {"picker": true, "sync_tabs": true}}`。
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

### 7.5 特定场景与官方专属宿主方法

以下接口服务于编辑区背景装饰、原生思维导图与原生塔罗模式等特定视觉能力（无需声明权限，按实际场景按需调用）：

1. **`host.ui.backdrop` / `host.ui.set_backdrop`**（编辑区矢量背景，适用于壁纸/背景装饰类插件如 `block-poop-everywhere`）
   - `host.ui.backdrop`（参数 `{}`）：返回本插件当前持久化的编辑区矢量背景状态 `{"id": "...", "enabled": true, "pattern": "scatter", "density": "medium", "opacity": 0.09, "tint": "accent", "dark_glow": false}`。
   - `host.ui.set_backdrop`（参数：`{"id": "poop-wallpaper", "enabled": true, "pattern": "scatter", "density": "medium", "opacity": 0.10, "tint": "accent", "dark_glow": false}`，未传的样式字段保留同 `id` 已有值或清单默认值）：实时更新编辑区矢量背景并持久化于 `contrib-state.json`，停用插件时自动失效，返回归一化后的状态对象。
   - 「到处拉屎」插件在 `plugin.initialize` 读取已保存的背景设置并调用 `host.ui.set_backdrop`，外观设置中的控件变化通过 `plugin.render` 的 `values` 即时保存和应用。
2. **`host.ui.editor_background` / `host.ui.set_editor_background`**（编辑器点阵/方格背景，适用于 `block-canvas-dots` 等外观扩展）
   - `host.ui.editor_background`（参数 `{}`）：返回当前插件设置的编辑器背景状态 `{"mode": "off"|"dots"|"grid", "contrast": "standard"|"high"}`。
   - `host.ui.set_editor_background`（参数 `{"mode": "dots", "contrast": "standard"}`）：设置与编辑器行高对齐的点阵（`"dots"`）、方格（`"grid"`）或关闭（`"off"`）背景及对比度（`"standard"` | `"high"`，兼容 `"strong"` 归一化为 `"high"`），持久化于 `contrib-state.json` 并按插件隔离；停用插件时自动失效，返回归一化后的状态对象。
3. **`host.ui.mindmap_state` / `host.ui.set_mindmap`**（原生思维导图配置，适用于 `block-mindmap` 官方扩展）
   - `host.ui.mindmap_state`（参数 `{}`）：返回当前插件设置的原生思维导图配置 `{"layout": "right"|"both", "height": "compact"|"standard"|"tall", "show_hints": true}`。
   - `host.ui.set_mindmap`（参数 `{"layout": "right"|"both", "height": "compact"|"standard"|"tall", "show_hints": true}`，字段均可选）：更新原生思维导图块（```` ```mindmap ````）的默认布局、行内画布高度与底部快捷键提示栏显隐，持久化于 `contrib-state.json` 并按插件隔离；停用插件时自动恢复默认，返回归一化后的状态对象。
4. **`host.ui.tarot_state` / `host.ui.set_tarot`**（宿主原生塔罗模式，适用于 `block-tarot` 官方扩展）
   - `host.ui.tarot_state`（参数 `{}`）：返回 `{"supported": true, "enabled": false, "room": "writing", "collected": 3, "total": 22, "daily_drawn": false, "daily_note": "", "active_note": ""}`。
   - `host.ui.set_tarot`（参数 `{"enabled": true, "toggle": false, "room": "writing"|"table"|"daily"|"deck"}`，字段均可选）：切换宿主原生塔罗模式（午夜铜版）或直接进入指定房间（从设置页指定 `room` 时自动收起设置），返回最新状态对象。

---

## 8. 语言扩展规范（`contributes.languages` + `grammars/*.json`）

语言扩展插件用于为 Block 补充代码语法高亮与编辑辅助，采用**纯声明式、零子进程、零权限**模型在宿主机内直接运行：

- **零进程与零权限**：当 `manifest.category` 为 `"language"`、`contributes.languages` 非空、且未声明 `entry.command`、`permissions`、`surfaces` 与任何运行时 UI 贡献（`titlebar_buttons` / `settings_sections` / `commands` / `page_menu` / `editor_menu` / `panels` / `status_items` / `dialogs`）时，插件以纯声明式模式加载，不启动任何外部子进程，安装页也不展示危险权限提示。
- **双场景同步生效**：编译后的语法同时作用于**打开其他文件夹时的源码编辑器**与 **Markdown 代码块**（按 `aliases` 与 `id` 匹配语言标识）。
- **内置语言与覆盖顺序**：主程序已内置 C、C++、C#、Java、Kotlin、Scala、Groovy/Gradle（含 `Jenkinsfile`）、Swift、Dart、Go（含 `go.mod` / `go.sum`）、Rust、Python（含 `requirements.txt`）、Ruby、PHP、Lua、R、Elixir、HEEx/EEx、JavaScript、TypeScript、HTML、Vue、Svelte、Astro、Handlebars/Mustache、EJS、Go/Helm 模板、CSS/SCSS/Less、JSON/JSONC/JSONL、YAML（含 Clash/Stash `.stoverride`）、TOML、INI/Properties/systemd/xcconfig、Dotenv（`.env` / `.env.*`）、Surge/代理规则（`.sgmodule` / `.lpx` / `.plugin` / `.module` / `.list` 及 Surge `.conf`）、Nginx、Apache/htaccess、HAProxy、Caddyfile、SSH Config、Crontab、Procfile、Ignore（`.gitignore` 等）、Hosts、HTTP 请求、XML（含 `.ps1xml`）、Property List（含 `.entitlements` / `.strings` / `.pbxproj`）、SQL、Prisma、GraphQL、Protobuf、Thrift、Shell、PowerShell、Windows Batch、Dockerfile、Makefile、CMake、Justfile、HCL、Nix、Zig、ERB、Jinja、Diff、Markdown 等常用语言，无需安装插件；语言扩展只需覆盖小众语言。已启用插件声明的文件名、后缀或标识与内置语言重名时，**插件优先**。
- **生命周期**：首次安装纯声明式语言扩展时**自动启用**；纯声明式语言扩展不占用侧栏设置页，可在应用商店详情页管理与卸载（卸载或停用后编辑器与代码块高亮**实时生效**，移除对应语言注册）。若插件同时声明了 `entry.command` 或 `surfaces`，则作为混合型交互插件按普通插件生命周期管理。已安装的旧版 Go Modules 和 Property List 扩展仍可管理，并按安装来源显示认证状态。卸载后由主程序提供高亮。
- **大代码块**：Markdown 代码块含超长单行时会使用基础高亮。整个代码块超过大小上限时会按纯文本显示，以保持编辑响应。
- **系统「打开方式」自动关联**：已启用插件声明的文件后缀会自动同步到 Windows、macOS、Linux 的系统「打开方式」菜单（仅靠 `filenames` 匹配的规则不注册；宿主打包期与运行期关联机制见下文「宿主打包期文件关联集成（主仓维护参考）」）。

### 8.1 `contributes.languages` 字段定义

`contributes.languages` 为对象数组，单插件最多声明 **16** 个语言条目；所有条目均须符合以下字段与约束：

| 字段 | 必填 | 类型与约束 |
| --- | --- | --- |
| `id` | 是 | 字符串，匹配 `^[a-z][a-z0-9_-]{0,31}$`，同一插件内不可重复 |
| `name` | 是 | `LocalizedText`（`zh` 与 `en` 均必填非空，禁止含 Emoji） |
| `extensions` | 与 `filenames` 至少其一 | 字符串数组，最多 **16** 项，不可重复；每项匹配 `^\.[a-z0-9][a-z0-9._-]{0,23}$`（小写点号前缀，如 `".plist"`、`".d.ts"`；匹配文件时按最长后缀优先，例如 `".d.ts"` 优先于 `".ts"`） |
| `filenames` | 与 `extensions` 至少其一 | 字符串数组，最多 **16** 项，不可重复；纯文件基本名（非空、`<= 64` 字节、不含 `/ \ :`，不可为 `.` 或 `..`） |
| `aliases` | 否 | 字符串数组，最多 **16** 项，不可重复；每项匹配 `^[a-z0-9][a-z0-9._+-]{0,31}$`（用于 Markdown 代码块围栏标识） |
| `display_tags` | 否 | 字符串数组，最多 **8** 项；每项去空白后非空且 `<= 24` 字节、禁止含 Emoji。用于商店列表与详情页展示；省略时优先回退 `filenames`，若为空则回退 `extensions`（商店索引 `file_extensions` 跨语言去重后最多保留 **6** 个） |
| `grammar` | 是 | 字符串，插件目录内相对 `/` 分隔路径，须以 `.json` 结尾，禁止以 `/` 开头、禁止含 `\` 或 `:`、禁止 `..` 路径逃逸；单文件大小上限 **256 KiB** |

### 8.2 `grammars/*.json` Schema（RE2 有状态行分词器）

Grammar JSON 采用严格解码：**禁止重复键、禁止未知字段**。

#### 顶层字段

| 字段 | 必填 | 类型与约束 |
| --- | --- | --- |
| `schema_version` | 是 | 固定整数 `1` |
| `line_comment` | 否 | 字符串，单行注释前缀（如 `"//"`、`"#"`） |
| `block_comment` | 否 | 2 元素字符串数组 `[open, close]`（如 `["<!--", "-->"]`） |
| `word_chars` | 否 | 字符串，追加到标识符词边界的额外字符集（如 `".-/"`） |
| `tab_size` | 否 | 整数，`0`–`16`（`0` 表示沿用编辑器默认） |
| `hard_tabs` | 否 | 布尔值，是否优先使用硬制表符 `\t`（默认 `false`） |
| `brackets` | 否 | `[open, close]` 二元组数组，用于括号匹配高亮 |
| `auto_close` | 否 | `[open, close]` 二元组数组，用于输入时自动闭合括号/引号 |
| `initial_state` | 否 | 字符串，初始状态名，省略时默认为 `"root"`；必须存在于 `states` 中 |
| `states` | 是 | 对象（`map[string][]GrammarRule`），至少 **1** 个、最多 **63** 个状态；状态名匹配 `^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`；每状态最多 **128** 条规则，全部状态合计最多 **512** 条规则；状态栈最大深度 **9** 层（超深时退化为替换当前栈顶） |
| `sample` | 否 | 字符串，用于设置页/详情页的语法高亮预览示例 |

#### `GrammarRule` 字段

| 字段 | 必填 | 类型与约束 |
| --- | --- | --- |
| `match` | 是 | 非空 RE2 正则表达式字符串；编译时自动以 `\A(?:...)` 锚定到当前行扫描位置 |
| `line_start` | 否 | 布尔值，默认 `false`；设为 `true` 时该规则仅在整行第 0 列（`pos == 0`）尝试匹配，用于整行行首定界符（如 frontmatter `---`、PowerShell here-string 结束符）而不改变已有规则的 `^` 语义 |
| `token` | 否 | 字符串，合法 Token 名称之一（见下表，大小写不敏感） |
| `captures` | 否 | 对象（`map[string]string`），键为 `"1"`–`"16"` 的捕获组编号（不可超过 `match` 的子表达式数量），值为合法 Token 名称 |
| `next` | 否 | 字符串，切换到指定状态名（须存在于 `states` 中） |
| `push` | 否 | 字符串，将指定状态名压栈并进入（须存在于 `states` 中） |
| `pop` | 否 | 布尔值，`true` 时弹栈返回上一状态（栈空时回到初始状态） |

规则约束：`token`、`captures`、`next`、`push`、`pop` 至少设置其一；`next`、`push`、`pop` 至多设置其一；零长度匹配仅在发生状态转移时允许，连续零步循环上限为 **8** 次。

扫描器行为说明：
- 每一步仅在当前剩余切片 `line[pos:]` 上匹配 `\A(?:match)`，因此 `^` 等价于当前扫描位置而非整行行首，`\b` 也会把 `line[pos:]` 开头视为词边界；为避免关键字规则误标标识符后缀（如 `untrue` 中的 `true`），请在关键字规则之后放置整词标识符规则（如 `[A-Za-z_][A-Za-z0-9_]*`）。
- 若当前位置无任何规则命中，扫描器消耗 **1 个 UTF-8 字符**并输出 `text`，然后继续前进。
- 当 `pos == len(line)` 到达行尾时循环直接结束，因此零宽 `$` 退栈规则不会在行尾触发；单行上下文请用消费到行尾的规则（如 `[^\n]*` 或换行外字符匹配）或状态切换处理。

#### 全部合法 `token` 名称（共 25 个）

- 基础与标识符：`text`、`keyword`、`type`、`function`、`variable`、`property`、`constant`
- 字面量与注释：`number`、`string`、`escape`、`regexp`、`comment`
- 语法符号与标记：`operator`、`punctuation`、`tag`、`attribute`、`meta`
- 文档与差异状态：`heading`、`emphasis`、`strong`、`link`、`code`、`inserted`、`deleted`、`invalid`

### 8.3 可复制的精简示例

目录结构：

```text
example-lang/
├── manifest.json
├── README.md
├── README.en.md
└── grammars/
    └── conf.json
```

`manifest.json`：

```json
{
  "app": "blockmd",
  "schema_version": 1,
  "id": "example-lang",
  "category": "language",
  "name": {
    "zh": "示例配置语言",
    "en": "Example Config"
  },
  "version": "1.0.0",
  "author": "Your Name",
  "repo": "your-org/example-lang",
  "summary": {
    "zh": "为 .conf 配置文件提供语法高亮",
    "en": "Syntax highlighting for .conf configuration files"
  },
  "permissions": [],
  "surfaces": [],
  "contributes": {
    "languages": [
      {
        "id": "example-conf",
        "name": {
          "zh": "示例配置",
          "en": "Example Config"
        },
        "extensions": [".conf"],
        "filenames": ["example.conf"],
        "aliases": ["conf", "example-conf"],
        "display_tags": [".conf"],
        "grammar": "grammars/conf.json"
      }
    ]
  },
  "icon": {
    "view_box": "0 0 24 24",
    "fill_paths": ["M5.5 3.5h8.5l4.5 4.5v12.5a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5v-17Z"],
    "stroke_paths": [
      "M5.5 3.5h8.5l4.5 4.5v12.5a1.5 1.5 0 0 1-1.5 1.5H7a1.5 1.5 0 0 1-1.5-1.5v-17Z",
      "M14 3.5V8h4.5"
    ],
    "accent_paths": ["M9 12.5h6", "M9 16h4"]
  }
}
```

`grammars/conf.json`：

```json
{
  "schema_version": 1,
  "line_comment": "#",
  "tab_size": 2,
  "brackets": [["[", "]"], ["{", "}"]],
  "auto_close": [["\"", "\""], ["[", "]"]],
  "initial_state": "root",
  "states": {
    "root": [
      { "match": "#.*$", "token": "comment" },
      { "match": "\\[[^\\]]+\\]", "token": "heading" },
      { "match": "\"(?:\\\\.|[^\"\\\\])*\"", "token": "string" },
      { "match": "([A-Za-z_][A-Za-z0-9_.-]*)(\\s*=)", "captures": { "1": "property", "2": "operator" } },
      { "match": "(?:true|false|null)\\b", "token": "constant" },
      { "match": "[A-Za-z_][A-Za-z0-9_]*", "token": "text" },
      { "match": "[0-9]+(?:\\.[0-9]+)?\\b", "token": "number" },
      { "match": "[ \\t]+", "token": "text" }
    ]
  },
  "sample": "# Example config\n[server]\nhost = \"127.0.0.1\"\nport = 8080\ntls = true"
}
```

### 8.4 宿主打包期文件关联集成（主仓维护参考）

主程序内置支持的全部语言与格式，以及已启用/停用/卸载的语言扩展，都会自动汇总并同步到 Windows、macOS、Linux 的系统「打开方式」菜单，均无需人工维护清单；仅靠 `filenames` 匹配的规则不会注册到系统菜单：

- **macOS**：打包期通过 `cmd/block-fileassoc` 自动生成主程序 `Info.plist`；运行期维护按安装路径隔离的隐形中转应用（`dev.eachann.block.associations.<hash>`），原子交换更新并跳过主程序 `Info.plist` 与 `public.plain-text` 已接管的类型。
- **Windows**：打包期通过 `cmd/block-fileassoc` 自动生成 NSIS 机器级 `Block.Machine.Document` 的 `OpenWithProgids`；用户版安装与运行期按安装形态分别注册 `Block.User.Document`、`Block.Machine.User.Document` 或按路径哈希隔离的 `Block.Portable.<hash>`，仅写入 `OpenWithProgids` 而不抢占默认程序。
- **Linux**：`.deb` 安装后在 `postinst` 降权为安装用户执行 `--sync-user-linux-file-associations`，并通过 `/etc/xdg/autostart/block-fileassoc.desktop` 覆盖其他桌面用户，确保首次启动前即可出现在文件管理器「打开方式」中；用户脚本安装通过 `cmd/block-fileassoc -install-linux-desktop` 写入独立 `block-user.desktop`；AppImage 为避免外部集成器在无默认程序保护的情况下写入静态 `MimeType=` 劫持既有默认关联，在首次运行或显式执行 `--sync-user-linux-file-associations` 时完成受保护注册。所有 Linux 注册均按安装路径隔离 `NoDisplay=true` 入口，并在写入前对非 Block 默认程序加钉保护（跨实例共享引用计数、支持 `mimeapps.list` 符号链接保真与断电恢复）。

---

## 9. 本地协议检查命令

```bash
CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check /绝对路径/到你的插件目录
```

终端输出 `[PASS] Plugin "<id>" passed all Block App Store checks.` 且退出码为 `0` 表示本地协议检查通过。若清单仍声明了已默认放行的权限（如 `storage`、`ui:*`、`system:notify`、`system:badge`），校验器会输出 `[WARN]` 提示删减（不阻断通过）。

- **自动检查流水**：在临时笔记本与插件数据目录中执行（目录安装完成后，按语言各自起算 20 秒整轮 RPC 预算，其中 `plugin.initialize` 单独执行与桌面端一致的 **15 秒**预算）：依次校验 `manifest.json`（含 `permission_reasons` 键匹配、字数与禁 Emoji 规则）、目录安装、初始化、无参初始渲染、携带完整默认值渲染、递归收集卡片顶层与 `group` 内的 `segmented`/`select`/`switch` 并逐项单值切换渲染（不做多控件组合，不遍历 `checklist`）、调用初始页全部 `header_actions` 与顶层 `actions` 按钮（不判断 `visible_when` 与折叠状态；对返回的 `open_path`/`reveal_path` 仅以 `os.Stat` 验证存在性，`ok: false` 仍可通过）、在声明 `"events:note_saved"` 时直接发送 `note.saved` 事件（不走桌面队列）以及 `plugin.shutdown` 回收（纯声明式语言扩展无需子进程，仅校验清单、双语文档与语法编译）。
- **双语门禁**：当 `manifest.repo` 属于官方仓库（`blockmd-plugins/*`）时，校验器会**自动启用双语检查**（官方仓库 CI 建议显式追加 `-bilingual` 参数，避免清单漏写 `repo` 时跳过双语门禁；社区插件也可追加 `-bilingual` 主动开启）：
  1. 校验清单中 `name.en`、`summary.en`、`contributes.*.en` 及已声明的 `permission_reasons[*].en` 均为非空纯英文且不含中文字符或中文标点；
  2. 校验根目录存在精确命名的普通文件 `README.md`（<= 512 KiB UTF-8，非空且含中文）与 `README.en.md`（<= 512 KiB UTF-8，非空、不含中文字符或中文标点、且与 `README.md` 不同）；若 README 引用了本地图片或 SVG，本地资产须存在，位图与含中文资源须为中英文版本分别提供独立路径，纯英文 UTF-8 `.svg`（零中文字符及全角标点）可由 `README.md` 与 `README.en.md` 共用同一路径（英文版引用的 `.svg` 不得包含中文字符或全角标点）；
  3. 交互式插件分别以 `locale="zh"` 与 `locale="en"` 各运行一轮完整检查，校验英文模式下全部界面文案与动作反馈零中文残留，且中英文初始渲染输出不同（注意：CLI 不自动调用 `plugin.command`，命令返回文案需在桌面端英文模式下复测）。
- **CLI 边界说明**：CLI 仅提供单个临时笔记本、不校验桌面默认停用与编辑器脏状态写保护，且未注入 `GetPerfSnapshot`（调用 `host.perf.snapshot` 会收到 `-32005`）；上传、同步、迁移与真实桌面交互仍需在 Block 桌面端使用临时笔记本进一步验证。

---

## 10. 发布到应用商店（`blockmd-plugin` 自动采集）

Block 应用商店采用无审批自动采集机制。**硬门槛与采集上限**：采集器抓取 `topic:blockmd-plugin fork:false archived:false` 按 stars 降序的**前 100 个公开、非 Fork、未归档 GitHub 仓库**（无分页；`verified.json` 中未落入前 100 的官方仓库会单独拉取），并自动跳过内置保留 ID（`block-diagnostics`）；桌面端通过本地目录、`owner/repo` 或 HTTPS 压缩包手动安装不受此 100 仓库上限限制。

发布步骤：

1. 将插件代码推送到公开、非 Fork、未归档的 GitHub 仓库，确保仓库根目录（或归档内根/唯一单层顶层目录）包含通过「本地协议检查命令」检查的 `manifest.json`（含 `"app": "blockmd"`；若填写 `repo` 须与仓库 `owner/repo` 一致）。**官方插件（`blockmd-plugins/*`，其上架认证以 `verified.json` 为准）必须同时放置 `README.md`（中文）与 `README.en.md`（英文），README 引用的位图及含中文资源须提供独立的中英文版本（纯英文 `.svg` 可共用），有子进程 UI 的插件还须支持 `locale="en"` 英文界面**；已审核作者（`eachann1024/*`）与社区插件同样建议提供双语文档、双语配图与 `permission_reasons`。
2. 在 GitHub 仓库主页右侧 **About -> Topics** 添加标签：**`blockmd-plugin`**。
3. 发布标准 **GitHub Release**（不要标为 Draft 或 Prerelease，建议保持 Release Tag 与包内 `manifest.version` 一致）：
   - **官方推荐自动版本号与发布门禁**：复用 `blockmd-plugins/block-sync` 的 `.github/workflows/release.yml`（`Daily Release`）。仓库 `manifest.json` 仅保留主次版本基线（如 `"1.2.0"`），日常改动直接推送 `main`；工作流每天 `00:00 UTC`（或手动 `workflow_dispatch`）检测代码或双语 README 变更，**打包前强制执行 `README.md` / `README.en.md` 与 `locale="en"` 英文界面门禁（未通过即中止发布）**，通过后以 `<major>.<minor>.<提交总数>` 生成候选版本（若不高于已有最高版本则递增）动态写入产物包并发布 Release，不向 `main` 回写提交。
   - **手动发版**：也可推送 `vX.Y.Z` Tag 触发同工作流打包对应版本。

4. **线上采集与校验规则**（由 `blockmd-plugins/store-index` 仓库的采集任务及 `cmd/block-store-crawler` 执行）：
   - **选包顺序**：拉取仓库 `/releases` 列表并筛选首个非草稿（`!draft`）、非预发布（`!prerelease`）的 Release，优先取其首个 `.tar.gz`/`.tgz`/`.zip` 附件，无匹配附件时回退该 Release 的 `tarball_url`（无 `zipball_url` 分支）；仅在尚未选出下载 URL 时才回退默认分支源码包，且会先解析默认分支当前提交 SHA 并固定到不可变归档地址（`codeload.github.com/{repo}/tar.gz/{commit_sha}`），防止后续推送导致索引 `sha256` 失效。已选归档下载或解析失败会直接跳过该仓库，不向低优先级回退重试。
   - **索引校验与同步字段**：采集器先对归档原始字节计算 `sha256` 并提取根或单层顶层目录下的 `manifest.json`（不运行 `block-plugin-check`，版本以包内 `manifest.version` 为准），依次执行 `app == "blockmd"`、非空 `id`、保留 ID 与 `blocklist` 过滤、官方 `verified.json` 1:1 绑定、官方插件双语文档（`README.md` 中文 + `README.en.md` 纯英文）、双语无 Emoji 清单与显式商店分类（`category` 必填且不得为已停用的 `sync` / `publish`）校验、`repo` 匹配、声明式语法文件编译校验与跨仓重复 `id` 处理（若上一版索引中已有归属仓库则保留原归属，否则全部剔除），并将 `category`、`permission_reasons` 与 `file_extensions`（最多 6 个展示标签）同步到商店索引；若任一官方认证条目缺失或校验不通过，整轮索引生成会直接失败。
   - **更新日志采集与对外接口**：采集器在拉取插件 Releases 时，自动提取最近版本的结构化双语更新日志（解析 `## 中文` / `## English` 与 `新增功能` / `体验优化` / `问题修复` / `其他改动`），在 `store-index.json` 的每个插件条目中内联最近 5 条 `changelog` 与 `changelog_url`，同时在 `blockmd-plugins/store-index` 仓库的 `changelogs/<id>.json`（附带 jsDelivr 镜像）对外提供最多 20 条完整版本历史的只读 JSON API。Block 应用商店详情页提供「说明 / 更新日志」标签页展示版本变更，插件亦可通过 `host.store.changelog` RPC 读取同源更新日志。
   - **商店排序规则**：索引 `downloads` 为该仓库各 Release 附件下载次数之和，采集时更新。应用商店列表默认按 `downloads` 从高到低排列并标注名次，下载量相同时按名称、再按 `id` 排列；有搜索词时先按匹配度排列，再按下载量；筛选菜单选「按名称」时不按下载量排列、不标名次。本机安装只计入展示数字，不改变名次，直到下次索引更新。
   - **桌面端不同安装来源的校验力度与权限确认**：从应用商店或手动输入商店插件 `id` 安装时，强制 HTTPS（<= 4096 字节、无凭据与 fragment、限制重定向主机）、核对 `sha256` 及包内清单的 `id`/`version`/`permissions` 集合与（非空时）`repo`；手动输入 `owner/repo`、`github:owner/repo`、`https://github.com/owner/repo`（拉取 `HEAD` 源码包）或直接 HTTPS 压缩包 URL（同样受 URL 长度、凭据、fragment 与重定向主机约束）或本地目录路径（支持 `~/`、`file://` 或点击「本地目录」）会先暂存并校验清单与打包上限，但不做商店 `sha256`/索引比对。无论商店还是手动来源，首次安装请求 5 类敏感权限任一项或更新新增敏感权限时，桌面端均会先弹出权限确认卡片，用户确认后才落盘；「全部更新」会先自动更新无新增敏感权限的插件，将新增敏感权限的插件留待用户逐项确认（若此时已离开商店页，则通过右上角通知横幅引导返回确认）。

- 带系统权限的发行包通过索引的 `system_apps` 提供，支持该字段的 Block 会优先选用。旧客户端从 `apps` 读取已验证的兼容包。同步插件为旧客户端保留 v1.3.44。随包索引与采集器使用相同分流规则，采集器校验兼容包的固定 SHA-256 后才写入索引。
- **随包内置种子来源（`builtin://`）**：主程序在编译期通过内嵌 `embed.FS` 嵌入离线兜底索引 `default_index.json`，并仅将历史内置插件 `plugins/tag-manager`（`block-tag-manager`）通过 `RegisterBuiltinCatalogApp` 注册为 `builtin://block-tag-manager` 种子条目（合并目录时优先于远端及离线索引，并过滤保留 ID `block-diagnostics`）。用户点击安装前不解包落盘，查看详情说明时直接从内嵌 `embed.FS` 读取 `README.zh.md` / `README.en.md` / `README.md`；点击安装时才从内嵌 `embed.FS` 解压暂存（免网络请求与 `sha256` 校验，仅核对包内 `id` 并校验清单与敏感权限确认），随后写入 `<BlockAppData>/apps/<id>/`（安装来源记为 `builtin:<id>`，首次安装仍遵循默认停用生命周期，后续同源自动更新受 `UpdateEntryNoEscalation` 约束且禁止新增敏感权限）。`builtin://` 仅用于上述随包历史特例，外部插件一律通过独立公开仓库与 `blockmd-plugin` Topic 参与自动采集，勿提交到主仓 `plugins/`。
