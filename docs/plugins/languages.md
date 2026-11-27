<!-- ai-guide:skip-start -->
# 语言扩展规范（`contributes.languages` + `grammars/*.json`）

> [← 宿主界面扩展](ui-contributions.md) · [文档中心](../README.md) · [检查与上架 →](check-and-store.md)

---
<!-- ai-guide:skip-end -->

## 8. 语言扩展规范（`contributes.languages` + `grammars/*.json`）

语言扩展插件用于为 Block 补充代码语法高亮与编辑辅助，采用**纯声明式、零子进程、零权限**模型在宿主机内直接运行：

- **零进程与零权限**：当 `manifest.category` 为 `"language"`、`contributes.languages` 非空、且未声明 `entry`、`permissions`、`surfaces` 与任何运行时 UI 贡献（`titlebar_buttons` / `settings_sections` / `commands` / `page_menu` / `editor_menu` / `panels` / `status_items` / `dialogs`）时，插件以纯声明式模式加载，不启动任何运行时或外部子进程，安装页也不展示危险权限提示。
- **双场景同步生效**：编译后的语法同时作用于**打开其他文件夹时的源码编辑器**与 **Markdown 代码块**（按 `aliases` 与 `id` 匹配语言标识）。
- **内置语言与覆盖顺序**：主程序已内置 C、C++、C#、Java、Kotlin、Scala、Groovy/Gradle（含 `Jenkinsfile`）、Swift、Dart、Go（含 `go.mod` / `go.sum`）、Rust、Python（含 `requirements.txt`）、Ruby、PHP、Lua、R、Elixir、HEEx/EEx、JavaScript、TypeScript、HTML、Vue、Svelte、Astro、Handlebars/Mustache、EJS、Go/Helm 模板、CSS/SCSS/Less、JSON/JSONC/JSONL、YAML（含 Clash/Stash `.stoverride`）、TOML、INI/Properties/systemd/xcconfig、Dotenv（`.env` / `.env.*`）、Surge/代理规则（`.sgmodule` / `.lpx` / `.plugin` / `.module` / `.list` 及 Surge `.conf`）、Nginx、Apache/htaccess、HAProxy、Caddyfile、SSH Config、Crontab、Procfile、Ignore（`.gitignore` 等）、Hosts、HTTP 请求、XML（含 `.ps1xml`）、Property List（含 `.entitlements` / `.strings` / `.pbxproj`）、SQL、Prisma、GraphQL、Protobuf、Thrift、Shell、PowerShell、Windows Batch、Dockerfile、Makefile、CMake、Justfile、HCL、Nix、Zig、ERB、Jinja、Diff、Markdown 等常用语言，无需安装插件；语言扩展只需覆盖小众语言。已启用插件声明的文件名、后缀或标识与内置语言重名时，**插件优先**。
- **生命周期**：首次安装纯声明式语言扩展时**自动启用**；纯声明式语言扩展不占用侧栏设置页，可在应用商店详情页管理与卸载（卸载或停用后编辑器与代码块高亮**实时生效**，移除对应语言注册）。若插件声明了运行时入口、权限或界面表面，则遵循 [清单与权限](manifest.md) 的「字段约束」与交互式插件生命周期。已安装的旧版 Go Modules 和 Property List 扩展仍可管理，并按安装来源显示认证状态。卸载后由主程序提供高亮。
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

<!-- ai-guide:skip-start -->
---

下一步：[6. 检查与上架 →](check-and-store.md)
<!-- ai-guide:skip-end -->
