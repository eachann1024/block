<!-- ai-guide:skip-start -->
# 本地协议检查与发布到应用商店

> [← 协议与 UI](protocol-and-ui.md) · [文档中心](../README.md) · [UI Contributions →](ui-contributions.md)

---
<!-- ai-guide:skip-end -->

## 6. 本地协议检查命令

以下命令在临时笔记本与插件数据目录中执行本地协议检查（目录安装完成后，按语言各自起算 20 秒整轮 RPC 预算，其中 `plugin.initialize` 单独执行与桌面端一致的 **15 秒**预算）：依次校验 `manifest.json`（含 `permission_reasons` 键匹配、字数与禁 Emoji 规则）、目录安装、初始化、无参初始渲染、携带完整默认值渲染、递归收集卡片顶层与 `group` 内的 `segmented`/`select`/`switch` 并逐项单值切换渲染（不做多控件组合，不遍历 `checklist`）、调用初始页全部 `header_actions` 与顶层 `actions` 按钮（不判断 `visible_when` 与折叠状态；对返回的 `open_path`/`reveal_path` 仅以 `os.Stat` 验证存在性，`ok: false` 仍可通过）、在声明 `"events:note_saved"` 时直接发送 `note.saved` 事件（不走桌面队列）以及 `plugin.shutdown` 回收（纯声明式语言扩展无需子进程，仅校验清单、双语文档与语法编译）：

```bash
CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check /绝对路径/到你的插件目录
```

- **双语门禁**：当 `manifest.repo` 属于官方仓库（`blockmd-plugins/*`）时，校验器会**自动启用双语检查**（官方仓库 CI 建议显式追加 `-bilingual` 参数，避免清单漏写 `repo` 时跳过双语门禁；社区插件也可追加 `-bilingual` 主动开启）：
  1. 校验清单中 `name.en`、`summary.en`、`contributes.*.en` 及已声明的 `permission_reasons[*].en` 均为非空纯英文且不含中文字符或中文标点；
  2. 校验根目录存在精确命名的普通文件 `README.md`（<= 512 KiB UTF-8，非空且含中文）与 `README.en.md`（<= 512 KiB UTF-8，非空、不含中文字符或中文标点、且与 `README.md` 不同）；
  3. 交互式插件分别以 `locale="zh"` 与 `locale="en"` 各运行一轮完整检查，校验英文模式下全部界面文案与动作反馈零中文残留，且中英文初始渲染输出不同（注意：CLI 不自动调用 `plugin.command`，命令返回文案需在桌面端英文模式下复测）。

终端输出 `[PASS] Plugin "<id>" passed all Block App Store checks.` 且退出码为 `0` 表示本地协议检查通过。注意：CLI 仅提供单个临时笔记本、不校验桌面默认停用与编辑器脏状态写保护，且未注入 `GetPerfSnapshot`（调用 `host.perf.snapshot` 会收到 `-32005`）；上传、同步、迁移与真实桌面交互仍需在 Block 桌面端使用临时笔记本进一步验证。

---

## 7. 发布到应用商店（`blockmd-plugin` 自动采集）

Block 应用商店采用无审批自动采集机制，发布步骤如下：

1. 将插件代码推送到公开、非 Fork、未归档的 GitHub 仓库，确保仓库根目录（或归档内根/唯一单层顶层目录）包含通过第 6 节检查的 `manifest.json`（含 `"app": "blockmd"`；若填写 `repo` 须与仓库 `owner/repo` 一致）。**官方插件（`blockmd-plugins/*`，其上架认证以 `verified.json` 为准）必须同时放置 `README.md`（中文）与 `README.en.md`（英文），有子进程 UI 的插件还须支持 `locale="en"` 英文界面**；已审核作者（`eachann1024/*`）与社区插件同样建议提供双语文档与 `permission_reasons`。
2. 在 GitHub 仓库主页右侧 **About -> Topics** 添加标签：**`blockmd-plugin`**。
3. 发布标准 **GitHub Release**（不要标为 Draft 或 Prerelease，建议保持 Release Tag 与包内 `manifest.version` 一致）：
   - **官方推荐自动版本号与发布门禁**：复用 `blockmd-plugins/block-sync` 的 `.github/workflows/release.yml`（`Daily Release`）。仓库 `manifest.json` 仅保留主次版本基线（如 `"1.2.0"`），日常改动直接推送 `main`；工作流每天 `00:00 UTC`（或手动 `workflow_dispatch`）检测代码或双语 README 变更，**打包前强制执行 `README.md` / `README.en.md` 与 `locale="en"` 英文界面门禁（未通过即中止发布）**，通过后以 `<major>.<minor>.<提交总数>` 生成候选版本（若不高于已有最高版本则递增）动态写入产物包并发布 Release，不向 `main` 回写提交。
   - **手动发版**：也可推送 `vX.Y.Z` Tag 触发同工作流打包对应版本。
4. **线上采集与校验规则**（由 `blockmd-plugins/store-index` 仓库的采集任务及 `cmd/block-store-crawler` 执行）：
   - 采集器抓取 `topic:blockmd-plugin fork:false archived:false` 按 stars 降序的前 100 个仓库（无分页），并对 `verified.json` 中未落入前 100 的官方仓库单独拉取，同时自动跳过内置保留 ID（`block-diagnostics`）。
   - **选包顺序**：拉取仓库 `/releases` 列表并筛选首个非草稿（`!draft`）、非预发布（`!prerelease`）的 Release，优先取其首个 `.tar.gz`/`.tgz`/`.zip` 附件，无匹配附件时回退该 Release 的 `tarball_url`（无 `zipball_url` 分支）；仅在尚未选出下载 URL 时才回退默认分支源码包，且会先解析默认分支当前提交 SHA 并固定到不可变归档地址（`codeload.github.com/{repo}/tar.gz/{commit_sha}`），防止后续推送导致索引 `sha256` 失效。已选归档下载或解析失败会直接跳过该仓库，不向低优先级回退重试。
   - **索引校验与同步字段**：采集器先对归档原始字节计算 `sha256` 并提取根或单层顶层目录下的 `manifest.json`（不运行 `block-plugin-check`，版本以包内 `manifest.version` 为准），依次执行 `app == "blockmd"`、非空 `id`、保留 ID 与 `blocklist` 过滤、官方 `verified.json` 1:1 绑定、官方插件双语文档（`README.md` 中文 + `README.en.md` 纯英文）与双语无 Emoji 清单校验、`repo` 匹配、声明式语法文件编译校验与跨仓重复 `id` 处理（若上一版索引中已有归属仓库则保留原归属，否则全部剔除），并将 `category`、`permission_reasons` 与 `file_extensions`（最多 6 个展示标签）同步到商店索引；若任一官方认证条目缺失或校验不通过，整轮索引生成会直接失败。
   - **更新日志采集与对外接口**：采集器在拉取插件 Releases 时，自动提取最近版本的结构化双语更新日志（解析 `## 中文` / `## English` 与 `新增功能` / `体验优化` / `问题修复` / `其他改动`），在 `store-index.json` 的每个插件条目中内联最近 5 条 `changelog` 与 `changelog_url`，同时在 `blockmd-plugins/store-index` 仓库的 `changelogs/<id>.json`（附带 jsDelivr 镜像）对外提供最多 20 条完整版本历史的只读 JSON API。Block 应用商店详情页提供「说明 / 更新日志」标签页展示版本变更，插件亦可通过 `host.store.changelog` RPC 读取同源更新日志。
   - **桌面端不同安装来源的校验力度**：从应用商店或手动输入商店插件 `id` 安装时，强制 HTTPS（<= 4096 字节、无凭据与 fragment、限制重定向主机）、核对 `sha256` 及包内清单的 `id`/`version`/`permissions` 集合与（非空时）`repo`；手动输入 `owner/repo`、`github:owner/repo`、`https://github.com/owner/repo`（拉取 `HEAD` 源码包）或直接 HTTPS 压缩包 URL（同样受 URL 长度、凭据、fragment 与重定向主机约束）或本地目录路径（支持 `~/`、`file://` 或点击「本地目录」）会校验清单与打包上限，但不做商店 `sha256`/索引比对。

- 带系统权限的发行包通过索引的 `system_apps` 提供，支持该字段的 Block 会优先选用。旧客户端从 `apps` 读取已验证的兼容包。同步插件为旧客户端保留 v1.3.44。随包索引与采集器使用相同分流规则，采集器校验兼容包的固定 SHA-256 后才写入索引。

<!-- ai-guide:skip-start -->
---

下一步：[5. UI Contributions（宿主界面扩展贡献） →](ui-contributions.md)
<!-- ai-guide:skip-end -->
