<!-- ai-guide:skip-start -->
# Host UI Contributions (`manifest.contributes`)

> [← Protocol, UI & SDK](protocol-and-ui.md) · [Docs Home](../README.md) · [Language Extensions →](languages.md)

---
<!-- ai-guide:skip-end -->

## 7. Host UI Contributions

Plugins extend Block outside their settings page via `manifest.contributes`. Contributions include titlebar buttons and layout management, built-in settings sections, commands and shortcuts, context menus, sidebar sections, file tree badges, file icon packs, ignored directories, editor backdrops, block decorators, theme skins, offline WebView stages, panels, status items, dialogs, and system menu bar items. See [Manifest & Permissions](manifest.md) for defaults and explicit permission requirements.

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
10. **`backdrops`** (max 8): custom editor vector or paper-grain backgrounds controlled via `host.ui.set_backdrop` (`pattern`: `"scatter"` | `"grid"` | `"watermark"` | `"corners"` | `"paper_dots"` | `"paper_grid"`; `density`, `contrast`, `opacity` clamped to `0.03`–`0.25`, `tint`, `dark_glow`).
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

> Note: Methods marked `"ui:*" (default-allowed)` are checked against that permission in code, which is auto-granted without declaring it in `manifest.permissions`. Methods marked **explicit** or sensitive permissions must be declared in `manifest.permissions`. See the [Host Method Index](protocol-and-ui.md#host-method-index) for all 78 `host.*` methods.

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
9.2. **`host.sidebar.set_ignored_dirs` / `host.sidebar.get_ignored_dirs`** both require an explicit `"ui:sidebar"` declaration. See the [JSON-RPC protocol](protocol-and-ui.md) for request and response shapes.
10. **`host.editor.selection`** (`"editor:read"`): `{"note", "text", "has_focus"}`.
11. **`host.editor.insert`** (`"editor:write"`): `{"text", "note"?, "mode": "replace"|"insert"}` (`text` required string `<= 4 MiB`; parses Markdown into native blocks and saves immediately; rejects folder/out-of-notebook context menu targets with `-32004`).
12. **`host.menubar.set`** (checks `"ui:menubar"`, default-allowed; requires `id` in `contributes.menubar_items`): `{"id", "title"?, "tooltip"?, "menu"?, "clear_menu"?}` (`title <= 16` no Emoji, `menu <= 20` dynamic items in memory, `payload <= 4096` bytes).
13. **`host.ui.set_background_error` / `clear_background_error`** (no permission check): `{"key", "label", "detail", "retry_action", "retry_label", "retry_payload", "severity": "danger"|"warning"}` (max 32 keys/plugin; omitting `key` on clear clears all).
14. **`host.ui.theme_skin_state` / `host.ui.set_theme_skin`**:
    - **Runtime permission**: both check `"ui:appearance"` (default-allowed at runtime even if omitted from `manifest.permissions`).
    - **Manifest validation**: declaring non-empty `contributes.themes` requires explicit `"ui:appearance"` in `manifest.permissions`.
    - **Per-method contribution requirement**:
      - `host.ui.theme_skin_state` (`{}`): does **not** require `contributes.themes`; plugins without `themes` can call it with default permissions and receive `{"id", "enabled", "skin_id"}` with `enabled: false`.
      - `host.ui.set_theme_skin` (`{"id", "enabled": true|false}`): **requires** `contributes.themes`; activates or deactivates a declared theme skin and persists state in `contrib-state.json`.
15. **`host.stage.*`**:
    - **Runtime permission**: all four methods check `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"` (all three are default-allowed at runtime).
    - **Manifest validation**: declaring non-empty `contributes.stages` requires explicitly declaring at least one of `"ui:appearance"`, `"ui:commands"`, or `"ui:panels"` in `manifest.permissions`.
    - **Per-method contribution requirement**:
      - `host.stage.open` (`{"id", "route", "state"}`; `state` accepts valid JSON `<= 2 MiB`) and `host.stage.emit` (`{"id", "event", "payload"}`; `payload <= 2 MiB`): **require** `contributes.stages`.
      - `host.stage.close` (`{"id"}`) and `host.stage.state` (`{}`): do **not** require `contributes.stages`; plugins without `stages` (or without an open stage) receive `{"ok": true, "closed": false}` and `{"open": false}` respectively (when open, `state` returns `{"open": true, "stage_id", "route", "state"}`).

### 7.5 Editor Backdrop Methods (`host.ui.backdrop` / `host.ui.set_backdrop`)

Controls editor vector or paper-grain backdrops declared in `contributes.backdrops` (checks `"ui:appearance"`, default-allowed; `set_backdrop` requires `contributes.backdrops`):

- **`host.ui.backdrop`** (`{}`): returns persisted backdrop state `{"id", "enabled", "pattern", "density", "contrast", "opacity", "tint", "dark_glow"}`.
- **`host.ui.set_backdrop`**: updates and persists backdrop state in `contrib-state.json` (omitted style fields keep existing or manifest defaults; disabled automatically when the plugin is turned off) and returns the normalized state object.

<!-- ai-guide:skip-start -->
---

Next: [5. Language Extensions →](languages.md)
<!-- ai-guide:skip-end -->
