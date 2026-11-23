<!-- ai-guide:skip-start -->
# Block 插件开发文档

> [返回文档中心](../README.md) · [清单与权限](manifest.md) · [协议与 UI](protocol-and-ui.md) · [检查与上架](check-and-store.md) · [UI Contributions](ui-contributions.md) · [语言扩展](languages.md) · [AI 单文件全量规范](../plugin-dev-for-ai.md)

本目录按主题拆分了 Block 应用商店插件（`blockmd`）的完整开发规范。你可以按需分章阅读，也可以直接把自动合成的单文件 [plugin-dev-for-ai.md](../plugin-dev-for-ai.md) 投喂给 AI 构建插件。

---
<!-- ai-guide:skip-end -->

## 1. 架构与硬边界

- **独立进程与安装模型**：只安装信任的插件。子进程使用 Block 的系统权限，清单 `permissions` 仅控制宿主 RPC。
  - 安装路径：`<BlockAppData>/apps/<plugin-id>/`
  - 私有存储：`<BlockAppData>/apps-data/<plugin-id>/state.json`（停用状态与目录缓存位于 `<BlockAppData>/apps-cache/`）
  - 外部深度链接（`block://`、`blockmd://` 的 `store` / `install`）：按 `repo` → `url` → `id` 顺序取**第一个非空**查询参数，仅用该单一值匹配当前目录中的 `id`、`repo` 或精确 `download_url`（未命中时不会回退尝试后续参数，直接停留在商店列表），不自动安装。
  - 新安装的交互式插件默认停用，用户手动启用后才会启动；纯声明式语言扩展首次安装自动启用；同 `id` 升级保留原有启用状态。商店卡片的「更新」按钮仅在目录版本高于已装版本时出现；手动安装同 `id` 不要求版本递增。
  - **替换与权限确认机制**：首次安装请求敏感权限（笔记与编辑、系统凭据、剪贴板、系统文件与外部操作、网络访问）的插件，或更新时新增敏感权限，宿主会先弹出权限确认卡片，用户点击确认后才落盘；「全部更新」会先更新无新增敏感权限的插件，将新增敏感权限的更新留待用户逐项确认。宿主校验候选包与现有包清单、停止旧进程，再通过两步目录重命名配合 `.previous` 备份完成可恢复替换；非交互接口默认拒绝跨来源、旧包无来源记录或新增权限的替换。桌面卸载移除插件程序，并保留私有配置供重新安装。
  - **保留 ID**：`block-diagnostics` 对应 Block 内置「性能与占用」窗口，宿主拒绝安装与启用该 ID、设置侧栏不挂载，远程采集器、缓存合并与离线内置索引回退均会过滤该 ID。
- **跨语言双向 JSON-RPC 2.0**：宿主以独立子进程启动 `manifest.json` 中的 `entry.command`（推荐 `["python3", "-u", "main.py"]`、`["node", "main.mjs"]` 或编译后可执行文件），通过 UTF-8 **按行分隔（`\n`）的 JSON-RPC 2.0** 在 `stdin` / `stdout` 双向通信，`stderr` 仅用于调试（宿主用 64 KiB 环形缓冲区保留尾部）。切换桌面笔记本或界面语言（`zh` / `en`）不会立即重启全部后台进程：仅在下次调用该插件且目标笔记本或语言发生变化时，宿主才关闭旧进程并按新上下文重新初始化（当前打开的活跃表面会随切换触发重渲染，从而立即按新上下文重启）。
  - `entry.command[0]` 必须是 `PATH` 可执行文件名（走 `LookPath`），或插件目录内解析后的相对普通文件（如 `"./bin/plugin"`，禁止符号链接、冒号或越出插件目录）。
  - **环境变量白名单**：宿主仅显式传入 `PATH`、`HOME`、`TMPDIR`、`PYTHONUNBUFFERED=1`、`BLOCK_PLUGIN_ID`、`BLOCK_DATA_DIR`、`BLOCKMD_PLUGIN_ID`、`BLOCKMD_DATA_DIR`、`BLOCKMD_DOWNLOADS_DIR`，不继承其他宿主环境变量。Windows 上不传入 `USERPROFILE` / `APPDATA` 等目录变量，插件应直接使用 `plugin.initialize` 传入的 `data_dir` / `downloads_dir` 或上述环境变量，避免依赖 `Path.home()`。
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
- **原生设置挂载、应用商店与详情页**：
  - **侧栏与顶栏控制**：声明了 `settings_page` 的已安装插件统一收拢在「设置」左侧导航栏「已安装」分组下（位于「应用商店」之下），带运行状态指示点；未声明独立设置页的插件（如语言扩展、主题类）不占用侧栏设置页，点击应用商店整张卡片进入详情页即可直接卸载。插件设置页头部与设置窗口标题栏统一采用 Ocean Paper 拨弦开关（Pluck Switch / `PluckDivider`）作为主控开关，并自动提供单行合并状态位、主操作位/实时进度胶囊与「⋯」更多菜单（含次要操作、查看说明/应用商店、重新加载与卸载），插件无需自行绘制启用开关或卸载按钮。
  - **分类、筛选与搜索**：应用商店固定提供 5 个分类标签（外观 / 编辑 / 整理 / AI / 工具），代码高亮扩展仅在搜索或已安装时可见，支持按安装状态（全部 / 已安装 / 未安装）与来源筛选，支持中英文名称与摘要联合模糊搜索并高亮命中字符；目录拉取在主源不可达时自动回退 jsDelivr CDN。
  - **三级信任标识**：应用商店按来源区分三类状态——`Block 官方`（`blockmd-plugins/*` 且在 `verified.json` 1:1 绑定，展示官方徽章）、`已审核`（`eachann1024/*` 信任作者源）、`未审核`（其余社区仓库，展示审阅风险提示）。
  - **权限胶囊悬停说明**：商店详情页在元信息行下方以胶囊形式展示该插件申请的 5 类敏感权限（免声明能力与静默事件订阅不展示），鼠标悬停每个权限胶囊时弹出结构化信息卡片：若插件在 `manifest.permission_reasons` 中填写了自定义缘由，优先展示该插件的具体用途并附带系统权限范围说明；未填写时自动展示系统内置的双语权限说明。
  - **详情页 README 渲染**：点击卡片在商店内以无嵌套滚动的只读编辑器直排渲染插件说明文档（中文优先，正文上限 512 KiB UTF-8），支持 GitHub 风格 `<picture>` 深浅色自适应图：
    - 本地已安装包按语言查根目录普通文件（不区分大小写）：中文依次查 `README.zh.md` → `README.md` → `README.markdown` → `README.txt` → `README`；英文依次查 `README.en.md` → `README.en.markdown` → `README.en.txt` → `README.md` → `README.markdown` → `README.txt` → `README`。
    - 本地缺失、为空或读取失败且存在有效仓库时优先回退 GitHub API（英文先查 `contents/README.en.md`，404 时再回退 `/readme`；中文直接查 `/readme`），当 GitHub API 不可达或限流时自动回退 jsDelivr CDN（`README.en.md` / `README.md`）；切换界面语言或完成安装/更新时自动刷新详情页说明。
    - 详情页支持相对路径图片、SVG、GIF（超动画预算可降为静态）与受限视频（最多 24 项媒体，其中视频最多 2 项，单张图片 <= 6 MiB，单个视频 <= 24 MiB）。
- **两项强制校验规则（违规直接拒绝加载）**：
  1. **禁止 Emoji**：`manifest.json` 的 `name`、`author`、`summary`、`permission_reasons`、`contributes` 静态文案，以及 `UIPage` 的静态界面文案（`page.title`、`card.title`、控件 `label`、按钮 `label`、`placeholder`、`empty_text`、`kv.rows[].label`、`metrics[].label`、`checklist.options[].badge`、`treemap` 瓦片 `label`/`detail` 与 `summary.total_label`、`preview.badge`/`cards[].badge`）**严禁包含 Emoji**（覆盖标准与扩展 Emoji 区块、变体选择符 `U+FE00–U+FE0F`、区域指示符/标签序列、ZWJ/键帽组合及常见 Emoji 符号如 `⭐`/`⌚`/`‼` 等），图标仅通过 `manifest.icon` 双色 SVG 路径定义。用户笔记内容、路径等动态字段（如 `page.status`、`card.meta`、`options[].label`/`detail`、`kv.rows[].value`、`metrics[].value`、`preview` 标题与正文、`log_list.entries`）放行用户原有字符，但插件自写文案仍应避免 Emoji。
  2. **严格 JSON 解码**：`manifest.json`（上限 256 KiB，须为单个 JSON 对象，且禁止重复键）与 `UIPage` 启用 `DisallowUnknownFields`，**严禁出现未定义字段名**（外层 JSON-RPC 包与 `host.*` 参数使用普通解码；`UIItem` 为共享结构，已知字段仅在消费它的 `kind` 上生效）。

<!-- ai-guide:skip-start -->
---

下一步：[2. 目录结构与 `manifest.json` 规范 →](manifest.md)
<!-- ai-guide:skip-end -->
