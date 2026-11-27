<!-- ai-guide:skip-start -->
# Language Extensions (`contributes.languages` + `grammars/*.json`)

> [← Host UI Contributions](ui-contributions.md) · [Docs Home](../README.md) · [Check & Store →](check-and-store.md)

---
<!-- ai-guide:skip-end -->

## 8. Language Extension Specification

Language extensions add syntax highlighting and editor assistance using a **purely declarative, zero-process, zero-permission** model executed directly inside the host:

- **Zero Process & Zero Permissions**: When `manifest.category` is `"language"`, `contributes.languages` is non-empty, and the manifest omits `entry`, keeps `permissions` empty (`[]`—even adding `"storage"` disqualifies entry-less mode), keeps `surfaces` empty, and declares no `contributes` field other than `languages` (none of `titlebar_buttons`, `titlebar_manager`, `settings_sections`, `commands`, `*_menu`, `panels`, `sidebar_sections`, `file_badges`, `file_icons`, `backdrops`, `block_decorators`, `themes`, `stages`, `status_items`, `dialogs`, or `menubar_items`), Block loads the package declaratively without starting any sandbox or child process.
- **Dual Coverage**: Compiled grammars apply to both the source code editor (when opening non-Markdown folders/files) and Markdown fenced code blocks (matched by `aliases` and `id`).
- **Built-in Languages & Precedence**: Block ships built-in grammars for common languages (C/C++/C#, Java/Kotlin/Scala/Groovy, Swift/Dart, Go incl. `go.mod`/`go.sum`, Rust, Python, Ruby, PHP, Lua, R, Elixir/HEEx, JS/TS/HTML/Vue/Svelte/Astro/CSS/SCSS/Less, JSON/YAML/TOML/INI/Dotenv, Surge/Nginx/Apache/HAProxy/Caddyfile/SSH/Crontab/Procfile/Ignore/Hosts/HTTP, XML/Property List, SQL/Prisma/GraphQL/Protobuf/Thrift, Shell/PowerShell/Batch, Dockerfile/Makefile/CMake/Justfile/HCL/Nix/Zig, ERB/Jinja/Diff/Markdown). Language extensions are intended for additional or niche formats; when an enabled extension overlaps a built-in extension, filename, or alias, **the extension takes precedence**.
- **Lifecycle**: Pure declarative language extensions auto-enable on first install and are managed/uninstalled from their Store detail page (disabling or uninstalling removes registrations immediately).
- **Large Code Blocks**: Very long lines fall back to basic highlighting, and oversized code blocks render as plain text to keep editing responsive.
- **OS "Open With" Registration**: Enabled extension file suffixes automatically sync to macOS, Windows, and Linux "Open With" menus (`filenames`-only rules are not registered).

### 8.1 `contributes.languages` Fields

Array of up to **16** language entries per plugin:

| Field | Required | Type & Constraints |
| --- | --- | --- |
| `id` | Yes | String matching `^[a-z][a-z0-9_-]{0,31}$`, unique per plugin |
| `name` | Yes | `LocalizedText` (`zh` and `en` non-empty, no Emoji) |
| `extensions` | At least one of `extensions` / `filenames` | `<= 16` unique strings matching `^\.[a-z0-9][a-z0-9._-]{0,23}$` (longest suffix wins, e.g. `.d.ts` over `.ts`) |
| `filenames` | At least one of `extensions` / `filenames` | `<= 16` unique bare filenames (`<= 64` bytes, no `/ \ :`, not `.` or `..`) |
| `aliases` | No | `<= 16` unique strings matching `^[a-z0-9][a-z0-9._+-]{0,31}$` (fenced code block tags) |
| `display_tags` | No | `<= 8` non-empty strings (`<= 24` bytes, no Emoji); falls back to `filenames` then `extensions` (Store index keeps up to 6 deduplicated tags) |
| `grammar` | Yes | Relative `/` path inside plugin dir ending in `.json` (no leading `/`, no `\`, `:`, or `..`; max **256 KiB**) |

### 8.2 `grammars/*.json` Schema (Stateful RE2 Line Tokenizer)

Strictly decoded: **no duplicate keys, no unknown fields**.

#### Top-Level Fields

| Field | Required | Type & Constraints |
| --- | --- | --- |
| `schema_version` | Yes | Fixed integer `1` |
| `line_comment` | No | Single-line comment prefix (e.g. `"//"`, `"#"`) |
| `block_comment` | No | 2-element `[open, close]` array |
| `word_chars` | No | Extra identifier characters |
| `tab_size` | No | Integer `0`–`16` (`0` inherits editor default) |
| `hard_tabs` | No | Boolean (default `false`) |
| `brackets` | No | Array of `[open, close]` pairs for bracket matching |
| `auto_close` | No | Array of `[open, close]` pairs for auto-closing |
| `initial_state` | No | Defaults to `"root"`; must exist in `states` |
| `states` | Yes | Map of 1–63 states (`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`), `<= 128` rules/state, `<= 512` rules total; max state stack depth **9** |
| `sample` | No | Preview snippet for Settings / Store detail view |

#### `GrammarRule` Fields

| Field | Required | Type & Constraints |
| --- | --- | --- |
| `match` | Yes | Non-empty RE2 regex anchored automatically as `\A(?:...)` at the current scan offset |
| `line_start` | No | Boolean (default `false`); when `true`, matches only at column 0 (`pos == 0`) |
| `token` | No | One of the 25 valid token names (case-insensitive) |
| `captures` | No | Map of capture group `"1"`–`"16"` to valid token name |
| `next` | No | Replace top state with target state |
| `push` | No | Push target state onto stack |
| `pop` | No | Boolean; pop top state (falls back to `initial_state` if empty) |

Rule constraints: at least one of `token`, `captures`, `next`, `push`, `pop`; at most one of `next`, `push`, `pop`; zero-length matches require a state transition and are capped at **8** consecutive zero-width steps.

Scanner behavior:
- Each step matches `\A(?:match)` against `line[pos:]`, so `^` and `\b` treat `pos` as start-of-slice. Place a general identifier rule (e.g. `[A-Za-z_][A-Za-z0-9_]*`) after keyword rules so suffixes of identifiers are not mis-tokenized.
- If no rule matches, the scanner consumes **1 UTF-8 rune** as `text`.
- When `pos == len(line)` the loop exits immediately, and **empty lines return without running rules**. Zero-width `$` pop rules do not run at end-of-line, and pushed states persist across empty lines. Consume single-line constructs within one rule without pushing a multi-line state; reserve `push`/`pop` for genuine multi-line blocks (e.g. block comments or multi-line strings).

#### Valid Token Names (25)

- Identifiers & basics: `text`, `keyword`, `type`, `function`, `variable`, `property`, `constant`
- Literals & comments: `number`, `string`, `escape`, `regexp`, `comment`
- Syntax & markup: `operator`, `punctuation`, `tag`, `attribute`, `meta`
- Prose & diff: `heading`, `emphasis`, `strong`, `link`, `code`, `inserted`, `deleted`, `invalid`

### 8.3 Minimal Copyable Example

Directory layout:
```text
example-lang/
├── manifest.json
├── README.md
├── README.en.md
└── grammars/
    └── conf.json
```

`manifest.json`:
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

`grammars/conf.json`:
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

### 8.4 Host File Association Reference

Built-in languages and enabled language extensions sync automatically to OS "Open With" menus via `cmd/block-fileassoc`:
- **macOS**: generates main `Info.plist` entries at build time and maintains an install-path-scoped helper app (`dev.eachann.block.associations.<hash>`) at runtime.
- **Windows**: registers `OpenWithProgids` (`Block.Machine.Document`, `Block.User.Document`, `Block.Machine.User.Document`, or `Block.Portable.<hash>`) without hijacking existing default apps.
- **Linux**: registers install-path-scoped `NoDisplay=true` desktop entries (`--sync-user-linux-file-associations` on `.deb` `postinst`, script install, or AppImage first run) while pinning existing non-Block default handlers.

<!-- ai-guide:skip-start -->
---

Next: [6. Check & Store →](check-and-store.md)
<!-- ai-guide:skip-end -->
