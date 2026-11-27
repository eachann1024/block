<!-- ai-guide:skip-start -->
# Block Plugin Developer Documentation

> [Back to Docs](../README.md) · [Manifest & Permissions](manifest.md) · [Protocol, UI & SDK](protocol-and-ui.md) · [Host UI Contributions](ui-contributions.md) · [Language Extensions](languages.md) · [Check & Store](check-and-store.md) · [Self-Contained AI Spec](../plugin-dev-for-ai.md)

This directory splits the complete Block App Store (`blockmd`) plugin specification into modular chapters. Read by topic or use the single-file bundle [plugin-dev-for-ai.md](../plugin-dev-for-ai.md).

> **AI Coding Assistants**: Fetch the plain-text full specification at [https://docs.block.cooool.top/en/plugin-dev-for-ai.md](https://docs.block.cooool.top/en/plugin-dev-for-ai.md). It includes all schemas, RPC contracts, 16 UI controls, and the built-in QuickJS SDK starter skeleton. If browsing by chapter, read the required chapters before writing code and never invent undocumented fields.

---
<!-- ai-guide:skip-end -->

## 1. Before You Start (AI Execution Guide)

External plugins **require no PR to the main repository and no manual approval**. Host your plugin in a standalone public, non-fork, unarchived GitHub repository with the `blockmd-plugin` topic to participate in automatic store crawling (top 100 repositories sorted by stars; publishing a non-draft, non-prerelease GitHub Release is recommended, and the crawler falls back to a commit-pinned default-branch tarball when no suitable Release exists—see [Check & Store](check-and-store.md)). At any stage, desktop users can also install directly via local folder, `owner/repo`, or HTTPS archive URL.

### 1.1 Choose Your Path (3 Development Paths)

| Path | Use Case | Deliverables | Notes |
| --- | --- | --- | --- |
| Interactive / Community Plugin | Sync, image hosting, export, AI panels, host UI extensions | Standalone repo + `manifest.json` + `entry.js` (built-in `wazero` + `quickjs-ng` sandbox; `entry.wasm` for compute-heavy work) | Entry point requires no user-installed runtime (only explicit `host.exec.run` calls depend on allowlisted host binaries); add `blockmd-plugin` topic for automatic crawling |
| Declarative Language Extension | Syntax highlighting for additional languages in code blocks and source editor | Standalone repo + `manifest.json` (`category: "language"`) + `grammars/*.json` | Zero process, zero permissions, auto-enabled on install; see [Language Extensions](languages.md) |
| Officially Verified (`blockmd-plugins/*`) | Officially maintained plugins bound 1:1 in `store/verified.json` | Standalone repo + `manifest.json` + entry or grammars | Requires bilingual `README.md` / `README.en.md` and `locale="en"` UI gate |

### 1.2 Eight Hard Rules to Avoid Rejection

1. **Never submit plugins via PR to the main repo or under `plugins/`**: all external plugins live in independent repositories.
2. **Never draw your own enable/disable switch, uninstall button, or bypass host UI**: use declarative `UIPage` JSON for settings and extensions—the host renders the enable toggle, titlebar controls, and uninstall menu. When a custom interactive canvas is required, use an offline `block-plugin://` WebView via `contributes.stages` (no external network resources; see [Host UI Contributions](ui-contributions.md)).
3. **Zero Emoji in static UI copy**: manifests or static `UIPage` labels containing Emoji are rejected on load. Icons must use `manifest.icon` two-tone SVG paths (`view_box: "0 0 24 24"`, at least 1 `stroke_paths` entry).
4. **Never invent unknown fields or invalid values**: `manifest.json` (`<= 256 KiB`, no duplicate keys) and `UIPage` use strict decoding (`DisallowUnknownFields`). `id` must match `^[a-z0-9][a-z0-9-]{1,47}$` and must not be `block-diagnostics`; `surfaces` allows only `["settings_page"]` or `[]`; `category` must be one of the 5 display categories (`"language"` is required for declarative language packs; retired `"sync"` / `"publish"` should not be used).
5. **Interactive plugins must use the built-in sandbox**: declare `entry.js` or `entry.wasm` (see [Manifest & Permissions](manifest.md)). Write only newline-delimited JSON-RPC 2.0 to `stdout` and flush logs to `stderr`. `entry.js` is loaded as an ES module and `serve` / `callHost` are synchronous—do not pass an `async` handler returning a Promise.
6. **Access host paths only through host RPCs**: the sandbox mounts only read-only `/plugin` and `/runtime` and cannot open host absolute paths directly. Use `host.storage.*` for private state, and `host.fs.*` / `host.notes.*` with `data_dir`, `downloads_dir`, or authorized paths.
7. **Request minimal permissions and obey shutdown**: declare only needed sensitive permissions across the 5 groups (15 total), omit unnecessary default-allowed permissions (keeping explicit `ui:*` where still required by `titlebar_manager`, `block_decorators`, `themes`, or `stages`, and removing matching `permission_reasons` keys when removing a permission). Exit cleanly within 2s on `plugin.shutdown` without calling `host.*`.
8. **Keep initial actions safe under empty/default config**: `block-plugin-check` renders with empty and default values, toggles individual controls, and invokes every top-level `header_actions` and `actions` button regardless of `visible_when`. Return `{"ok": false, "status": "..."}` cleanly when configuration is incomplete.

### 1.3 Standard Workflow

1. **Read the matching chapters before coding**: interactive plugins read [Manifest & Permissions](manifest.md) + [Protocol, UI & SDK](protocol-and-ui.md) (plus [Host UI Contributions](ui-contributions.md) if extending host chrome); language extensions read [Manifest & Permissions](manifest.md) + [Language Extensions](languages.md).
2. **Create a standalone repository**: include `manifest.json` and `README.md` at the root (official and bilingual plugins also include English `README.en.md`).
3. **Implement core logic and declarative UI**: start interactive plugins from the locale-aware `entry.js` + `/runtime/block-sdk.js` skeleton in [Protocol, UI & SDK](protocol-and-ui.md); language extensions only need `manifest.json` and `grammars/*.json`.
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

<!-- ai-guide:skip-start -->
---

Next: [2. Manifest & Permissions →](manifest.md)
<!-- ai-guide:skip-end -->
