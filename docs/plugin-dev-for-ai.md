# Block App Store Plugin Developer Guide (Self-Contained AI Spec v1 · `blockmd`)

> Generated automatically by `bun run docs:sync` from [`docs/plugins/`](https://github.com/eachann1024/block/tree/main/docs/plugins) and kept in sync with the documentation site at [docs.block.cooool.top/plugins/](https://docs.block.cooool.top/plugins/). Chinese version: [zh/plugin-dev-for-ai.md](https://docs.block.cooool.top/zh/plugin-dev-for-ai.md).

This document is the authoritative self-contained specification for building, validating, and publishing **Block** App Store plugins. Developers and AI coding assistants can build an installable plugin with native Ocean Paper settings UI that passes `block-plugin-check` from this file alone.

---

## 1. Before You Start (AI Execution Guide)

External plugins **require no PR to the main repository and no manual approval**. Host your plugin in a standalone public, non-fork, unarchived GitHub repository with the `blockmd-plugin` topic to participate in automatic store crawling (top 100 repositories sorted by stars; publishing a non-draft, non-prerelease GitHub Release is recommended, and the crawler falls back to a commit-pinned default-branch tarball when no suitable Release exists—see [Check & Store](https://docs.block.cooool.top/plugins/check-and-store)). At any stage, desktop users can also install directly via local folder, `owner/repo`, or HTTPS archive URL.

### 1.1 Choose Your Path (3 Development Paths)

| Path | Use Case | Deliverables | Notes |
| --- | --- | --- | --- |
| Interactive / Community Plugin | Sync, image hosting, export, AI panels, host UI extensions | Standalone repo + `manifest.json` + `entry.js` (built-in `wazero` + `quickjs-ng` sandbox; `entry.wasm` for compute-heavy work) | Entry point requires no user-installed runtime (only explicit `host.exec.run` calls depend on allowlisted host binaries); add `blockmd-plugin` topic for automatic crawling |
| Declarative Language Extension | Syntax highlighting for additional languages in code blocks and source editor | Standalone repo + `manifest.json` (`category: "language"`) + `grammars/*.json` | Zero process, zero permissions, auto-enabled on install; see [Language Extensions](https://docs.block.cooool.top/plugins/languages) |
| Officially Verified (`blockmd-plugins/*`) | Officially maintained plugins bound 1:1 in `store/verified.json` | Standalone repo + `manifest.json` + entry or grammars | Requires bilingual `README.md` / `README.en.md` and `locale="en"` UI gate |

### 1.2 Eight Hard Rules to Avoid Rejection

1. **Never submit plugins via PR to the main repo or under `plugins/`**: all external plugins live in independent repositories.
2. **Never draw your own enable/disable switch, uninstall button, or bypass host UI**: use declarative `UIPage` JSON for settings and extensions—the host renders the enable toggle, titlebar controls, and uninstall menu. When a custom interactive canvas is required, use an offline `block-plugin://` WebView via `contributes.stages` (no external network resources; see [Host UI Contributions](https://docs.block.cooool.top/plugins/ui-contributions)).
3. **Zero Emoji in static UI copy**: manifests or static `UIPage` labels containing Emoji are rejected on load. Icons must use `manifest.icon` two-tone SVG paths (`view_box: "0 0 24 24"`, at least 1 `stroke_paths` entry).
4. **Never invent unknown fields or invalid values**: `manifest.json` (`<= 256 KiB`, no duplicate keys) and `UIPage` use strict decoding (`DisallowUnknownFields`). `id` must match `^[a-z0-9][a-z0-9-]{1,47}$` and must not be `block-diagnostics`; `surfaces` allows only `["settings_page"]` or `[]`; `category` must be one of the 5 display categories (`"language"` is required for declarative language packs; retired `"sync"` / `"publish"` should not be used).
5. **Interactive plugins must use the built-in sandbox**: declare `entry.js` or `entry.wasm` (see [Manifest & Permissions](https://docs.block.cooool.top/plugins/manifest)). Write only newline-delimited JSON-RPC 2.0 to `stdout` and flush logs to `stderr`. `entry.js` is loaded as an ES module and `serve` / `callHost` are synchronous—do not pass an `async` handler returning a Promise.
6. **Access host paths only through host RPCs**: the sandbox mounts only read-only `/plugin` and `/runtime` and cannot open host absolute paths directly. Use `host.storage.*` for private state, and `host.fs.*` / `host.notes.*` with `data_dir`, `downloads_dir`, or authorized paths.
7. **Request minimal permissions and obey shutdown**: declare only needed sensitive permissions across the 5 groups (15 total), omit unnecessary default-allowed permissions (keeping explicit `ui:*` where still required by `titlebar_manager`, `block_decorators`, `themes`, or `stages`, and removing matching `permission_reasons` keys when removing a permission). Exit cleanly within 2s on `plugin.shutdown` without calling `host.*`.
8. **Keep initial actions safe under empty/default config**: `block-plugin-check` renders with empty and default values, toggles individual controls, and invokes every top-level `header_actions` and `actions` button regardless of `visible_when`. Return `{"ok": false, "status": "..."}` cleanly when configuration is incomplete.

### 1.3 Standard Workflow

1. **Read the matching chapters before coding**: interactive plugins read [Manifest & Permissions](https://docs.block.cooool.top/plugins/manifest) + [Protocol, UI & SDK](https://docs.block.cooool.top/plugins/protocol-and-ui) (look up RPCs in the [Host Method Index](https://docs.block.cooool.top/plugins/protocol-and-ui#host-method-index), plus [Host UI Contributions](https://docs.block.cooool.top/plugins/ui-contributions) if extending host chrome); language extensions read [Manifest & Permissions](https://docs.block.cooool.top/plugins/manifest) + [Language Extensions](https://docs.block.cooool.top/plugins/languages).
2. **Create a standalone repository**: include `manifest.json` and `README.md` at the root (official and bilingual plugins also include English `README.en.md`).
3. **Implement core logic and declarative UI**: start interactive plugins from the locale-aware `entry.js` + `/runtime/block-sdk.js` skeleton in [Protocol, UI & SDK](https://docs.block.cooool.top/plugins/protocol-and-ui); language extensions only need `manifest.json` and `grammars/*.json`.
4. **Complete verification (1.4)**: static checks → `block-plugin-check` `[PASS]` → desktop verification. If Go or the desktop app is unavailable in your environment, report unexecuted steps explicitly instead of fabricating pass results.
5. **Publish**: push to a public, non-fork, unarchived GitHub repo, verify `manifest.repo`, add the `blockmd-plugin` topic, and publish a standard GitHub Release.

### 1.4 Verification Checklist

**Step 1: Static & Package Self-Check**

- [ ] `manifest.json` is a single valid JSON object (`<= 256 KiB`, no duplicate keys, no unknown fields) with `app: "blockmd"`, valid `id` (not `block-diagnostics`), valid `category`, `surfaces` in `["settings_page"]` or `[]`, and `icon` with `view_box: "0 0 24 24"` and `>= 1` `stroke_paths`.
- [ ] Zero Emoji across `manifest.json` and static `UIPage` fields.
- [ ] Only needed sensitive permissions declared with clear `permission_reasons`; unnecessary default-allowed permissions removed cleanly.
- [ ] Interactive entry uses `entry.js` or `entry.wasm` (relative path, no symlinks, no `..` substring); `stdout` emits line-delimited JSON-RPC 2.0; logs flush to `stderr`; file/network/exec boundaries respect `host.fs.*`, `network_domains`, and `exec_allowlist`.
- [ ] Root `README.md` present (plus English `README.en.md` with valid assets for official or bilingual plugins).

**Step 2: Automated Protocol Check (`block-plugin-check`)**

- [ ] Run against the main repository checkout (clone shallowly if needed; pass `-bilingual` before the plugin path for official or bilingual plugins):
  ```bash
  git clone --depth 1 https://github.com/eachann1024/block.git /tmp/block
  CGO_ENABLED=0 go -C /tmp/block run ./cmd/block-plugin-check /abs/path/to/plugin
  # With bilingual gate (auto-enabled for blockmd-plugins/* repos):
  CGO_ENABLED=0 go -C /tmp/block run ./cmd/block-plugin-check -bilingual /abs/path/to/plugin
  ```
- [ ] Exit code `0` with `[PASS] Plugin "<id>" passed all Block App Store checks.`; review any `[WARN]` lines (keep required `ui:*` permissions for `titlebar_manager`, `block_decorators`, `themes`, and `stages`).

**Step 3: Desktop Verification**

- [ ] Install via **Settings → App Store → Local Folder** and verify the sensitive permission confirmation card.
- [ ] Verify lifecycle: interactive plugins install disabled and start when toggled on; pure language packs auto-enable on install.
- [ ] Verify areas outside CLI coverage: `plugin.command`, host chrome extensions, light/dark themes, `zh`/`en` locale switching, multi-notebook switching, save/delete events, dirty-editor protection, and Store `README` rendering.
- [ ] Verify reload, disable, and uninstall cleanly tear down the sandbox.

**Step 4: Release & Store Verification**

- [ ] Repository is public, non-fork, unarchived, has topic `blockmd-plugin`, and matches `manifest.repo`.
- [ ] Standard (non-draft, non-prerelease) GitHub Release published with `manifest.json` at archive root or single top-level directory and version matching the release.
- [ ] Remote installation via `owner/repo` verified in the desktop app.

---

## 2. Architecture & Host Hard Boundaries

### 2.1 Sandbox Runtime, Environment Variables & Strict Validation

- **Built-in Sandbox**: Block executes WASI Preview 1 modules on `wazero` (512 pages / 32 MiB linear memory limit) and mounts only read-only `/plugin` (plugin directory) and `/runtime` (built-in SDK). Host directories are never mounted directly into the sandbox.
  - **`entry.js`**: loaded as an ES module inside embedded `quickjs-ng` (16 MiB default heap, 1 MiB stack) with `/runtime/block-sdk.js` pre-injected. `serve` and `callHost` are synchronous—do not pass an `async` handler (returned Promises serialize as empty objects).
  - **`entry.wasm`**: runs directly under WASI Preview 1 and handles newline-delimited UTF-8 JSON-RPC 2.0 over `stdin` / `stdout`.
  - The host retains up to the last 64 KiB of `stderr` for diagnostics. Switching notebooks or locales re-initializes the sandbox with the updated context on demand (open surfaces and certain appearance/UI plugins re-render or warm immediately).
- **Environment Variable Allowlist**: `BLOCK_PLUGIN_ID`, `BLOCK_DATA_DIR`, `BLOCKMD_PLUGIN_ID`, `BLOCKMD_DATA_DIR`, and `BLOCKMD_DOWNLOADS_DIR`. These hold host absolute paths; pass them to `host.fs.*` rather than opening them directly inside WASI.
- **Two Mandatory Validation Rules (violations fail load immediately)**:
  1. **No Emoji**: static strings in `manifest.json` (`name`, `author`, `summary`, `permission_reasons`, `contributes`) and static `UIPage` copy (`page.title`, `card.title`, control/button `label`, `placeholder`, `empty_text`, `kv.rows[].label`, `metrics[].label`, `checklist.options[].badge`, `treemap` tile `label`/`detail`, `summary.total_label`, `preview.badge`, `cards[].badge`) must not contain Emoji (standard/extended ranges, variation selectors `U+FE00–U+FE0F`, regional indicators, tag sequences, ZWJ/keycap sequences, and common symbols such as `⭐`/`⌚`/`‼`). Dynamic user data fields allow existing user characters, though plugin-authored text should still avoid Emoji.
  2. **Strict JSON Decoding**: `manifest.json` (`<= 256 KiB`, single JSON object, no duplicate keys) and `UIPage` enforce `DisallowUnknownFields`.

### 2.2 Timeouts & Size Limits

- **Execution Timeouts & Termination**:
  - Initial render (15s) and edit-preview render (8s) start after acquiring the plugin RPC lock; periodic polling (5s) includes lock wait. Each budget covers both initialization and render.
  - `plugin.command`: 30s including lock wait and on-demand initialization. `events:workspace` (`note.opened`, `theme.changed`, `locale.changed`): 5s, delivered only to already-running sandboxes.
  - Page `plugin.action`: queue wait is cancelable; 120s budget starts after acquiring the RPC lock (covering on-demand initialization and pre-action form refresh; titlebar shows a `■` cancel button). Toast-triggered `surface: "toast"` actions use a separate 30s budget including lock wait.
  - Note save/delete events share 45s after acquiring the RPC lock (15s init + 120s event budget when declaring `network` or `system:fs`). System events get 15s init + 45s event budget after lock acquisition (120s event budget when declaring `network`, `system:fs`, or `system:exec`).
  - Host response write-back timeout: 5s after the host method completes.
  - `plugin.shutdown`: 2s graceful exit, followed by forced termination and up to 1s cleanup wait.
  - `block-plugin-check`: 15s for `plugin.initialize`, 20s total per locale pass.
- **Size & Package Limits (Host vs. Sandbox Memory)**:
  - Plugin `stdout` single-line JSON-RPC scanner limit: **98 MiB** (covers a 16 MiB note under worst-case 6x JSON escaping; `stdin` from host has no line scanner cap).
  - Concurrent in-flight `host.*` requests: **8** max (exceeding aborts the sandbox; notification-only `host.action.progress` without `id` does not consume a slot, whereas requests with `id` do).
  - Private `state.json` serialized size: **8 MiB** max.
  - Note read / `host.notes.write`: **16 MiB** per file; `host.editor.insert`: **4 MiB** per call.
  - The **64 MiB** cap on `host.fs.read`/`write` and `host.http.request` is a host-side limit. Default `entry.js` runs with a **16 MiB** QuickJS heap (which must also hold JSON and Base64 copies), so use host-side streaming (`host.fs.copy`, `host.http.request` `body_file_path` / `save_to_path`) for large files instead of buffering them in JS memory.
  - Package archive download `<= 64 MiB`, single file `<= 32 MiB`, unpacked/copied total `<= 256 MiB` (tar stream `<= 320 MiB`), entries/paths `<= 8192`, path length `<= 1024` bytes and `< 32` depth, no `\` or `:`, regular files and directories only (no symlinks).

### 2.3 Native Settings Integration, Store Display & README Rendering

- **Sidebar & Header Controls**: Plugins declaring `settings_page` appear under **Settings → Installed** with a live status indicator. Plugins without a dedicated settings page are managed and uninstalled directly from their Store detail view. The host renders the Ocean Paper Pluck Switch, single-line status, primary action / live progress pill, and `⋯` menu (secondary actions, View Details, Reload, Uninstall).
- **Categories, Filtering & Search**: The Store displays 5 category tabs (Appearance / Editing / Organize / AI / Tools). Language extensions appear in search results or when installed. Supports bilingual fuzzy search, install-state and source filters, and automatic jsDelivr CDN fallback.
- **Three Trust Tiers**: `Official` (`blockmd-plugins/*` bound 1:1 in `verified.json`), `Reviewed` (`eachann1024/*`), and `Unreviewed` (community repositories with a review notice).
- **Permission Pills**: Store detail pages display the 5 sensitive permission groups (default-allowed capabilities and silent events are omitted). Hovering a pill shows custom `manifest.permission_reasons` first, falling back to built-in bilingual descriptions.
- **In-App README Rendering**: Read-only inline Markdown preview (`<= 512 KiB` UTF-8) supporting GitHub `<picture>` dark/light images, relative images, SVG, GIF, and bounded video (max 24 media items, max 2 videos, image `<= 6 MiB`, video `<= 24 MiB`). Lookup checks local root files by locale (EN: `README.en.md` → `README.md` ...; ZH: `README.zh.md` → `README.md` ...), then GitHub API, then jsDelivr CDN.

### 2.4 Storage Paths, Safe Replacement, Deep Links & Reserved ID

- **Paths**:
  - Install directory: `<BlockAppData>/apps/<plugin-id>/`
  - Private storage: `<BlockAppData>/apps-data/<plugin-id>/state.json` (disabled state and catalog cache live in `<BlockAppData>/apps-cache/`)
- **Lifecycle & Safe Replacement**: Newly installed interactive plugins start disabled; declarative language packs auto-enable; same-`id` upgrades preserve enabled state; manual same-`id` installation does not require a higher version. Installing sensitive permissions or adding new ones on update prompts a permission confirmation card ("Update All" updates non-escalating plugins first and queues escalating updates for individual confirmation). Replacements stop the running sandbox and swap directories via `.previous` backup; uninstall removes program files while retaining `apps-data/<plugin-id>/`.
- **External Deep Links** (`block://` / `blockmd://` `store` or `install`): selects the first non-empty query parameter in `repo` → `url` → `id` order and matches strictly against catalog `id`, `repo`, or exact `download_url` without auto-installing.
- **Reserved ID**: `block-diagnostics` is reserved for Block's built-in Diagnostics window and is rejected/filtered everywhere.

### 2.5 Runtime Transition & Migration from External Commands

Block introduced embedded sandbox entries (`entry.js` / `entry.wasm`) in `v1.0.947` and retired external process entries (`entry.command`) in `v1.0.954`. Moving interactive plugins onto the built-in `wazero` / `quickjs-ng` runtime removes the requirement for users to pre-install external interpreters (such as Python) and enforces filesystem, network, and subprocess permissions uniformly inside the host.

- **New Installs & Validation**: fresh installs, re-imports, and `block-plugin-check` reject any manifest containing `entry.command` (including `"command": null` or manifests declaring both `entry.js` and `entry.command`).
- **Behavior for Existing Retired Installs**: after upgrading Block, already-installed plugins that declare only `entry.command` (with no `js`/`wasm` entry and otherwise compatible metadata; strict `exec_allowlist` checks are skipped for retired installs so they remain discoverable) stay visible with status **Update required**:
  - Execution is suspended, manual enabling is blocked, and the titlebar toggle is hidden;
  - Private configuration under `<BlockAppData>/apps-data/<plugin-id>/` and enablement preferences are preserved;
  - When the Store catalog offers a download URL with a strictly higher SemVer version, users can click **Update** to upgrade in place; manual installation of a same-`id` sandboxed package also upgrades in place.
- **Developer Migration Checklist**:
  1. Keep the same `manifest.id` and publish a strictly higher `manifest.version` so existing users can update in place and retain their data.
  2. Replace `entry.command` with `entry.js` (loaded as an ES module with synchronous `serve` / `callHost` from `/runtime/block-sdk.js`) or `entry.wasm` (WASI Preview 1 with stdio JSON-RPC).
  3. Migrate direct host file I/O, HTTP requests, and subprocess calls to `host.fs.*` (`system:fs`), `host.http.request` (`network` + `network_domains`), and `host.exec.run` (`system:exec` + `exec_allowlist`).
  4. Review any newly required sensitive permissions and add `permission_reasons`. Note that `host.secrets.*` isolates credentials by installation provenance, so keep the same Store distribution source.

---

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
- `surfaces`: optional array. Declare `["settings_page"]` for a dedicated Settings page (other surfaces derive automatically from `contributes`; see [Host UI Contributions](https://docs.block.cooool.top/plugins/ui-contributions)). Omit or use `[]` when no dedicated settings page is needed.
- `permissions`: optional array of unique strings. **30 valid permissions** exist across 3 tiers:
  - **5 Sensitive Groups (15 permissions; trigger user confirmation on install or escalation)**:
    1. **Notes & Editor**: `"notes:read"`, `"notes:write"`, `"notes:path"` (host absolute path in `plugin.command` `context.file` without reading content), `"editor:read"`, `"editor:write"`
    2. **System Credentials**: `"system:secrets"` (per-plugin isolated OS keychain/credential store)
    3. **Clipboard**: `"system:clipboard_read"`, `"system:clipboard_write"`
    4. **System Files & External Actions**: `"system:trash"`, `"system:open"`, `"system:dialogs"`, `"system:fs"` (`host.fs.*` within `data_dir`, `downloads_dir`, authorized notebooks, and granted paths), `"system:exec"` (shell-free `host.exec.run` for `exec_allowlist` binaries), `"ai:complete"` (`host.ai.*`)
    5. **Network**: `"network"` (mandatory for `host.http.request`; pair with `network_domains` to restrict target hosts)
  - **Silent Event Subscriptions (3 permissions; no confirmation prompt)**:
    - `"events:note_saved"`: note save/delete events via `plugin.event`
    - `"events:system"`: wake, network change, app focus, `notebook.renamed` / `notebook.removed` lifecycle events (requires note permissions), and `host.scheduler.*` ticks
    - `"events:workspace"`: `note.opened`, `theme.changed`, `locale.changed` (delivered only to already-running sandboxes)
  - **Default-allowed permissions (12)** include `"storage"`, the nine `"ui:*"` permissions (`ui:titlebar`, `ui:settings`, `ui:commands`, `ui:panels`, `ui:status`, `ui:notify`, `ui:sidebar`, `ui:appearance`, `ui:menubar`), `"system:notify"`, and `"system:badge"`. Some extensions still require explicit declarations. `titlebar_manager` requires `"ui:titlebar"`. `block_decorators` requires `"ui:appearance"` or `"editor:write"`. `themes` requires `"ui:appearance"`. `stages` requires `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"`. Both `contributes.ignored_dirs` and `host.sidebar.*_ignored_dirs` require `"ui:sidebar"` without prompting for consent. When removing a redundant permission, also remove its key from `permission_reasons`. Pure declarative language extensions require `permissions: []`.
- `network_domains`: optional array (`<= 64` entries, case-insensitive deduplication) restricting `host.http.request` targets and redirects. Accepts exact hosts (`"api.example.com"`, any port), explicit host:port (`"127.0.0.1:8080"`, matches explicit URL port), or single-label wildcard subdomains (`"*.example.com"`, subdomains only, not apex). Schemes, paths, and whitespace are forbidden. Regardless of whether `network_domains` is set, runtime SSRF guards block unspecified, link-local, and `0.0.0.0/8` addresses; when `network_domains` is configured, non-literal/non-`localhost` domains resolving to private or loopback IPs are also blocked.
- `exec_allowlist`: optional array (`<= 8` entries, case-sensitive deduplication and runtime matching). `host.exec.run` resolves bare executable names from host `PATH` (e.g. `"git"`, `"rclone"`). Names must match `^[a-zA-Z0-9._-]+$` and reject `.`, `..`, path separators, and forbidden interpreters (checked in lowercase after stripping `.exe`, `.cmd`, `.bat`, `.com`): `sh`, `bash`, `zsh`, `dash`, `fish`, `cmd`, `powershell`, `pwsh`, `python`, `pythonw`, `py`, `pyw`, `node`, `perl`, `ruby`, `osascript`, plus versioned Python names matching `^(pythonw?|pyw?)[0-9]+(\.[0-9]+)*$`.
- `contributes`: optional object for host UI extensions and declarative language grammars; see [Host UI Contributions](https://docs.block.cooool.top/plugins/ui-contributions) and [Language Extensions](https://docs.block.cooool.top/plugins/languages).
  - `contributes.ignored_dirs` is an array of directory names such as `[".generated"]`. See [Host UI Contributions](https://docs.block.cooool.top/plugins/ui-contributions) for validation and scope.
- `icon` is the Ocean Paper two-tone vector icon (`view_box: "0 0 24 24"`, at least one `stroke_paths` entry, optional `fill_paths` and `accent_paths`). For optional artwork, see the [App Store guide](https://docs.block.cooool.top/plugins/check-and-store).

#### Optional Metadata Fields
- `repo`: optional GitHub slug `"owner/repo"` (`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`). When published to the Store, must match the actual repository (case-insensitive).
- `permission_reasons`: optional `map<Permission, LocalizedText>` explaining why each sensitive permission is requested. Every key must also appear in `manifest.permissions`. Values require non-empty `zh` or `en` (`en` required under `-bilingual`), `zh <= 80` / `en <= 140` runes, zero Emoji. Shown in permission confirmation cards and Store detail pills.
- `beta`: optional boolean (default `false`; omit for stable releases). Use only when both (1) UX/storage schema may still change incompatibly and (2) host capabilities or real-device verification are incomplete. Do not use `beta` for newly published stable plugins or data-critical sync plugins.

---

## 4. Bidirectional JSON-RPC 2.0 Protocol

All requests and responses are single-line UTF-8 JSON terminated by `\n`.

### 4.1 Host -> Plugin Requests

1. **`plugin.initialize`**
   - Request `params`:
     ```json
     {
       "plugin_id": "example-sync",
       "data_dir": "/abs/path/to/apps-data/example-sync",
       "downloads_dir": "/Users/name/Downloads",
       "notebook_root": "/abs/path/to/notebook",
       "notebooks": ["/abs/path/to/notebook", "/abs/path/to/other-notebook"],
       "locale": "en",
       "theme": "system",
       "host_version": "1.0.958",
       "ui_capabilities": [
         "kv_tooltip", "treemap", "checklist_add_folder", "action_feedback",
         "action_progress", "note_revision", "sidebar_sections", "open_settings", "form_autosave", "button_link"
       ]
     }
     ```
     - **Runtime Context**: `notebook_root` is populated only when declaring `notes:read`, `notes:write`, or `events:note_saved` and a notebook is active (otherwise `""`); `notebooks` is populated only when declaring `notes:read` (otherwise `[]`). Switching notebooks typically re-initializes the sandbox with the new `notebook_root` on the next call (including background `note.saved` events), while certain appearance/UI plugins may warm immediately on switch.
     - `downloads_dir` (defaults to `~/Downloads`, env `BLOCKMD_DOWNLOADS_DIR`): export files, backups, and user-facing output should default to `downloads_dir` rather than hidden directories inside `apps-data` or notebooks.
     - `ui_capabilities`: progressive capability flags (up to 40 keys depending on host version, config, and OS; see Section 4.4).
   - Return `result`: `{"ready": true}`.

2. **`plugin.render`**
   - Request `params`: e.g. `{"surface": "settings_page", "values": {"protocol": "webdav"}}`.
     - `values` is omitted on initial open, notebook/active-note/notebook-list reload, periodic polling, and post-`note.saved` refresh. Pre-action form refreshes may include unsynced `values` when present.
     - During interactive editing (discrete controls ~25ms debounce, text inputs ~220ms debounce), `values` contains only unsynced user edits; merge them with stored config to render previews and persist explicit form submissions in `plugin.action`.
   - Return `result`: required `page` (strictly decoded `UIPage`) and optional `values`.
     - Initial loads, notebook changes, edit-preview refreshes, and pre-action refreshes consume `result.values` and clear edited flags for those keys.
     - Periodic polling (`refresh_interval_ms`) and post-`note.saved` refreshes ignore `result.values`, updating only unedited control keys from `page` defaults.
     - During edit-preview refreshes, `page` defaults only fill keys not yet present in the form cache.

3. **`plugin.action`**
   - Triggered when clicking page buttons or toast notification action buttons (for toasts: `surface: "toast"`, `action_id: action.id`, `values: action.payload`). Note: `block-plugin-check` calls every top-level `header_actions` and `actions` button on the initial `UIPage` regardless of `visible_when` or collapsed state—ensure defaults never trigger destructive side effects.
   - **Autosave** sends the full form `values` and adds `"trigger": "autosave"` to `params`. The checker only calls button actions. Autosave uses `ok`, `tone`, `status`, `page`, and `values`, preserving the focused input and caret while merging other fields. Warnings and errors appear inline. Success and informational responses are quiet, with no toast or button flash.
   - Request `params`:
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
     `values` contains the host's cached form map (and may retain keys from hidden branches); read only active branch keys and persist via `host.storage.set`.
   - Return `result` (all fields optional; `page`, if returned, must be a complete valid `UIPage`):
     ```json
     {
       "ok": true,
       "tone": "success",
       "values": { "protocol": "local_mirror" },
       "status": "Synced 2 notes",
       "busy_label": "Syncing...",
       "success_label": "Synced",
       "open_path": "/Users/name/Downloads/Welcome.html",
       "reveal_path": "/Users/name/Downloads/Welcome.html"
     }
     ```
     - **Labels & Extra Fields**: optional `busy_label`, `success_label`, `error_label`, `retry_label` (no Emoji), plus optional `icon` and `synced_notebooks` (`string[]`). Write error or status text to `status` (the host does not read `message`).
     - **Merge Order**: if both `page` and `values` are returned, the host applies `page` control defaults first and `result.values` second. Button actions also fill empty `page.status` from `result.status`.
     - **Feedback (`ok` / `tone` / `status`)**: explicit valid `tone` (`"success"` | `"warning"`/`"warn"` | `"danger"`/`"error"` | `"info"`) wins, then `ok`, then keyword inference defaulting to `"success"`. `test_conn`, `test_*`, and `*_test` buttons stay in success state until the form changes or another action runs; other buttons show a green check for 2.4s.
     - **Output Files (`open_path` / `reveal_path`)**: paths are cleaned and verified to exist inside `downloads_dir` with no symlinks under `downloads_dir` (otherwise silently ignored). `open_path` blocks `.app` directories and allows only `.html`, `.htm`, `.pdf`, `.txt`, `.md`, `.markdown`, `.csv`, `.tsv`, `.png`, `.jpg`, `.jpeg`, `.webp`, `.gif`, `.docx`, `.xlsx`, `.pptx`; use `reveal_path` for other formats (e.g. `.zip`). Omit both fields on failed actions.

4. **`plugin.event`** (`"events:note_saved"` for note save/delete; `"events:system"` for system/scheduler; `"events:workspace"` for workspace)
   - Note save/delete: `{"event": "note.saved"|"note.deleted", "notebook_root": "...", "path": "welcome.md", "timestamp": "..."}`; folder deletes set `"path": ""` and `"folder": "notes/archive"`.
   - Delivered only to enabled subscribers (excluding the writing/deleting plugin itself and excluding raw source-editor saves). Host coalesces per `(plugin_id, notebook_root, path|dir:folder)`.
   - Return `{"handled": true}` / `{"handled": false}` / `{}` (non-object or wrong field types fail decoding). Return a JSON-RPC `error` for business failures so the host surfaces sidebar error/retry UI.

5. **`plugin.stage_action`** (requires `contributes.stages`)
   - Invoked by frontend `window.blockView.invoke(action, payload)` with `{"stage_id", "action", "payload"}`; return value resolves the frontend `Promise`.

6. **`plugin.shutdown`**
   - Request `params`: `{}`.
   - The host stops accepting reverse `host.*` calls before sending `plugin.shutdown`—do not call `host.storage.set` during shutdown. Return `{"ok": true}` and exit within 2 seconds.

---

### Host Method Index

This table lists all 80 `host.*` JSON-RPC methods currently dispatched by the host and their exact permission / manifest requirements. Capabilities marked "default-allowed" may be omitted from `manifest.permissions` unless an explicit declaration is noted; all other permissions must be declared explicitly.

<!-- host-methods-table:start -->
| Method | Required Permission / Manifest Condition | Purpose | Details |
| --- | --- | --- | --- |
| `host.storage.get` | `storage` (default-allowed) | Read one private storage key or the full key-value map | [§4.2 · 1](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.storage.set` | `storage` (default-allowed) | Write or overwrite a single private storage key | [§4.2 · 2](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.storage.set_many` | `storage` (default-allowed) | Atomically write 1–64 private storage keys at once | [§4.2 · 2](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notes.active` | `notes:read` | Get the active tab's relative note path | [§4.2 · 3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notebooks.list` | `notes:read` | List known notebook IDs, paths, and active state | [§4.2 · 4](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notebooks.data_dir` | `notes:read` | Resolve or create a per-notebook data directory under plugin `data_dir` | [§4.2 · 4](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notes.list` | `notes:read` | List visible Markdown notes inside a notebook | [§4.2 · 5](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notes.read` | `notes:read` | Read note Markdown content and SHA-256 `revision` | [§4.2 · 6](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notes.write` | `notes:write` | Write or update a note (supports `expected_revision` and `expected_root_identity`) | [§4.2 · 7](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notes.trash` | `notes:write` | Trash a notebook file by `expected_revision` (supports `expected_root_identity`) without prompt | [§4.2 · 8](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.notes.open` | `notes:read` (`-32005` in headless CLI) | Open a notebook note in the desktop editor | [§4.2 · 9](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.action.progress` | None (check `action_progress`; channel mode needs `progress_bar`) | Report live `plugin.action` progress and channel status | [§4.2 · 10](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.perf.snapshot` | `notes:read` (check `host_perf`) | Snapshot host CPU, RSS, and 7 disk categories | [§4.2 · 11](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.store.changelog` | None (check `store_changelog`) | Fetch App Store plugin release changelog | [§4.2 · 12](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.log` | None | Append structured diagnostic entries to `logs/block.log` | [§4.2 · 13](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.list_grants` | `system:fs` (notebook listing also needs `notes:read`/`notes:write`) | List granted directories, data/downloads dirs, and notebooks | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.stat` | `system:fs` | Inspect metadata and iCloud placeholder state of a granted path | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.list` | `system:fs` | List entries in a granted directory (optionally recursive) | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.read` | `system:fs` | Read a granted file as UTF-8 or Base64 | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.write` | `system:fs` | Write UTF-8 or Base64 data to a granted path (supports atomic) | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.mkdir` | `system:fs` | Create a directory under a granted path | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.delete` | `system:fs` | Delete a granted file or directory | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.delete_data` | `system:fs` | Delete a file or directory strictly under plugin `data_dir` without following symlinks or junctions | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.rename` | `system:fs` | Rename or move a granted file or directory | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.copy` | `system:fs` | Stream-copy a regular file on the host without Wasm buffering | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.fs.atomic_swap` | `system:fs` | Atomically exchange two granted files in the same directory | [§4.2 · 14](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.crypto.hash` | None (`system:fs` when `file_path` is passed) | Compute SHA/MD5 hash of text, Base64, or a granted file | [§4.2 · 15](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.crypto.hmac` | None | Compute SHA-1, SHA-256, or SHA-512 HMAC | [§4.2 · 15](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.http.request` | `network` (restricted by `network_domains`; file I/O needs `system:fs`) | Perform HTTP(S) request on the host with optional file streaming | [§4.2 · 16](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.exec.run` | `system:exec` + binary in `manifest.exec_allowlist` | Run an allowlisted executable without a shell | [§4.2 · 17](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.scheduler.register` | `events:system` | Register or update a periodic/wake/network/focus background job | [§4.2 · 18](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.scheduler.unregister` | `events:system` | Unregister a scheduled background job | [§4.2 · 18](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.scheduler.list` | `events:system` | List all registered background jobs for this plugin | [§4.2 · 18](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.ai.status` | `ai:complete` | Query host AI enabled, ready, and streaming flags | [§4.2 · 19](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.ai.complete` | `ai:complete` | Run a single-turn AI completion using the host model | [§4.2 · 19](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.ai.open_with_prompt` | `ai:complete` | Open the host AI panel prefilled with a prompt | [§4.2 · 19](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.sidebar.get_ignored_dirs` | **Explicit `ui:sidebar`** (check `sidebar_ignored_dirs`) | Read this plugin's runtime ignored directory names | [§4.2 · 20](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.sidebar.set_ignored_dirs` | **Explicit `ui:sidebar`** (check `sidebar_ignored_dirs`) | Replace or clear this plugin's runtime ignored directory names | [§4.2 · 20](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.capabilities` | None (check `system_integration`) | Query boolean availability of OS integration features | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.notify` | `system:notify` (default-allowed; note link needs `notes:read`) | Send a native OS notification banner | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.open_url` | `system:open` + active user action & window focus | Open an HTTP(S) URL in the default browser | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.reveal` | `system:open` + `notes:read` + user action & focus | Reveal a notebook file in the OS file manager | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.open_dialog` | `system:dialogs` + active user action & window focus | Show a native OS file or directory picker | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.save_dialog` | `system:dialogs` + active user action & window focus | Show a native OS save-location picker | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.trash` | `system:trash` + `notes:write` + user action & focus | Confirm natively and move a regular notebook file to Trash | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.system.badge` | `system:badge` (default-allowed) | Set or clear this plugin's app icon badge count | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.clipboard.read` | `system:clipboard_read` + user action & window focus | Read UTF-8 text from the OS clipboard | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.clipboard.write` | `system:clipboard_write` + user action & window focus | Write UTF-8 text to the OS clipboard | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.secrets.get` | `system:secrets` | Read a plugin-isolated credential from the OS keychain | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.secrets.set` | `system:secrets` | Store or update a plugin-isolated credential | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.secrets.delete` | `system:secrets` | Delete a stored credential from the OS keychain | [§4.3](https://docs.block.cooool.top/plugins/protocol-and-ui) |
| `host.titlebar.get_items` | `ui:titlebar` (default-allowed) | Get manageable titlebar slots (`group`/`preview`) and per-mode `groups` order | [§7.4 · 1](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.titlebar.set_hidden` | Runtime `ui:titlebar` (default-allowed) + `contributes.titlebar_manager: true` (manifest requires explicit `ui:titlebar`) | Persist hidden titlebar slot IDs (per-plugin; ignores `locked` and non-manageable slots) | [§7.4 · 2](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.titlebar.set_button` | `ui:titlebar` (default-allowed) + `contributes.titlebar_buttons` | Update badge text and active highlight of a titlebar button | [§7.4 · 3](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.menubar.set` | `ui:menubar` (default-allowed) + `contributes.menubar_items` | Update OS menu bar icon title, tooltip, and dynamic menu | [§7.4 · 12](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.status.set` | `ui:status` (default-allowed) + `contributes.status_items` | Update text, tooltip, and icon of a status bar item | [§7.4 · 4](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.toast` | None | Show an in-app top-right notification banner | [§7.4 · 5](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.open_settings` | None + `surfaces` includes `"settings_page"` (`open_settings`) | Open this plugin's dedicated Settings page | [§7.4 · 5.1](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.open_dialog` | `ui:notify` (default-allowed) + `contributes.dialogs` | Open a declared modal dialog and return `dialog_token` | [§7.4 · 6](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.close_dialog` | `ui:notify` (default-allowed) + `contributes.dialogs` | Close a declared modal dialog | [§7.4 · 6](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.open_panel` | `ui:panels` (default-allowed) + `contributes.panels` | Open a declared right-side panel | [§7.4 · 7](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.close_panel` | `ui:panels` (default-allowed) + `contributes.panels` | Close a declared right-side panel | [§7.4 · 7](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.toggle_panel` | `ui:panels` (default-allowed) + `contributes.panels` | Atomically toggle a side panel and return open state | [§7.4 · 7](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.refresh` | None + target `surface` must be declared by plugin | Asynchronously re-render an already-opened surface | [§7.4 · 8](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.set_background_error` | None | Register or update a background error indicator in Settings/titlebar | [§7.4 · 13](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.clear_background_error` | None | Clear one or all background errors for this plugin | [§7.4 · 13](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.backdrop` | `ui:appearance` (default-allowed) | Read persisted editor or window backdrop state | [§7.5](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.set_backdrop` | `ui:appearance` (default-allowed) + `contributes.backdrops` | Update and persist editor or window vector/paper backdrop state | [§7.5](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.theme_skin_state` | Runtime `ui:appearance` (default-allowed; callable without `contributes.themes`, returning `enabled: false`; declaring `themes` requires explicit `ui:appearance` in manifest) | Query active state of this plugin's theme skin | [§7.4 · 14](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.ui.set_theme_skin` | Runtime `ui:appearance` (default-allowed) + requires `contributes.themes` (declaring `themes` requires explicit `ui:appearance` in manifest) | Activate or deactivate a declared theme skin | [§7.4 · 14](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.stage.open` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed) + requires `contributes.stages` (declaring `stages` requires one explicitly in manifest) | Open an offline `block-plugin://` WebView stage | [§7.4 · 15](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.stage.close` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed; callable without `contributes.stages`, returning `closed: false`; declaring `stages` requires one explicitly in manifest) | Close the plugin's active WebView stage | [§7.4 · 15](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.stage.state` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed; callable without `contributes.stages`, returning `open: false`; declaring `stages` requires one explicitly in manifest) | Query whether the plugin's WebView stage is open | [§7.4 · 15](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.stage.emit` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed) + requires `contributes.stages` (declaring `stages` requires one explicitly in manifest) | Emit an event payload to the open stage WebView | [§7.4 · 15](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.sidebar.get_badges` | `ui:sidebar` (default-allowed) | Read file tree badge entries written by this plugin | [§7.4 · 9](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.sidebar.set_badges` | `ui:sidebar` (default-allowed) + `contributes.file_badges` | Set, patch, or clear file tree note badges | [§7.4 · 9](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.sidebar.get_file_icons` | `ui:sidebar` (default-allowed) | Read custom file/folder icons and preferences for a notebook | [§7.4 · 9.1](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.sidebar.set_file_icons` | `ui:sidebar` (default-allowed) + `contributes.file_icons` | Update or clear custom file/folder/notebook icons and preferences | [§7.4 · 9.1](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.editor.selection` | `editor:read` | Read the active editor selection, note path, and focus flag | [§7.4 · 10](https://docs.block.cooool.top/plugins/ui-contributions) |
| `host.editor.insert` | `editor:write` | Parse Markdown and insert or replace at the active selection | [§7.4 · 11](https://docs.block.cooool.top/plugins/ui-contributions) |
<!-- host-methods-table:end -->

> **Removed methods**: Legacy RPCs `host.ui.editor_background`, `host.ui.set_editor_background`, `host.ui.mindmap_state`, and `host.ui.set_mindmap` have been removed and now return `-32601` (superseded by `host.ui.backdrop` / `host.ui.set_backdrop` and `contributes.block_decorators`).

---

### 4.2 Plugin -> Host RPC Methods

Plugins may call `host.*` while handling `plugin.initialize`, `plugin.render`, `plugin.action`, `plugin.command`, or `plugin.event` (max 8 concurrent in-flight calls; unavailable during `plugin.shutdown`).

Common error codes: `-32001` missing permission, `-32002` storage failure or `> 8 MiB`, `-32003` note listing failure / unregistered notebook, `-32004` filesystem error / symlink / `> 16 MiB`, `-32005` host backend unavailable or failed, `-32006` dirty editor / virtual preview / revision conflict / rate limit, `-32008` notebook/root identity mismatch (`expected_root_identity`), `-32601` unknown method, `-32602` invalid params.

Note path rules: case-insensitive `.md`, `.markdown`, `.mdown`, `.mkd`, `.mkdn`, `.mdwn`, `.mdtxt`, `.mdtext`, `.mdx`. Explicit `notebook_root` must match a registered notebook; omitting uses runtime root. `host.notes.list` skips dot-prefixed entries and non-regular files; `read`/`write` reject `:`, dot segments, escapes, symlinks, and files `> 16 MiB`.

1. **`host.storage.get`** (no permission): `{"key": "config"}` -> `{"value", "found"}` or `{}` -> `{"data": {...}}`.
2. **`host.storage.set`** / **`host.storage.set_many`** (no permission; 8 MiB cap): `{"key", "value"}` or `{"values": {...}}` (1–64 non-blank keys) -> `{"ok": true}`. Preserves raw JSON numbers.
3. **`host.notes.active`** (`"notes:read"`): `{}` -> `{"path": "welcome.md"}`.
4. **`host.notebooks.list`** / **`host.notebooks.data_dir`** (`"notes:read"`):
   - `host.notebooks.list {}` -> `{"notebooks": [{"id", "path", "name", "active", "previous_paths", "identity_verified", "root_identity"}]}` (`id` is a host-managed durable identifier preserved across moves/renames; `previous_paths` lists previously recorded paths; `identity_verified` reports whether live filesystem identity—xattr and/or `dev:ino`—matches the registered notebook without following symlinks; `root_identity` reports `"<dev>:<ino>"` when available).
   - `host.notebooks.data_dir` (`{"id" | "path" | "notebook" | "notebook_id" | "notebook_root"}`, defaulting to the active notebook when omitted) creates and returns `{"id", "path", "previous_paths", "identity_verified", "root_identity"?}` pointing to `<data_dir>/notebooks/<id>/` (stable across renames; cleaned by the host 30 days after removal or confirmed absence).
5. **`host.notes.list`** (`"notes:read"`): `{"notebook_root"?}` -> `{"notes": [{"path", "size", "mod_time"}]}` (returns `{"notes": []}` when no notebook is active; unregistered notebook returns `-32003`).
6. **`host.notes.read`** (`"notes:read"`): `{"notebook_root"?, "path"}` -> `{"path", "content", "revision"}` (`revision` is 64-char lowercase SHA-256).
7. **`host.notes.write`** (`"notes:write"`): `{"notebook_root"?, "path", "content", "expected_revision"?, "expected_root_identity"?}` -> `{"ok": true, "changed": true|false}`. `content` must be a string (max 16 MiB). When optional `expected_root_identity` (`"<dev>:<ino>"`) is provided, the host verifies the opened notebook `os.Root` handle identity against it at entry and commit time under the same handle (returning `-32008` on mismatch, `-32602` if malformed). Checks dirty editor / virtual preview (`-32006`), then byte equality (`changed: false`), then `expected_revision` (`""` requires non-existent file; SHA-256 requires match, else `-32006`). Does not return new `revision`—re-read before chained writes.
8. **`host.notes.trash`** (`"notes:write"`): `{"notebook_root"?, "path", "expected_revision", "expected_root_identity"?}` -> `{"ok": true, "trashed": true|false}`. `expected_revision` is mandatory (SHA-256 of current file bytes). Optional `expected_root_identity` (`"<dev>:<ino>"`) is checked on the opened notebook `os.Root` handle right before hashing and trashing under that same handle (`-32008` on mismatch). Allows non-Markdown regular files inside the notebook (still rejects dot segments and symlinks). Use `host.system.trash` when user confirmation is desired.
9. **`host.notes.open`** (`"notes:read"`): `{"notebook_root"?, "path"}` -> `{"ok": true, "path"}` (headless CLI returns `-32005`).
10. **`host.action.progress`** (no permission; check `"action_progress"` capability): prefer JSON-RPC notification (no `id`) so it does not consume an in-flight slot. Supports `{"done", "total"}` or `{"progress": 0..1}`, optional `label` (`<= 32` chars), and with `"progress_bar"` optional `channel`, `tone`, and `running`.
11. **`host.perf.snapshot`** (`"notes:read"`): `{"refresh_disk": false}` -> host CPU, RSS, and 7 disk categories (`notes`, `assets`, `history`, `recovery`, `ai_sessions`, `logs`, `config`).
12. **`host.store.changelog`** (no permission; check `"store_changelog"` capability): `{"id": "example-sync", "locale": "en-US", "limit": 10}` (`limit` default 10, max 20). Resolves static JSON API → GitHub Releases → inline index changelog → local `CHANGELOG.md` (local fallback populates `markdown` and may have `releases: null`).
13. **`host.log`** (no permission; `-32601` on older hosts): `{"entries": [{"level", "event", "notebook_root", "path", "protocol", "detail"}]}` (max 200 entries, 400 bytes per field). Never log note bodies or secrets.
14. **`host.fs.*`** (`"system:fs"`, host absolute paths only; resolves symlinks before checking scope via `os.Root`. Authorized scope covers `data_dir`, `downloads_dir`, `granted-paths.json` entries, and registered notebooks when also holding `"notes:read"` for reads or `"notes:write"` for writes. Protected host files inside `data_dir`—`granted-paths.json`, `scheduler.json`, `state.json`—cannot be modified directly. Note that `host.system.open_dialog`/`save_dialog` only return selected paths; file grants are registered via form pickers or menubar drops):
    - `host.fs.list_grants` (`{}`): `{"grants": [...], "data_dir", "downloads_dir", "notebooks": [{"path", "writable"}]}`.
    - `host.fs.stat` (`{"path", "lstat"?}`): `{"exists", "path", "name", "is_dir", "is_regular", "is_symlink", "size", "mod_time", "mod_time_ms", "is_icloud_placeholder"}` (pass `"lstat": true` to inspect symlinks/junctions without following them).
    - `host.fs.list` (`{"path", "recursive"}`, max 10000 entries): `{"path", "entries": [...], "truncated": bool}` (recursive entries include `rel_path`).
    - `host.fs.read` (`{"path", "encoding": "utf8"|"base64", "max_bytes"}`, default 16 MiB, host cap 64 MiB): `{"path", "encoding", "size", "content"|"content_base64"}`.
    - `host.fs.write` (`{"path", "content"|"content_base64", "atomic", "mkdir_parents", "expected_root_identity"?}`, host cap 64 MiB).
    - `host.fs.mkdir` (`{"path", "recursive"|"parents"}`), `host.fs.delete` (`{"path", "recursive", "expected_root_identity"?}`), `host.fs.delete_data` (`{"path", "recursive"?}` -> `{"ok": true, "deleted": bool, "path"}`; strictly confined to plugin `data_dir` using the cached startup `os.Root` handle—never grants, downloads, or notebooks—checks protected host files by `os.SameFile` identity rather than filename alone and traverses/deletes once along open directory handles, rejecting any symlink/junction with `-32004` or protected file/escape with `-32001`), `host.fs.rename` (`{"from", "to", "overwrite", "mkdir_parents", "expected_root_identity"?}`; falls back to copy+delete across different grant roots), `host.fs.copy` (`{"from", "to", "overwrite", "mkdir_parents", "expected_root_identity"?}`; host-side streaming atomic copy of regular non-symlink files bypassing Wasm memory, rejects identical paths and grant roots themselves, returns `{"ok": true, "from", "to", "size"}`), `host.fs.atomic_swap` (`{"path_a", "path_b", "expected_root_identity"?}`; may succeed with `atomic: false` if OS atomic exchange is unavailable). Optional `expected_root_identity` (`"<dev>:<ino>"`) verifies the opened destination/target directory `os.Root` handle identity and returns `-32008` on mismatch.
15. **`host.crypto.*`** (memory hashing requires no permission; `file_path` requires `"system:fs"`):
    - `host.crypto.hash`: `{"algorithm": "sha1"|"sha256"|"sha512"|"md5", "text"|"base64"|"file_path"}` -> `{"algorithm", "hex", "base64", "size"}`.
    - `host.crypto.hmac`: `{"algorithm": "sha1"|"sha256"|"sha512", "key_text"|"key_base64", "data_text"|"data_base64"}` -> `{"algorithm", "hex", "base64"}`.
16. **`host.http.request`** (`"network"`, enforced against `network_domains` and SSRF rules):
    - Params: `{"url", "method", "headers", "body"|"body_base64"|"body_file_path", "save_to_path", "mkdir_parents", "response_encoding": "utf8"|"base64"|"none", "timeout_ms", "max_response_bytes"}`.
    - Supports `GET`, `POST`, `PUT`, `DELETE`, `HEAD`, `PATCH`, `OPTIONS`, `PROPFIND`, `MKCOL`, `MOVE`, `COPY`. Default timeout 30s (max 120s); response cap 16 MiB default (64 MiB max). Rejects on the 10th candidate redirect (up to 9 hops); strips `Authorization`, `Cookie`, and `Proxy-Authorization` across origins. `body_file_path` / `save_to_path` additionally require `"system:fs"`, and `save_to_path` defaults `response_encoding` to `"none"`. Non-2xx status codes return normally (and still write to `save_to_path` when specified). Returns `{"status", "headers", "bytes", "final_url", "body"|"body_base64", "saved_path"}`.
17. **`host.exec.run`** (`"system:exec"`, resolves allowlisted binary via host `PATH` without shell; case-sensitive allowlist match):
    - Params: `{"program", "args", "cwd", "env", "stdin", "timeout_ms", "max_output_bytes"}` (`args` max 256; `cwd` must be an authorized directory, default `data_dir`; safe env vars + up to 16 safe `GIT_CONFIG_*`, forced `GIT_TERMINAL_PROMPT=0`; timeout 30s default / 120s max; `max_output_bytes` applies separately to `stdout` and `stderr`, 1 MiB default / 8 MiB max each).
    - Returns `{"exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "timed_out"}`.
18. **`host.scheduler.*`** (`"events:system"`, persisted in `scheduler.json`, max 32 jobs/plugin):
    - `host.scheduler.register`: `{"id", "interval_sec", "event", "require_network", "run_on_wake", "run_on_network_restore", "run_on_focus"}` (`interval_sec` clamped to `60`–`86400`, default event `"scheduler.tick"`) -> `{"ok": true, "id"}`. Fires `plugin.event` with `{"event", "job_id", "trigger", "online", "timestamp"}` (plus `notebook_root` when holding note permissions).
    - `host.scheduler.unregister` (`{"id"}`) and `host.scheduler.list` (`{}`).
19. **`host.ai.*`** (`"ai:complete"`):
    - `host.ai.status` (`{}`): `{"enabled", "ready", "streaming"}`.
    - `host.ai.complete`: `{"system_prompt", "user_prompt", "max_tokens"}` (`user_prompt` required non-empty, combined prompts `<= 128 KiB`, `max_tokens <= 16384`, 120s timeout) -> `{"text", "model"}` (`model` may be omitted).
    - `host.ai.open_with_prompt`: `{"prompt", "note_path"}` (`prompt <= 128 KiB`) -> `{"ok": true}`.

20. **`host.sidebar.set_ignored_dirs` / `host.sidebar.get_ignored_dirs`** require an explicit `"ui:sidebar"` declaration without a consent prompt. Check for `"sidebar_ignored_dirs"` in `ui_capabilities` before calling.
    - `set_ignored_dirs` takes `{"dirs": [".generated"]}`, replaces the calling plugin's runtime list, and returns `{"ok": true}`. Send `{"dirs": []}` to clear the runtime list.
    - `get_ignored_dirs` takes `{}` and returns the calling plugin's runtime list as `{"dirs": [...]}`. The initial value is `{"dirs": []}`.
    - Block combines the static and runtime lists of all enabled plugins. Runtime lists are held in memory and cleared on disable, uninstall, plugin update, or Block restart. See [Host UI Contributions](https://docs.block.cooool.top/plugins/ui-contributions) for directory name rules.

### 4.3 System Integration

Desktop hosts advertise `"system_integration"` in `ui_capabilities`. Call `host.system.capabilities {}` (no permission) to query live boolean support for `notify`, `notify_click`, `secrets`, `open_url`, `reveal`, `open_dialog`, `save_dialog`, `clipboard_read`, `clipboard_write`, `trash`, `badge`, `wake`, `network`, and `focus`.

| Method | Permission | Contract |
| --- | --- | --- |
| `host.system.notify` | Default-allowed | `{"title", "body", "tone", "notebook_root"?, "rel_path"?, "timeout_ms"?, "dedup_key"?}`. Sends OS native notification (`title <= 80`, `body <= 240`). `notebook_root`/`rel_path` require `notes:read`. Rate limit: 5 distinct notifications per 10s (`-32006`); 2-minute dedup window (`suppressed: true`). |
| `host.system.open_url` | `system:open` | `{"url": "https://example.com"}` (HTTP/HTTPS without embedded credentials). |
| `host.system.reveal` | `system:open`, `notes:read` | `{"rel_path", "notebook_root"?}`: reveals file in OS file manager. |
| `host.system.open_dialog`, `host.system.save_dialog` | `system:dialogs` | Optional `title`, `default_path`, `extensions` (plus `directory`, `multiple` for open). Returns `{"ok": true, "paths": [...], "canceled": false}`. Canceled/timed-out actions return `-32007`. |
| `host.clipboard.read`, `host.clipboard.write` | `system:clipboard_read` / `write` | UTF-8 text up to 64 KiB. |
| `host.system.trash` | `system:trash`, `notes:write` | Regular file only (`{"rel_path", "notebook_root"?, "expected_root_identity"?}`); verifies optional `expected_root_identity` (`-32008` on mismatch) and shows native confirmation dialog before moving to OS Trash. |
| `host.system.badge` | Default-allowed | `{"count": 0..999}` (0 clears plugin contribution; host sums enabled plugins up to 999). |
| `host.secrets.get`, `set`, `delete` | `system:secrets` | `{"key", "value"?}` (`key <= 128` ASCII alnum/`._-`, `value` 1–1536 bytes, no NUL). Isolated by app data dir, plugin ID, and install provenance. |

Interactive system operations (open URL, reveal, dialogs, clipboard, trash) require a user-triggered `plugin.action` or `plugin.command` while Block is focused. `events:system` delivers `system.wake`, `network.changed`, and `app.focus` via `plugin.event`, plus ordered `notebook.renamed` (`{"id", "old_path", "new_path"}`) and `notebook.removed` (`{"id", "path"}`) lifecycle events when the plugin also holds note permissions.

### 4.4 Compatibility & Platform Notes

- **`ui_capabilities` keys** (up to 40 total, depending on host version, config, and OS):
  `kv_tooltip`, `treemap`, `host_perf`, `checklist`, `checklist_add_folder`, `form_layout`, `multi_notebook`, `card_collapse`, `metric_tone`, `log_columns`, `file_picker`, `action_feedback`, `action_progress`, `note_revision`, `sidebar_sections`, `file_badges`, `sidebar_menu`, `sidebar_ignored_dirs`, `backdrops`, `status_icon`, `toast_icon`, `notes_open`, `log_actions`, `panel_toggle`, `editor_insert_mode`, `dialog_token`, `item_icons`, `tab_menu`, `file_path`, `log_timeline`, `progress_bar`, `segmented_status`, `toast_banner`, `open_settings`, `form_autosave`, `button_link`, `store_changelog`, `system_integration`, `menubar`, `menubar_drop` (macOS only).
- **Platform Details**: macOS uses Notification Center (click targets persist 7 days across restarts), Keychain, and Dock; Windows uses native notifications, Credential Manager, and taskbar badge; Linux uses desktop notifications, Secret Service (must be unlocked), Unity Launcher Dock, and logind wake events.
- **Safe Trash**: saves dirty editors before confirming, stages on the same volume, verifies file identity, restores on failure, and tracks original paths for recovery prompts.

---

## 5. Declarative UI Schema (`UIPage`)

Within a `UIPage`, every `card.id`, control `item.id`, and `button.id` (`header_actions` + `actions`) must be unique. The host owns breadcrumbs, Pluck Switch enable toggle, primary action pill, and `⋯` menu.

### Minimal `UIPage` Example
```json
{
  "title": "Example Sync",
  "status": "Last sync 14:20",
  "header_actions": [
    { "id": "sync_now", "label": "Sync Now", "variant": "primary" }
  ],
  "cards": [
    {
      "id": "target-card",
      "title": "Target",
      "items": [
        {
          "kind": "segmented",
          "id": "protocol",
          "label": "Protocol",
          "value": "local_mirror",
          "options": [
            { "value": "local_mirror", "label": "Local Mirror" },
            { "value": "webdav", "label": "WebDAV" }
          ]
        }
      ]
    }
  ]
}
```
- `title`: required non-empty string.
- `status`: optional short read-only status (omit or `""` when idle; never write placeholders like `"Ready"` / `"Enabled"`).
- `progress`: optional `0`–`1` float for polling background progress (use `host.action.progress` during `plugin.action`).
- `refresh_interval_ms`: optional `500`–`60000` ms (`0`/omitted disables polling).
- `header_actions`: optional buttons (`variant`: `"primary"` | `"secondary"` | `"danger"` | `"link"` (requires `button_link`), plus optional `busy_label`/`success_label`/`error_label`/`retry_label`). First `primary` (or first `danger`) appears in the titlebar; remaining buttons move into `⋯`.
- `cards`: required non-empty array (`<= 64` cards, `<= 400` total items). Each card has unique `id`, optional `title`, `meta`, `badge`, `collapsible`, `default_collapsed`, and non-empty `items`.

### 16 `UIItem` Controls

With `form_autosave`, set `"autosave": "<action_id>"` on `input` (including URLs and numbers entered as strings), `secret`, `select`, `segmented`, `switch`, `checklist`, or `folder`. Supported `group.fields` work too. The ID follows button ID rules and can be shared across controls. Text triggers on blur or about 800 ms after typing stops. Other controls trigger immediately when their value changes. The host skips unchanged values. Each page sends one autosave at a time and combines queued triggers into one follow-up using the latest action ID and the current form values. Omit `autosave` and keep a submit button on older hosts.

```json
[
  { "kind": "input", "id": "endpoint_url", "label": "Server URL", "value": "", "autosave": "save_config" },
  { "kind": "actions", "buttons": [{ "id": "help", "label": "Setup guide", "variant": "link" }] }
]
```

All `UIItem`s (including `group.fields`) support optional `visible_when` (`{"control_id", "equals"? , "in"?}`; non-empty `in` takes precedence over `equals`; strict JSON type equality). Valid `layout` values: `""`, `"stacked"` (`segmented`/`folder`/`input`/`secret`), `"full"` (`segmented` only; allows omitting `label`). Soft palette keys: `"assets"`, `"config"`, `"notes"`, `"history"`, `"recovery"`, `"logs"`, `"ai_sessions"`.

1. **`segmented`**: `id`, `label` (unless `layout == "full"`), `options` (`>= 2`, optional `tone`/`tooltip`/`channel` with `"segmented_status"`), `value` matching an option.
2. **`select`**: `id`, `label`, `options` (`>= 1`), `value` matching an option, optional `placeholder`.
3. **`input`**: `id`, `label`, `value` (`string`), optional `placeholder`, `layout`.
4. **`folder`**: `id`, `label`, `value` (path or `""`), optional `layout`, `accept_extensions` (hint only—validate selected files in plugin code).
5. **`secret`**: same as `input` with masked display and visibility toggle.
6. **`switch`**: `id`, `label`, `value` (`bool`), optional `icon` (`{"name"}` | inline `DuoIconSpec` | `{"chip", "dot"}` with `"item_icons"`).
7. **`actions`**: non-empty `buttons` array (same fields as `header_actions` plus optional `icon`). With `button_link`, `variant: "link"` uses accent text on a transparent background, with no border, a click target at least 28 px tall, and a keyboard focus ring. Use `secondary` on older hosts.
8. **`metrics`**: non-empty `metrics` array (`label`, `value`, optional `tone`), auto-laid out 1–4 columns.
9. **`kv`**: non-empty `rows` (`label`, `value`, optional `tooltip`, `icon`).
10. **`preview`**: `preview` object with `variant` (`"article"` | `"cards"` | `"diff"` | `"plan"`), `palette` (`"ocean"` | `"paper"` | `"ink"` | `"warm"`), required `title`, optional `badge`, `subtitle`, `body`, `before`, `after`, `cards`.
11. **`log_list`**: required non-empty `empty_text`, optional `label`, `entries` (`title`, optional `sub_text`, `status`, `time`, `tone`, `ratio`, `tooltip`, `action_id`). With `"log_timeline"`, set `"variant": "timeline"` for `kind: "event"|"span"`, `badge`, `span`, `tags` (`<= 24`), and `details` (`<= 64`).
12. **`checklist`** (card top-level only; not allowed in `group`): `id`, `value` (`string[]`), `options` (`value`, `label`, optional `detail`/`badge`/`tooltip`). With `"checklist_add_folder"`, set `add_folder: true` for a user-managed folder list.
13. **`group`**: `fields` (`>= 2` items of `input`/`secret`/`segmented`/`select`/`folder`/`switch`) arranged side by side.
14. **`treemap`**: non-empty `tiles` (`id`, `label`, `bytes`, optional `detail`/`file_count`/`color`/`entries`), optional `id`, `total_bytes` (`uint64`), `summary` (`total_value` required).
15. **`icon_strip`** (requires `"item_icons"`, card top-level only): `icons` (`<= 32`), optional `label`, `empty_text`.
16. **`progress`** (requires `"progress_bar"`): at least one of `label` or `status`, optional `id`, `channel`, `progress` (`0`–`1`), `tone`, `tooltip`.

---

## 6. `entry.js` + `/runtime/block-sdk.js` Starter Skeleton

Block's `wazero` + `quickjs-ng` sandbox loads `entry.js` as an ES module and injects `/runtime/block-sdk.js` (`BlockSDK`, `serve`, `callHost`, `notifyHost`, `send`, `log`, `HostRPCError` on `globalThis`). Both `serve` and `callHost` are synchronous—do not pass an `async` handler. The example below tracks `locale` so it passes `-bilingual` out of the box:

```javascript
let locale = 'zh';
const tr = (zh, en) => (locale.toLowerCase().startsWith('en') ? en : zh);

serve((method, params) => {
  switch (method) {
    case 'plugin.initialize':
      locale = String(params?.locale || 'zh');
      return { ready: true };
    case 'plugin.render':
      return {
        page: {
          title: tr('示例插件', 'Example Plugin'),
          cards: [
            {
              id: 'main',
              items: [
                {
                  kind: 'kv',
                  rows: [{ label: tr('状态', 'Status'), value: tr('正常', 'OK') }],
                },
              ],
            },
          ],
        },
      };
    case 'plugin.action':
      return { ok: true, tone: 'success', status: tr('已完成', 'Done') };
    case 'plugin.event':
      if (params?.event === 'locale.changed' && params?.locale) {
        locale = String(params.locale);
      }
      return { handled: true };
    case 'plugin.shutdown':
      return { ok: true };
    default:
      return {};
  }
});
```

---

## 7. Host UI Contributions

Plugins extend Block outside their settings page via `manifest.contributes`. Contributions include titlebar buttons and layout management, built-in settings sections, commands and shortcuts, context menus, sidebar sections, file tree badges, file icon packs, ignored directories, editor backdrops, block decorators, theme skins, offline WebView stages, panels, status items, dialogs, and system menu bar items. See [Manifest & Permissions](https://docs.block.cooool.top/plugins/manifest) for defaults and explicit permission requirements.

### 7.1 Multi-Surface Rendering & State Isolation

- Declare `["settings_page"]` in `manifest.surfaces` if your plugin needs a dedicated settings page; `settings_sections`, `panels`, `sidebar_sections`, and `dialogs` automatically expose prefixed surfaces:
  - `"settings_section:<id>"`
  - `"panel:<id>"`
  - `"sidebar_section:<id>"`
  - `"dialog:<id>"`
- The host passes `params.surface` to `plugin.render` and `plugin.action` and isolates loading state, busy/feedback status, form values, polling intervals, and notebook context per `(plugin_id, notebook_root, surface)`.

### 7.2 `manifest.contributes` Fields

Shared validation rules:
- Contribution `id`s must match `^[a-z][a-z0-9_-]{0,31}$` and be unique within each category.
- All `LocalizedText` fields require non-empty `zh` and `en` with zero Emoji. Every `DuoIconSpec` requires `view_box == "0 0 24 24"`, `>= 1` `stroke_paths`, `<= 16` combined paths, and `<= 4096` total path bytes.
- Per-category limits: `titlebar_buttons` 3, `settings_sections` 8, `commands` 64, `page_menu` 16, `editor_menu` 16, `sidebar_menu` 16, `tab_menu` 16, `panels` 8, `sidebar_sections` 4, `file_badges` 4, `file_icons` 1, `backdrops` 8, `block_decorators` 8, `themes` 8, `stages` 8, `status_items` 4, `dialogs` 16, `menubar_items` 2.

1. **`titlebar_buttons`** (max 3): icon buttons in the right titlebar (overflowing into the "More plugins" menu; users can pin up to 3). Requires `id`, `icon`, `tooltip`, and **exclusively one** of `command` or `panel`.
1.1. **`titlebar_manager`** (top-level boolean in `contributes`; requires `"ui:titlebar"` in `manifest.permissions`): when `true`, allows calling `host.titlebar.set_hidden`.
2. **`settings_sections`**: appends a declarative card to a built-in settings tab (`tab`: `"ai"` | `"appearance"` | `"folders"` | `"shortcuts"`). Prefer a dedicated `settings_page` unless a single setting genuinely belongs in a built-in tab.
3. **`commands`**: registers command palette entries and optional default `shortcut` (e.g. `"cmd-alt-p"`; host-reserved shortcuts take precedence). Block automatically surfaces plugin commands under **Settings → Shortcuts** and at the bottom of the plugin's settings page—do not build custom shortcut pickers.
4. **`page_menu`**: appends items to the page `…` menu (`id`, `title`, `command`, optional `icon`).
5. **`editor_menu`**: appends items to the editor context menu (`id`, `title`, `command`, optional `icon`).
6. **`sidebar_menu`**: appends items to the file tree context menu (`id`, `title`, `command`, optional `icon`, optional `targets`: subset of `["note", "folder"]`, default `["note"]`). Sets `context.note` for notes or `context.folder` for folders.
6.1. **`tab_menu`**: appends items to tab context menus (`context.source: "tab_menu"`). Targets the right-clicked tab; files outside the notebook have empty `note` and provide `context.file` when holding `"notes:path"`.
7. **`panels`**: right-side panels (`id`, `title`, `icon`; surface `"panel:<id>"`).
8. **`sidebar_sections`** (max 4): compact collapsible cards below the file tree (`id`, `title`, `icon`, optional `collapsible`, `default_collapsed`; surface `"sidebar_section:<id>"`).
9. **`file_badges`** (max 4): trailing file tree badges pushed via `host.sidebar.set_badges` (max 2 shown per row; cleared on disable/uninstall).
9.1. **`file_icons`** (max 1): custom notebook/file/folder icon pack (`id`, `pack` relative `.json <= 512 KiB`, optional `title`, `picker`, `sync_tabs`). Pack schema: `schema_version: 1`, `categories` (1–16), `icons` (1–256, each using either host `ref` or inline `spec`).
9.2. **`ignored_dirs`** requires an explicit `"ui:sidebar"` declaration. Each list accepts up to 32 exact directory names of 1–128 bytes each. Names may not contain `/`, `\`, or NUL, or equal `.` or `..`. An enabled plugin can exclude matching directories and their contents from the file tree, workspace scans, and search in every notebook, including when Show Hidden Files is on. Disable or uninstall removes that plugin's exclusions and refreshes the tree.
10. **`backdrops`** (max 8): custom editor or full-window vector or paper-grain backgrounds controlled via `host.ui.set_backdrop` (`pattern`: `"scatter"` | `"grid"` | `"watermark"` | `"corners"` | `"paper_dots"` | `"paper_grid"`; `density`, `contrast`, `scope`: `"editor"` default | `"window"` (`"fullscreen"` alias), `opacity` clamped to `0.03`–`0.25`, `tint`, `dark_glow`).
11. **`status_items`**: bottom status bar entries (`id`, optional `text`, `command`, `icon`).
12. **`dialogs`**: modal dialogs opened via `host.ui.open_dialog` (`id`, `title`, optional `width` `320`–`720` default `460`, `icon`; surface `"dialog:<id>"`).
13. **`menubar_items`** (max 2): OS menu bar icons (`id`, `icon`—template strokes only, `fill_paths` omitted—`tooltip`, and at least one of `command`, `menu` (`<= 16`), or `drop`; `command` and `menu` are mutually exclusive). Menu items with `input: "clipboard"` require `"system:clipboard_read"`. Drag-and-drop (`drop`) is currently macOS-only (`ui_capabilities` includes `"menubar_drop"`).
14. **`block_decorators`** (max 8; requires `"ui:appearance"` or `"editor:write"`): matches hidden HTML comment signatures to render Callout styling and empty placeholders (`id`, `signature_prefix`, `default_callout`, optional `block_kind`, `legacy_tag_prefix`, `callout_by_kind`, `empty_placeholder`).
15. **`themes`** (max 8; requires `"ui:appearance"`): JSON theme skins (`id`, `title`, `file` relative `.json <= 512 KiB`) switched via `host.ui.set_theme_skin`.
16. **`stages`** (max 8; requires `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"`): offline WebView stages served from `web/` (`<= 128` regular files, `<= 4 MiB` check limit / `<= 16 MiB` serve limit) over `block-plugin://<plugin-id>/...` with `window.blockView` (`getInitState`, `invoke`, `onEvent`, `close`) and 4-layer offline isolation (static `http://`/`https://` literal ban in `web/`, WebView `ContentRules` blocking external HTTP/WS, same-origin navigation lock with no popups, strict CSP `connect-src 'none'; script-src 'self'`).

### 7.3 `plugin.command` & Workspace `plugin.event`

1. **`plugin.command`** (Host -> Plugin, 30s timeout; auto-starts plugin if stopped):
   - Request `params`: `{"command_id", "context": {"note"?, "folder"?, "file"?, "selection"?, "source", ...}}`.
   - `context.file` requires `"notes:path"`; `context.selection` requires `"editor:read"`. Menubar sources also pass `menubar_item`, `payload`, `kind` (`"files"` | `"image"` | `"text"`), `files` (`<= 50`), and `text` (`<= 64 KiB`). Temporary image drop PNGs live in a plugin temp dir and are cleaned after 24 hours.
   - Return `result`: optional `ok`, `tone`, `status`, `body`, `action`, `show_plugin_icon`, and `copy_text` (requires `"system:clipboard_write"`; works even when Block is in the background).
2. **Workspace `plugin.event`** (`"events:workspace"`, 5s timeout, delivered only to already-running plugins):
   - `"note.opened"` (`{"path"}`), `"theme.changed"` (`{"theme": "light"|"dark"}`), `"locale.changed"` (`{"locale": "zh"|"en"}`).

### 7.4 Host UI RPC Methods (Plugin -> Host)

> Note: Methods marked `"ui:*" (default-allowed)` are checked against that permission in code, which is auto-granted without declaring it in `manifest.permissions`. Methods marked **explicit** or sensitive permissions must be declared in `manifest.permissions`. See the [Host Method Index](https://docs.block.cooool.top/plugins/protocol-and-ui#host-method-index) for all 79 `host.*` methods.

1. **`host.titlebar.get_items`** (runtime checks `"ui:titlebar"`, default-allowed; `{}`): returns `{"items": [...], "groups": [...]}`.
   - Each `item` includes `id`, `label`, `locked`, `hidden`, optional `plugin` (**returned only on plugin buttons** and omitted on built-in slots), `icon` (for `switch` rows), **`group`** (`"markdown"`, `"source"`, or `"preview"`; plugin buttons use `"markdown"`), and **`preview`** (array of `UIIcon`s generated from the same definitions as real titlebar rendering, ready for `icon_strip.icons`—e.g. `zoom` is `[{"name": "tb-minus"}, {"chip": "100%"}, {"name": "tb-plus"}, {"chip": "Fit"}]`, `send_all` is `[{"chip": "Send all", "dot": true}]`, and plugin buttons return a 1-element inline-vector array).
   - `groups` is derived from the same mode rules as real titlebar rendering and lists each mode group (`"markdown"` / `"source"` / `"preview"`), localized `label` (`"Markdown"` / `"Source Mode"` / `"Image Preview"`), and left-to-right `order` (expanding enabled plugin buttons as `"plugin:<pluginID>/<buttonID>"` with pinned buttons first, followed by install order; **the same plugin button can appear in multiple groups' `order`** across `"markdown"`, `"source"`, and `"preview"`, and `"markdown"` composite pill sub-items `send_all` and `more` follow the live pill layout). Older hosts omit `group`, `preview`, and `groups` (and earlier hosts omit `icon`).
   - Manageable built-in slot IDs: `feedback`, `ai`, `send_all`, `wrap`, `zoom`, `more` (`locked: true`); plugin buttons use `plugin:<pluginID>/<buttonID>`. `reveal` lives only in the page `…` menu and tab context menu, is non-manageable, and is omitted from `get_items`.
2. **`host.titlebar.set_hidden`** (runtime checks `"ui:titlebar"`, default-allowed, plus `contributes.titlebar_manager: true`; manifest validation requires explicit `"ui:titlebar"` when `titlebar_manager: true` is declared): `{"hidden": [...]}` returns `{"ok": true, "items": [...], "groups": [...]}`.
   - Hidden state is **isolated per calling plugin** in `contrib-state.json`, and **disabling a `titlebar_manager` plugin immediately withdraws its hiding effect**.
   - Enforces `locked` from each slot definition (such as `"more"`) and ignores unknown or non-manageable slots (such as `"reveal"`, while preserving the existing `…` and tab context menu items).
3. **`host.titlebar.set_button`** (checks `"ui:titlebar"`, default-allowed; requires `id` in `contributes.titlebar_buttons`): `{"id", "badge"?, "active"?}` (`badge <= 3` runes).
4. **`host.status.set`** (checks `"ui:status"`, default-allowed; requires `id` in `contributes.status_items`): `{"id", "text"?, "tooltip"?, "icon"?, "show_plugin_icon"?, "clear_icon"?}` (`text <= 40` runes, no Emoji; `tooltip <= 200` runes).
5. **`host.ui.toast`** (no permission check): in-app top-right notification banner (`message` required `<= 160`, optional `body <= 1000`, `tone`, `icon`, `show_plugin_icon`, `action` callback to `plugin.action` with `surface: "toast"`). Returns `{"ok": true, "shown", "queued"}`.
5.1. **`host.ui.open_settings`** (no permission check; requires `"open_settings"` capability and `"settings_page"` surface): opens the plugin's Settings page.
6. **`host.ui.open_dialog` / `host.ui.close_dialog`** (checks `"ui:notify"`, default-allowed; requires `id` in `contributes.dialogs`): `{"id"}`. `open_dialog` returns `{"ok": true, "dialog_token": "dlg-..."}`. Actions from that `dialog:*` surface carry the active dialog instance's `dialog_token` (reused across actions within the same open dialog instance).
7. **`host.ui.open_panel` / `close_panel` / `toggle_panel`** (checks `"ui:panels"`, default-allowed; requires `id` in `contributes.panels`): `{"id"}`; `toggle_panel` returns `{"ok": true, "open": true|false}`.
8. **`host.ui.refresh`** (no permission check; requires declared `surface`): `{"surface"}` asynchronously re-renders an already-opened surface; returns `{"ok": true}` or `{"ok": true, "skipped": true}` if notebook scope does not match.
9. **`host.sidebar.set_badges` / `get_badges`** (checks `"ui:sidebar"`, default-allowed; `set_badges` requires `badge_id` in `contributes.file_badges`): `{"badge_id": "review-mark", "notebook_root"?, "clear"?, "patch"?, "entries": {...}}` (`<= 512` entries, `text <= 4` no Emoji, `tooltip <= 120` no Emoji, `tone` in `accent|success|warning|danger`).
9.1. **`host.sidebar.set_file_icons` / `get_file_icons`** (checks `"ui:sidebar"`, default-allowed; `set_file_icons` requires `contributes.file_icons`): `entries` (`<= 2048`, `"."` for notebook itself), `tint` (`none|blue|teal|green|amber|red|purple`), `prefs` (`picker`, `sync_tabs`).
9.2. **`host.sidebar.set_ignored_dirs` / `host.sidebar.get_ignored_dirs`** both require an explicit `"ui:sidebar"` declaration. See the [JSON-RPC protocol](https://docs.block.cooool.top/plugins/protocol-and-ui) for request and response shapes.
10. **`host.editor.selection`** (`"editor:read"`): `{"note", "text", "has_focus"}`.
11. **`host.editor.insert`** (`"editor:write"`): `{"text", "note"?, "mode": "replace"|"insert"}` (`text` required string `<= 4 MiB`; parses Markdown into native blocks and saves immediately; rejects folder/out-of-notebook context menu targets with `-32004`).
12. **`host.menubar.set`** (checks `"ui:menubar"`, default-allowed; requires `id` in `contributes.menubar_items`): `{"id", "title"?, "tooltip"?, "menu"?, "clear_menu"?}` (`title <= 16` no Emoji, `menu <= 20` dynamic items in memory, `payload <= 4096` bytes).
13. **`host.ui.set_background_error` / `clear_background_error`** (no permission check): `{"key", "label", "detail", "retry_action", "retry_label", "retry_payload", "severity": "danger"|"warning"}` (max 32 keys/plugin; omitting `key` on clear clears all).
14. **`host.ui.theme_skin_state` / `host.ui.set_theme_skin`**:
    - **Runtime permission**: both check `"ui:appearance"` (default-allowed at runtime even if omitted from `manifest.permissions`).
    - **Manifest validation**: declaring non-empty `contributes.themes` requires explicit `"ui:appearance"` in `manifest.permissions`.
    - **Per-method contribution requirement**:
      - `host.ui.theme_skin_state` (`{}`): does **not** require `contributes.themes`; plugins without `themes` can call it with default permissions and receive `{"id", "enabled", "skin_id"}` (`skin_id` is optional and only returned while a skin is active; otherwise `enabled` is `false`).
      - `host.ui.set_theme_skin` (`{"id", "enabled": true|false}`): **requires** `contributes.themes`; activates or deactivates a declared theme skin and persists state in `contrib-state.json`.
15. **`host.stage.*`**:
    - **Runtime permission**: all four methods check `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"` (all three are default-allowed at runtime).
    - **Manifest validation**: declaring non-empty `contributes.stages` requires explicitly declaring at least one of `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"` in `manifest.permissions`.
    - **Per-method contribution requirement**:
      - `host.stage.open` (`{"id", "route", "state"}` -> `{"ok": true, "open": true, "stage": {...}}`) and `host.stage.emit` (`{"id", "event", "payload"}` -> `{"ok": true}`): **require** `contributes.stages` (`room` is accepted as a legacy alias of `route`; `state` and `payload` accept any valid JSON up to 2 MiB).
      - `host.stage.close` (`{"id"}`) and `host.stage.state` (`{}`): do **not** require `contributes.stages`; plugins without `stages` (or without an open stage) receive `{"ok": true, "closed": false}` and `{"open": false}` respectively (when open, `state` returns `{"open": true, "stage_id", "route", "state"}`).

### 7.5 Editor Backdrop Methods (`host.ui.backdrop` / `host.ui.set_backdrop`)

Controls editor or full-window vector or paper-grain backdrops declared in `contributes.backdrops` (checks `"ui:appearance"`, default-allowed; `set_backdrop` requires `contributes.backdrops`):

- **`host.ui.backdrop`** (`{}`): returns persisted backdrop state `{"id", "enabled", "pattern", "density", "contrast", "scope", "opacity", "tint", "dark_glow"}` (`scope` defaults to `"editor"`).
- **`host.ui.set_backdrop`**: updates and persists backdrop state in `contrib-state.json` (omitted style fields keep existing or manifest defaults; `scope`: `"editor"` for the centered text column or `"window"`—`"fullscreen"` is accepted as an alias and normalized to `"window"`—to fill the entire editor canvas and empty page stage edge-to-edge without extending into the sidebar or titlebar; treats the canvas border as grid index 0 so the first drawn dot/grid line never sits on the border, automatically clears the left outline rail, and keeps floating panels, menus, tooltips, modals, and toasts isolated above it; disabled automatically when the plugin is turned off) and returns the normalized state object.

---

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

---

## 9. Local Protocol Check (`block-plugin-check`)

```bash
CGO_ENABLED=0 go -C /path/to/block run ./cmd/block-plugin-check [-bilingual] /abs/path/to/plugin
```

Place `-bilingual` before the plugin directory path; the CLI accepts a single plugin directory per run. Exit code `0` with `[PASS] Plugin "<id>" passed all Block App Store checks.` confirms the local check passed. Default-allowed permissions in `manifest.permissions` produce non-blocking `[WARN]` messages (note: keep required `ui:*` declarations for `titlebar_manager`, `block_decorators`, `themes`, and `stages`).

- **Automated Pipeline**: Runs in a temporary notebook and data directory (20s per-locale RPC budget after install, with a 15s `plugin.initialize` budget): validates `manifest.json` (`network_domains`, `exec_allowlist`, `permission_reasons`, lengths, zero Emoji, and `ignored_dirs` names and permission), `languages` / `file_icons` / `themes` / `stages` (static `web/` offline & size checks), directory installation (including pure declarative packs), initialization, empty and default-value rendering, single-control value sweeps across top-level and `group` `segmented`/`select`/`switch` controls (no multi-control cartesian product; `checklist` not swept), invocation of all initial `header_actions` and top-level `actions` buttons (ignoring `visible_when` and collapsed state; `open_path`/`reveal_path` checked via `os.Stat`; `ok: false` still passes), direct `note.saved` dispatch when `"events:note_saved"` is declared, and `plugin.shutdown`. Interactive `entry.js` / `entry.wasm` packages execute inside the real embedded `wazero` + `quickjs-ng` sandbox.
- **Bilingual Gate**: Automatically enabled when `manifest.repo` is under `blockmd-plugins/*` (official CI should still pass `-bilingual` explicitly; community plugins may opt in with `-bilingual`; `verified.json` 1:1 binding is enforced by the store crawler):
  1. Validates `name.en`, `summary.en`, `contributes.*.en`, and declared `permission_reasons[*].en` are non-empty English with no Chinese characters or punctuation.
  2. Requires root `README.md` (`<= 512 KiB` UTF-8, contains Chinese) and `README.en.md` (`<= 512 KiB` UTF-8, no Chinese characters/punctuation, distinct from `README.md`). Local image assets must exist; bitmaps and Chinese assets require separate paths per language, while pure-English UTF-8 `.svg` files may be shared.
  3. Runs interactive plugins under both `locale="zh"` and `locale="en"`, verifying zero Chinese characters in scanned English UI fields and action `status`/`page` outputs and verifying different initial renders when a render surface exists (the CLI does not scan button state labels or some timeline fields, surface-less command plugins have no render diff, and `plugin.command` must be verified in the desktop app).
- **CLI Coverage Limits**: Uses a single temporary notebook, does not test default-disabled state or dirty-editor protection, and does not wire `ContribHost`, system integration, AI, or `GetPerfSnapshot` (`host.perf.snapshot` returns `-32005`; `store_changelog` is advertised in `ui_capabilities` but returns `-32601` in CLI; `stages` is checked statically only). Verify real sync, uploads, commands, and desktop UI in Block itself.

---

## 10. Publishing to the App Store (`blockmd-plugin` Crawling)

Block's App Store uses approval-free automatic crawling. **Threshold & Cap**: The crawler queries `topic:blockmd-plugin fork:false archived:false` and takes the **top 100 public, non-fork, unarchived GitHub repositories sorted by stars** (no pagination; official repos in `verified.json` outside the top 100 are fetched separately) while filtering reserved IDs (`block-diagnostics`). Desktop manual installation via local folder, `owner/repo`, or HTTPS archive has no 100-repo cap.

Publishing steps:

1. Push your plugin to a public, non-fork, unarchived GitHub repository with a validated `manifest.json` (`"app": "blockmd"`, `repo` matching `owner/repo` if set) at the root or single top-level archive folder. Official plugins (`blockmd-plugins/*`, verified via `verified.json`) must include bilingual `README.md` / `README.en.md`, separate non-English assets, and `locale="en"` UI support; community plugins are also encouraged to provide bilingual docs and `permission_reasons`.
2. Add the **`blockmd-plugin`** topic under **About → Topics** on GitHub.
3. Publishing a standard **GitHub Release** (non-draft, non-prerelease, tag matching `manifest.version`) is recommended (when no suitable Release exists, the crawler falls back to a commit-pinned default-branch tarball):
   - **Automated Releases**: You can configure a scheduled or `workflow_dispatch` GitHub Actions workflow that runs manifest/bilingual smoke checks before packaging, computes a monotonically increasing SemVer version into the archive artifact, and publishes a Release without pushing commits back to `main`.
   - **Manual Releases**: Push a `vX.Y.Z` tag or create a GitHub Release directly.

4. **Crawler & Desktop Verification Rules** (executed by `blockmd-plugins/store-index` and `cmd/block-store-crawler`):
   - **Package Selection**: Picks the first non-draft, non-prerelease Release, preferring its first `.tar.gz`/`.tgz`/`.zip` asset, then its `tarball_url`. Only when no Release URL is selected does it fall back to a commit-pinned default-branch archive (`codeload.github.com/{repo}/tar.gz/{commit_sha}`) so `sha256` stays immutable. Download/parse failure skips the repository without falling back further.
   - **Index Validation & Synced Fields**: Computes `sha256` on raw archive bytes, extracts `manifest.json` (version comes from `manifest.version`), checks `app == "blockmd"`, `id`, reserved/blocklist rules, `verified.json` 1:1 binding, official bilingual docs/manifest/explicit category, `repo` match, grammar compilation, and cross-repo duplicate `id` conflicts, then syncs `category`, `permission_reasons`, and `file_extensions` (`<= 6` tags). Any official verified entry failure fails the crawl run.
   - **Changelogs**: Extracts structured bilingual release notes (`## 中文` / `## English`), inlines the latest 5 in `store-index.json`, and publishes up to 20 releases in `changelogs/<id>.json` (with jsDelivr mirror), consumed by the Store detail view and `host.store.changelog`.
   - **Ranking**: Sorted by total Release asset `downloads` (tie-break by name, then `id`); search ranks by match score then downloads.
   - **Desktop Install Verification**: Store installs enforce HTTPS (`<= 4096` bytes, no credentials/fragment, restricted redirect hosts), `sha256`, and manifest `id`/`version`/`permissions`/`repo` match. Manual `owner/repo`, HTTPS archive URL, or local folder installs validate manifest and archive limits without store `sha256` matching. All sources prompt a permission confirmation card whenever sensitive permissions are first granted or added on update.

- **Catalog and offline cache.** Block fetches `store-index.json` from GitHub or jsDelivr, merges `system_apps` over `apps`, and caches it under `<BlockAppData>/apps-cache/`. The crawler and desktop Store both filter by plugin ID or repository using `store/blocklist.json`. With no catalog cache, the Store shows only installed entries while offline. External plugins are published in independent repositories.
- **Optional artwork.** Put `store/icon.svg` (icon) and `store/hero.svg` (featured art) in the repository root; no manifest field is needed, and each file can be up to 512 KiB. Release packages include both files: installed plugins read them from the package, so the Store and the Settings sidebar show the same icon. Plugins that are not installed fetch them from the default branch (GitHub with a jsDelivr fallback) and cache them for the session. The icon falls back to `manifest.icon`. A featured card without artwork shows a large `manifest.icon` on a colored background. Featured cards come from the plugin ID list in `store/featured.json`; tones rotate automatically in order.
