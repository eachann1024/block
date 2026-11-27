<!-- ai-guide:skip-start -->
# Host UI Contributions (`manifest.contributes`)

> [← Protocol, UI & SDK](protocol-and-ui.md) · [Docs Home](../README.md) · [Language Extensions →](languages.md)

---
<!-- ai-guide:skip-end -->

## 7. Host UI Contributions

Plugins extend Block outside their settings page via `manifest.contributes`: titlebar buttons, titlebar layout management, built-in settings sections, commands/shortcuts, page/editor/sidebar/tab menus, sidebar sections, file tree badges, custom file icon packs, editor backdrops, block decorators, theme skins, offline WebView stages, right-side panels, status bar items, modal dialogs, and system menu bar items. All `ui:*` permissions are default-allowed at runtime, except `titlebar_manager`, `block_decorators`, `themes`, and `stages`, which still require explicit permission entries during manifest validation (see [Manifest & Permissions](manifest.md)).

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

1. **`host.titlebar.get_items`** (`{}`): returns all built-in (`feedback`, `ai`, `send_all`, `wrap`, `zoom`, `reveal`, `more` [locked]) and plugin (`plugin:<pluginID>/<buttonID>`) titlebar slots with their `icon` specs.
2. **`host.titlebar.set_hidden`** (requires `contributes.titlebar_manager: true` and `"ui:titlebar"`): `{"hidden": [...]}` persisted in `contrib-state.json` (`"more"` is permanently locked and cannot be hidden).
3. **`host.titlebar.set_button`**: `{"id", "badge"?, "active"?}` (`badge <= 3` runes).
4. **`host.status.set`**: `{"id", "text"?, "tooltip"?, "icon"?, "show_plugin_icon"?, "clear_icon"?}` (`text <= 40` runes, no Emoji; `tooltip <= 200` runes).
5. **`host.ui.toast`**: in-app top-right notification banner (`message` required `<= 160`, optional `body <= 1000`, `tone`, `icon`, `show_plugin_icon`, `action` callback to `plugin.action` with `surface: "toast"`). Returns `{"ok": true, "shown", "queued"}`.
5.1. **`host.ui.open_settings`** (`{}`; requires `"open_settings"` capability and `"settings_page"` surface): opens the plugin's Settings page.
6. **`host.ui.open_dialog` / `host.ui.close_dialog`**: `{"id"}`. `open_dialog` returns `{"ok": true, "dialog_token": "dlg-..."}`. Actions from that `dialog:*` surface carry the active dialog instance's `dialog_token` (reused across actions within the same open dialog instance).
7. **`host.ui.open_panel` / `close_panel` / `toggle_panel`**: `{"id"}`; `toggle_panel` returns `{"ok": true, "open": true|false}`.
8. **`host.ui.refresh`**: `{"surface"}` asynchronously re-renders an already-opened surface; returns `{"ok": true}` or `{"ok": true, "skipped": true}` if notebook scope does not match.
9. **`host.sidebar.set_badges` / `get_badges`**: `{"badge_id": "review-mark", "notebook_root"?, "clear"?, "patch"?, "entries": {...}}` (`<= 512` entries, `text <= 4` no Emoji, `tooltip <= 120` no Emoji, `tone` in `accent|success|warning|danger`).
9.1. **`host.sidebar.set_file_icons` / `get_file_icons`** (requires `contributes.file_icons`): `entries` (`<= 2048`, `"."` for notebook itself), `tint` (`none|blue|teal|green|amber|red|purple`), `prefs` (`picker`, `sync_tabs`).
10. **`host.editor.selection`** (`"editor:read"`): `{"note", "text", "has_focus"}`.
11. **`host.editor.insert`** (`"editor:write"`): `{"text", "note"?, "mode": "replace"|"insert"}` (`text` required string `<= 4 MiB`; parses Markdown into native blocks and saves immediately; rejects folder/out-of-notebook context menu targets with `-32004`).
12. **`host.menubar.set`**: `{"id", "title"?, "tooltip"?, "menu"?, "clear_menu"?}` (`title <= 16` no Emoji, `menu <= 20` dynamic items in memory, `payload <= 4096` bytes).
13. **`host.ui.set_background_error` / `clear_background_error`**: `{"key", "label", "detail", "retry_action", "retry_label", "retry_payload", "severity": "danger"|"warning"}` (max 32 keys/plugin; omitting `key` on clear clears all).
14. **`host.ui.theme_skin_state` / `set_theme_skin`** (requires `contributes.themes` and `"ui:appearance"`): gets or switches active plugin theme skin.
15. **`host.stage.*`** (requires `contributes.stages` and matching UI permission): `host.stage.open` (`{"id", "route", "state"}`; `state` accepts any valid JSON `<= 2 MiB`), `close`, `state`, and `emit` (`{"id", "event", "payload"}`; `payload <= 2 MiB`).

### 7.5 Editor Backdrop Methods (`host.ui.backdrop` / `host.ui.set_backdrop`)

Controls editor vector or paper-grain backdrops declared in `contributes.backdrops` (no extra permission required):

- **`host.ui.backdrop`** (`{}`): returns persisted backdrop state `{"id", "enabled", "pattern", "density", "contrast", "opacity", "tint", "dark_glow"}`.
- **`host.ui.set_backdrop`**: updates and persists backdrop state in `contrib-state.json` (omitted style fields keep existing or manifest defaults; disabled automatically when the plugin is turned off) and returns the normalized state object.

<!-- ai-guide:skip-start -->
---

Next: [5. Language Extensions →](languages.md)
<!-- ai-guide:skip-end -->
