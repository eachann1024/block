<!-- ai-guide:skip-start -->
# Directory Layout & `manifest.json` Specification

> [← Overview & AI Guide](README.md) · [Docs Home](../README.md) · [Protocol, UI & SDK →](protocol-and-ui.md)

---
<!-- ai-guide:skip-end -->

## 3. Directory Layout & `manifest.json` Specification

The plugin root must contain `manifest.json` and its entry file (pure declarative language extensions omit the entry file and provide `grammars/*.json` instead). Official plugins (`blockmd-plugins/*`) must provide both `README.md` (Chinese) and `README.en.md` (English), and interactive plugins must support `locale="en"`; community plugins are encouraged to do the same:

```text
example-sync/
├── manifest.json
├── README.md
├── README.en.md
└── main.js
```

### Complete `manifest.json` Example (replace `id` and `repo`; no unknown fields)

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
    "js": "main.js"
  },
  "permissions": [
    "notes:read",
    "notes:write",
    "network",
    "events:note_saved"
  ],
  "network_domains": [
    "dav.example.com"
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

### Field Constraints

#### Required & Core Architecture Fields
- `app`: required literal **`"blockmd"`**.
- `schema_version`: required integer `1`.
- `id`: required, matches `^[a-z0-9][a-z0-9-]{1,47}$` (`block-diagnostics` is reserved).
- `version`: required, must match both `^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$` and valid SemVer (`golang.org/x/mod/semver`, shared with Store update comparison; e.g. `"1.0.0"`, `"1.0.0-beta.1"`; no `+build` metadata, no leading zeros or empty prerelease identifiers).
- `name`: both `zh` and `en` required non-empty; no Emoji.
- `author`: required non-empty string; no Emoji.
- `summary`: `zh` (`<= 80` runes) and `en` (`<= 140` runes) both required; no Emoji. Write plain-language user-facing copy describing what problem the plugin solves; omit internal implementation details and defensive disclaimers.
- `category`: optional string (**must be `"language"`** when declaring `contributes.languages`). The App Store shows 5 category tabs in fixed order:
  1. `"appearance"`: themes, fonts, icons, layout
  2. `"edit"`: syntax helpers, templates, images, math, diagrams inside a note
  3. `"organize"`: tags, backlinks, search, outlines, archiving across notes
  4. `"ai"`: model-powered rewriting, summarization, Q&A, generation
  5. `"tools"`: sync, backup, import/export, publishing, system integration, external services
  - `"language"` (Code Highlighting) has no dedicated tab and appears in search results or when installed.
  - Retired `"sync"` and `"publish"` still validate and map to `"tools"`, but must not be used in new plugins. Omitting `category` defaults to `"tools"` and emits a `[WARN]`; official plugins (`blockmd-plugins/*` / `verified.json`) must declare a current category explicitly.
- `entry`: omitted only for pure declarative language extensions (which must also keep `permissions: []` and declare no `contributes` fields other than `languages`). Interactive plugins must specify exactly one of `entry.js` or `entry.wasm` (`entry.command` is rejected, including `"command": null`). Paths must be relative inside the plugin directory with no leading/trailing whitespace, no `..` substring anywhere (e.g. `build..prod.js` is rejected), no `\` or `:`, and no symlinks. Manifest decoding validates path syntax, installation checks regular file and size limits, and runtime compiles Wasm or loads JS:
  - `entry.js`: `.js` or `.mjs` loaded as an ES module in built-in `quickjs-ng` with `/runtime/block-sdk.js` pre-injected.
  - `entry.wasm`: `.wasm` WASI Preview 1 module (32 MiB linear memory limit) for compute-heavy workloads.
- `surfaces`: optional array. Declare `["settings_page"]` for a dedicated Settings page (other surfaces derive automatically from `contributes`; see [Host UI Contributions](ui-contributions.md)). Omit or use `[]` when no dedicated settings page is needed.
- `permissions`: optional array of unique strings. **30 valid permissions** exist across 3 tiers:
  - **5 Sensitive Groups (15 permissions; trigger user confirmation on install or escalation)**:
    1. **Notes & Editor**: `"notes:read"`, `"notes:write"`, `"notes:path"` (host absolute path in `plugin.command` `context.file` without reading content), `"editor:read"`, `"editor:write"`
    2. **System Credentials**: `"system:secrets"` (per-plugin isolated OS keychain/credential store)
    3. **Clipboard**: `"system:clipboard_read"`, `"system:clipboard_write"`
    4. **System Files & External Actions**: `"system:trash"`, `"system:open"` (open links or reveal notebook, Downloads, and granted files), `"system:dialogs"` (open file, folder, or save-location pickers), `"system:fs"` (`host.fs.*` within `data_dir`, `downloads_dir`, authorized notebooks, and granted paths), `"system:exec"` (shell-free `host.exec.run` for `exec_allowlist` binaries), `"ai:complete"` (`host.ai.*`)
    5. **Network**: `"network"` (mandatory for `host.http.request`; pair with `network_domains` to restrict target hosts)
  - **Silent Event Subscriptions (3 permissions; no confirmation prompt)**:
    - `"events:note_saved"`: note save/delete events via `plugin.event`
    - `"events:system"`: wake, network change, app focus, `notebook.renamed` / `notebook.removed` lifecycle events (requires note permissions), and `host.scheduler.*` ticks
    - `"events:workspace"`: `note.opened`, `theme.changed`, `locale.changed` (delivered only to already-running sandboxes)
  - **Default-allowed permissions (12)** include `"storage"`, the nine `"ui:*"` permissions (`ui:titlebar`, `ui:settings`, `ui:commands`, `ui:panels`, `ui:status`, `ui:notify`, `ui:sidebar`, `ui:appearance`, `ui:menubar`), `"system:notify"`, and `"system:badge"`. Some extensions still require explicit declarations. `titlebar_manager` requires `"ui:titlebar"`. `block_decorators` requires `"ui:appearance"` or `"editor:write"`. `themes` requires `"ui:appearance"`. `stages` requires `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"`. Both `contributes.ignored_dirs` and `host.sidebar.*_ignored_dirs` require `"ui:sidebar"` without prompting for consent. When removing a redundant permission, also remove its key from `permission_reasons`. Pure declarative language extensions require `permissions: []`.
- `network_domains`: optional array (`<= 64` entries, case-insensitive deduplication) restricting `host.http.request` targets and redirects. Accepts exact hosts (`"api.example.com"`, any port), explicit host:port (`"127.0.0.1:8080"`, matches explicit URL port), or single-label wildcard subdomains (`"*.example.com"`, subdomains only, not apex). Schemes, paths, and whitespace are forbidden. Regardless of whether `network_domains` is set, runtime SSRF guards block unspecified, link-local, and `0.0.0.0/8` addresses; when `network_domains` is configured, non-literal/non-`localhost` domains resolving to private or loopback IPs are also blocked.
- `exec_allowlist`: optional array (`<= 8` entries, case-sensitive deduplication and runtime matching). `host.exec.run` resolves bare executable names from host `PATH` (e.g. `"git"`, `"rclone"`). Names must match `^[a-zA-Z0-9._-]+$` and reject `.`, `..`, path separators, and forbidden interpreters (checked in lowercase after stripping `.exe`, `.cmd`, `.bat`, `.com`): `sh`, `bash`, `zsh`, `dash`, `fish`, `cmd`, `powershell`, `pwsh`, `python`, `pythonw`, `py`, `pyw`, `node`, `perl`, `ruby`, `osascript`, plus versioned Python names matching `^(pythonw?|pyw?)[0-9]+(\.[0-9]+)*$`.
- `contributes`: optional object for host UI extensions and declarative language grammars; see [Host UI Contributions](ui-contributions.md) and [Language Extensions](languages.md).
  - `contributes.ignored_dirs` is an array of directory names such as `[".generated"]`. See [Host UI Contributions](ui-contributions.md) for validation and scope.
- `icon` is the Ocean Paper two-tone vector icon (`view_box: "0 0 24 24"`, at least one `stroke_paths` entry, optional `fill_paths` and `accent_paths`). For optional artwork, see the [App Store guide](check-and-store.md).

#### Optional Metadata Fields
- `repo`: optional GitHub slug `"owner/repo"` (`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`). When published to the Store, must match the actual repository (case-insensitive).
- `permission_reasons`: optional `map<Permission, LocalizedText>` explaining why each sensitive permission is requested. Every key must also appear in `manifest.permissions`. Values require non-empty `zh` or `en` (`en` required under `-bilingual`), `zh <= 80` / `en <= 140` runes, zero Emoji. Shown in permission confirmation cards and Store detail pills.
- `beta`: optional boolean (default `false`; omit for stable releases). Use only when both (1) UX/storage schema may still change incompatibly and (2) host capabilities or real-device verification are incomplete. Do not use `beta` for newly published stable plugins or data-critical sync plugins.

<!-- ai-guide:skip-start -->
---

Next: [3. Protocol, UI & SDK →](protocol-and-ui.md)
<!-- ai-guide:skip-end -->
