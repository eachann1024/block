<!-- ai-guide:skip-start -->
# Bidirectional JSON-RPC 2.0 Protocol, Declarative UI & SDK Skeleton

> [← Manifest & Permissions](manifest.md) · [Docs Home](../README.md) · [Host UI Contributions →](ui-contributions.md)

---
<!-- ai-guide:skip-end -->

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

This table lists all 79 `host.*` JSON-RPC methods currently dispatched by the host and their exact permission / manifest requirements. Capabilities marked "default-allowed" may be omitted from `manifest.permissions` unless an explicit declaration is noted; all other permissions must be declared explicitly.

<!-- host-methods-table:start -->
| Method | Required Permission / Manifest Condition | Purpose | Details |
| --- | --- | --- | --- |
| `host.storage.get` | `storage` (default-allowed) | Read one private storage key or the full key-value map | [§4.2 · 1](protocol-and-ui.md) |
| `host.storage.set` | `storage` (default-allowed) | Write or overwrite a single private storage key | [§4.2 · 2](protocol-and-ui.md) |
| `host.storage.set_many` | `storage` (default-allowed) | Atomically write 1–64 private storage keys at once | [§4.2 · 2](protocol-and-ui.md) |
| `host.notes.active` | `notes:read` | Get the active tab's relative note path | [§4.2 · 3](protocol-and-ui.md) |
| `host.notebooks.list` | `notes:read` | List known notebook IDs, paths, and active state | [§4.2 · 4](protocol-and-ui.md) |
| `host.notebooks.data_dir` | `notes:read` | Resolve or create a per-notebook data directory under plugin `data_dir` | [§4.2 · 4](protocol-and-ui.md) |
| `host.notes.list` | `notes:read` | List visible Markdown notes inside a notebook | [§4.2 · 5](protocol-and-ui.md) |
| `host.notes.read` | `notes:read` | Read note Markdown content and SHA-256 `revision` | [§4.2 · 6](protocol-and-ui.md) |
| `host.notes.write` | `notes:write` | Write or update a note (supports `expected_revision`) | [§4.2 · 7](protocol-and-ui.md) |
| `host.notes.trash` | `notes:write` | Trash a notebook file by `expected_revision` without prompt | [§4.2 · 8](protocol-and-ui.md) |
| `host.notes.open` | `notes:read` (`-32005` in headless CLI) | Open a notebook note in the desktop editor | [§4.2 · 9](protocol-and-ui.md) |
| `host.action.progress` | None (check `action_progress`; channel mode needs `progress_bar`) | Report live `plugin.action` progress and channel status | [§4.2 · 10](protocol-and-ui.md) |
| `host.perf.snapshot` | `notes:read` (check `host_perf`) | Snapshot host CPU, RSS, and 7 disk categories | [§4.2 · 11](protocol-and-ui.md) |
| `host.store.changelog` | None (check `store_changelog`) | Fetch App Store plugin release changelog | [§4.2 · 12](protocol-and-ui.md) |
| `host.log` | None | Append structured diagnostic entries to `logs/block.log` | [§4.2 · 13](protocol-and-ui.md) |
| `host.fs.list_grants` | `system:fs` (notebook listing also needs `notes:read`/`notes:write`) | List granted directories, data/downloads dirs, and notebooks | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.stat` | `system:fs` | Inspect metadata and iCloud placeholder state of a granted path | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.list` | `system:fs` | List entries in a granted directory (optionally recursive) | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.read` | `system:fs` | Read a granted file as UTF-8 or Base64 | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.write` | `system:fs` | Write UTF-8 or Base64 data to a granted path (supports atomic) | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.mkdir` | `system:fs` | Create a directory under a granted path | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.delete` | `system:fs` | Delete a granted file or directory | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.rename` | `system:fs` | Rename or move a granted file or directory | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.copy` | `system:fs` | Stream-copy a regular file on the host without Wasm buffering | [§4.2 · 14](protocol-and-ui.md) |
| `host.fs.atomic_swap` | `system:fs` | Atomically exchange two granted files in the same directory | [§4.2 · 14](protocol-and-ui.md) |
| `host.crypto.hash` | None (`system:fs` when `file_path` is passed) | Compute SHA/MD5 hash of text, Base64, or a granted file | [§4.2 · 15](protocol-and-ui.md) |
| `host.crypto.hmac` | None | Compute SHA-1, SHA-256, or SHA-512 HMAC | [§4.2 · 15](protocol-and-ui.md) |
| `host.http.request` | `network` (restricted by `network_domains`; file I/O needs `system:fs`) | Perform HTTP(S) request on the host with optional file streaming | [§4.2 · 16](protocol-and-ui.md) |
| `host.exec.run` | `system:exec` + binary in `manifest.exec_allowlist` | Run an allowlisted executable without a shell | [§4.2 · 17](protocol-and-ui.md) |
| `host.scheduler.register` | `events:system` | Register or update a periodic/wake/network/focus background job | [§4.2 · 18](protocol-and-ui.md) |
| `host.scheduler.unregister` | `events:system` | Unregister a scheduled background job | [§4.2 · 18](protocol-and-ui.md) |
| `host.scheduler.list` | `events:system` | List all registered background jobs for this plugin | [§4.2 · 18](protocol-and-ui.md) |
| `host.ai.status` | `ai:complete` | Query host AI enabled, ready, and streaming flags | [§4.2 · 19](protocol-and-ui.md) |
| `host.ai.complete` | `ai:complete` | Run a single-turn AI completion using the host model | [§4.2 · 19](protocol-and-ui.md) |
| `host.ai.open_with_prompt` | `ai:complete` | Open the host AI panel prefilled with a prompt | [§4.2 · 19](protocol-and-ui.md) |
| `host.sidebar.get_ignored_dirs` | **Explicit `ui:sidebar`** (check `sidebar_ignored_dirs`) | Read this plugin's runtime ignored directory names | [§4.2 · 20](protocol-and-ui.md) |
| `host.sidebar.set_ignored_dirs` | **Explicit `ui:sidebar`** (check `sidebar_ignored_dirs`) | Replace or clear this plugin's runtime ignored directory names | [§4.2 · 20](protocol-and-ui.md) |
| `host.system.capabilities` | None (check `system_integration`) | Query boolean availability of OS integration features | [§4.3](protocol-and-ui.md) |
| `host.system.notify` | `system:notify` (default-allowed; note link needs `notes:read`) | Send a native OS notification banner | [§4.3](protocol-and-ui.md) |
| `host.system.open_url` | `system:open` + active user action & window focus | Open an HTTP(S) URL in the default browser | [§4.3](protocol-and-ui.md) |
| `host.system.reveal` | `system:open` + `notes:read` + user action & focus | Reveal a notebook file in the OS file manager | [§4.3](protocol-and-ui.md) |
| `host.system.open_dialog` | `system:dialogs` + active user action & window focus | Show a native OS file or directory picker | [§4.3](protocol-and-ui.md) |
| `host.system.save_dialog` | `system:dialogs` + active user action & window focus | Show a native OS save-location picker | [§4.3](protocol-and-ui.md) |
| `host.system.trash` | `system:trash` + `notes:write` + user action & focus | Confirm natively and move a regular notebook file to Trash | [§4.3](protocol-and-ui.md) |
| `host.system.badge` | `system:badge` (default-allowed) | Set or clear this plugin's app icon badge count | [§4.3](protocol-and-ui.md) |
| `host.clipboard.read` | `system:clipboard_read` + user action & window focus | Read UTF-8 text from the OS clipboard | [§4.3](protocol-and-ui.md) |
| `host.clipboard.write` | `system:clipboard_write` + user action & window focus | Write UTF-8 text to the OS clipboard | [§4.3](protocol-and-ui.md) |
| `host.secrets.get` | `system:secrets` | Read a plugin-isolated credential from the OS keychain | [§4.3](protocol-and-ui.md) |
| `host.secrets.set` | `system:secrets` | Store or update a plugin-isolated credential | [§4.3](protocol-and-ui.md) |
| `host.secrets.delete` | `system:secrets` | Delete a stored credential from the OS keychain | [§4.3](protocol-and-ui.md) |
| `host.titlebar.get_items` | `ui:titlebar` (default-allowed) | Get manageable titlebar slots (`group`/`preview`) and per-mode `groups` order | [§7.4 · 1](ui-contributions.md) |
| `host.titlebar.set_hidden` | Runtime `ui:titlebar` (default-allowed) + `contributes.titlebar_manager: true` (manifest requires explicit `ui:titlebar`) | Persist hidden titlebar slot IDs (per-plugin; ignores `locked` and non-manageable slots) | [§7.4 · 2](ui-contributions.md) |
| `host.titlebar.set_button` | `ui:titlebar` (default-allowed) + `contributes.titlebar_buttons` | Update badge text and active highlight of a titlebar button | [§7.4 · 3](ui-contributions.md) |
| `host.menubar.set` | `ui:menubar` (default-allowed) + `contributes.menubar_items` | Update OS menu bar icon title, tooltip, and dynamic menu | [§7.4 · 12](ui-contributions.md) |
| `host.status.set` | `ui:status` (default-allowed) + `contributes.status_items` | Update text, tooltip, and icon of a status bar item | [§7.4 · 4](ui-contributions.md) |
| `host.ui.toast` | None | Show an in-app top-right notification banner | [§7.4 · 5](ui-contributions.md) |
| `host.ui.open_settings` | None + `surfaces` includes `"settings_page"` (`open_settings`) | Open this plugin's dedicated Settings page | [§7.4 · 5.1](ui-contributions.md) |
| `host.ui.open_dialog` | `ui:notify` (default-allowed) + `contributes.dialogs` | Open a declared modal dialog and return `dialog_token` | [§7.4 · 6](ui-contributions.md) |
| `host.ui.close_dialog` | `ui:notify` (default-allowed) + `contributes.dialogs` | Close a declared modal dialog | [§7.4 · 6](ui-contributions.md) |
| `host.ui.open_panel` | `ui:panels` (default-allowed) + `contributes.panels` | Open a declared right-side panel | [§7.4 · 7](ui-contributions.md) |
| `host.ui.close_panel` | `ui:panels` (default-allowed) + `contributes.panels` | Close a declared right-side panel | [§7.4 · 7](ui-contributions.md) |
| `host.ui.toggle_panel` | `ui:panels` (default-allowed) + `contributes.panels` | Atomically toggle a side panel and return open state | [§7.4 · 7](ui-contributions.md) |
| `host.ui.refresh` | None + target `surface` must be declared by plugin | Asynchronously re-render an already-opened surface | [§7.4 · 8](ui-contributions.md) |
| `host.ui.set_background_error` | None | Register or update a background error indicator in Settings/titlebar | [§7.4 · 13](ui-contributions.md) |
| `host.ui.clear_background_error` | None | Clear one or all background errors for this plugin | [§7.4 · 13](ui-contributions.md) |
| `host.ui.backdrop` | `ui:appearance` (default-allowed) | Read persisted editor backdrop state | [§7.5](ui-contributions.md) |
| `host.ui.set_backdrop` | `ui:appearance` (default-allowed) + `contributes.backdrops` | Update and persist editor vector or paper backdrop state | [§7.5](ui-contributions.md) |
| `host.ui.theme_skin_state` | Runtime `ui:appearance` (default-allowed; callable without `contributes.themes`, returning `enabled: false`; declaring `themes` requires explicit `ui:appearance` in manifest) | Query active state of this plugin's theme skin | [§7.4 · 14](ui-contributions.md) |
| `host.ui.set_theme_skin` | Runtime `ui:appearance` (default-allowed) + requires `contributes.themes` (declaring `themes` requires explicit `ui:appearance` in manifest) | Activate or deactivate a declared theme skin | [§7.4 · 14](ui-contributions.md) |
| `host.stage.open` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed) + requires `contributes.stages` (declaring `stages` requires one explicitly in manifest) | Open an offline `block-plugin://` WebView stage | [§7.4 · 15](ui-contributions.md) |
| `host.stage.close` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed; callable without `contributes.stages`, returning `closed: false`; declaring `stages` requires one explicitly in manifest) | Close the plugin's active WebView stage | [§7.4 · 15](ui-contributions.md) |
| `host.stage.state` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed; callable without `contributes.stages`, returning `open: false`; declaring `stages` requires one explicitly in manifest) | Query whether the plugin's WebView stage is open | [§7.4 · 15](ui-contributions.md) |
| `host.stage.emit` | Runtime `ui:appearance` / `ui:commands` / `ui:panels` (default-allowed) + requires `contributes.stages` (declaring `stages` requires one explicitly in manifest) | Emit an event payload to the open stage WebView | [§7.4 · 15](ui-contributions.md) |
| `host.sidebar.get_badges` | `ui:sidebar` (default-allowed) | Read file tree badge entries written by this plugin | [§7.4 · 9](ui-contributions.md) |
| `host.sidebar.set_badges` | `ui:sidebar` (default-allowed) + `contributes.file_badges` | Set, patch, or clear file tree note badges | [§7.4 · 9](ui-contributions.md) |
| `host.sidebar.get_file_icons` | `ui:sidebar` (default-allowed) | Read custom file/folder icons and preferences for a notebook | [§7.4 · 9.1](ui-contributions.md) |
| `host.sidebar.set_file_icons` | `ui:sidebar` (default-allowed) + `contributes.file_icons` | Update or clear custom file/folder/notebook icons and preferences | [§7.4 · 9.1](ui-contributions.md) |
| `host.editor.selection` | `editor:read` | Read the active editor selection, note path, and focus flag | [§7.4 · 10](ui-contributions.md) |
| `host.editor.insert` | `editor:write` | Parse Markdown and insert or replace at the active selection | [§7.4 · 11](ui-contributions.md) |
<!-- host-methods-table:end -->

> **Removed methods**: Legacy RPCs `host.ui.editor_background`, `host.ui.set_editor_background`, `host.ui.mindmap_state`, and `host.ui.set_mindmap` have been removed and now return `-32601` (superseded by `host.ui.backdrop` / `host.ui.set_backdrop` and `contributes.block_decorators`).

---

### 4.2 Plugin -> Host RPC Methods

Plugins may call `host.*` while handling `plugin.initialize`, `plugin.render`, `plugin.action`, `plugin.command`, or `plugin.event` (max 8 concurrent in-flight calls; unavailable during `plugin.shutdown`).

Common error codes: `-32001` missing permission, `-32002` storage failure or `> 8 MiB`, `-32003` note listing failure / unregistered notebook, `-32004` filesystem error / symlink / `> 16 MiB`, `-32005` host backend unavailable or failed, `-32006` dirty editor / virtual preview / revision conflict / rate limit, `-32601` unknown method, `-32602` invalid params.

Note path rules: case-insensitive `.md`, `.markdown`, `.mdown`, `.mkd`, `.mkdn`, `.mdwn`, `.mdtxt`, `.mdtext`, `.mdx`. Explicit `notebook_root` must match a registered notebook; omitting uses runtime root. `host.notes.list` skips dot-prefixed entries and non-regular files; `read`/`write` reject `:`, dot segments, escapes, symlinks, and files `> 16 MiB`.

1. **`host.storage.get`** (no permission): `{"key": "config"}` -> `{"value", "found"}` or `{}` -> `{"data": {...}}`.
2. **`host.storage.set`** / **`host.storage.set_many`** (no permission; 8 MiB cap): `{"key", "value"}` or `{"values": {...}}` (1–64 non-blank keys) -> `{"ok": true}`. Preserves raw JSON numbers.
3. **`host.notes.active`** (`"notes:read"`): `{}` -> `{"path": "welcome.md"}`.
4. **`host.notebooks.list`** / **`host.notebooks.data_dir`** (`"notes:read"`):
   - `host.notebooks.list {}` -> `{"notebooks": [{"id", "path", "name", "active"}]}` (`id` is a host-managed durable identifier preserved across moves/renames).
   - `host.notebooks.data_dir` (`{"id" | "path" | "notebook" | "notebook_id" | "notebook_root"}`, defaulting to the active notebook when omitted) creates and returns `{"id", "path"}` pointing to `<data_dir>/notebooks/<id>/` (stable across renames; cleaned by the host when removed or missing for over 30 days).
5. **`host.notes.list`** (`"notes:read"`): `{"notebook_root"?}` -> `{"notes": [{"path", "size", "mod_time"}]}` (returns `{"notes": []}` when no notebook is active; unregistered notebook returns `-32003`).
6. **`host.notes.read`** (`"notes:read"`): `{"notebook_root"?, "path"}` -> `{"path", "content", "revision"}` (`revision` is 64-char lowercase SHA-256).
7. **`host.notes.write`** (`"notes:write"`): `{"notebook_root"?, "path", "content", "expected_revision"?}` -> `{"ok": true, "changed": true|false}`. `content` must be a string (max 16 MiB). Checks dirty editor / virtual preview (`-32006`), then byte equality (`changed: false`), then `expected_revision` (`""` requires non-existent file; SHA-256 requires match, else `-32006`). Does not return new `revision`—re-read before chained writes.
8. **`host.notes.trash`** (`"notes:write"`): `{"notebook_root"?, "path", "expected_revision"}` -> `{"ok": true, "trashed": true|false}`. `expected_revision` is mandatory (SHA-256 of current file bytes). Allows non-Markdown regular files inside the notebook (still rejects dot segments and symlinks). Use `host.system.trash` when user confirmation is desired.
9. **`host.notes.open`** (`"notes:read"`): `{"notebook_root"?, "path"}` -> `{"ok": true, "path"}` (headless CLI returns `-32005`).
10. **`host.action.progress`** (no permission; check `"action_progress"` capability): prefer JSON-RPC notification (no `id`) so it does not consume an in-flight slot. Supports `{"done", "total"}` or `{"progress": 0..1}`, optional `label` (`<= 32` chars), and with `"progress_bar"` optional `channel`, `tone`, and `running`.
11. **`host.perf.snapshot`** (`"notes:read"`): `{"refresh_disk": false}` -> host CPU, RSS, and 7 disk categories (`notes`, `assets`, `history`, `recovery`, `ai_sessions`, `logs`, `config`).
12. **`host.store.changelog`** (no permission; check `"store_changelog"` capability): `{"id": "example-sync", "locale": "en-US", "limit": 10}` (`limit` default 10, max 20). Resolves static JSON API → GitHub Releases → inline index changelog → local `CHANGELOG.md` (local fallback populates `markdown` and may have `releases: null`).
13. **`host.log`** (no permission; `-32601` on older hosts): `{"entries": [{"level", "event", "notebook_root", "path", "protocol", "detail"}]}` (max 200 entries, 400 bytes per field). Never log note bodies or secrets.
14. **`host.fs.*`** (`"system:fs"`, host absolute paths only; resolves symlinks before checking scope via `os.Root`. Authorized scope covers `data_dir`, `downloads_dir`, `granted-paths.json` entries, and registered notebooks when also holding `"notes:read"` for reads or `"notes:write"` for writes. Protected host files inside `data_dir`—`granted-paths.json`, `scheduler.json`, `state.json`—cannot be modified directly. Note that `host.system.open_dialog`/`save_dialog` only return selected paths; file grants are registered via form pickers or menubar drops):
    - `host.fs.list_grants` (`{}`): `{"grants": [...], "data_dir", "downloads_dir", "notebooks": [{"path", "writable"}]}`.
    - `host.fs.stat` (`{"path"}`): `{"exists", "path", "name", "is_dir", "is_regular", "size", "mod_time", "mod_time_ms", "is_icloud_placeholder"}`.
    - `host.fs.list` (`{"path", "recursive"}`, max 10000 entries): `{"path", "entries": [...], "truncated": bool}` (recursive entries include `rel_path`).
    - `host.fs.read` (`{"path", "encoding": "utf8"|"base64", "max_bytes"}`, default 16 MiB, host cap 64 MiB): `{"path", "encoding", "size", "content"|"content_base64"}`.
    - `host.fs.write` (`{"path", "content"|"content_base64", "atomic", "mkdir_parents"}`, host cap 64 MiB).
    - `host.fs.mkdir` (`{"path", "recursive"|"parents"}`), `host.fs.delete` (`{"path", "recursive"}`), `host.fs.rename` (`{"from", "to", "overwrite", "mkdir_parents"}`; falls back to copy+delete across different grant roots), `host.fs.copy` (`{"from", "to", "overwrite", "mkdir_parents"}`; host-side streaming atomic copy of regular non-symlink files bypassing Wasm memory, rejects identical paths and grant roots themselves, returns `{"ok": true, "from", "to", "size"}`), `host.fs.atomic_swap` (`{"path_a", "path_b"}`; may succeed with `atomic: false` if OS atomic exchange is unavailable).
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
    - Block combines the static and runtime lists of all enabled plugins. Runtime lists are held in memory and cleared on disable, uninstall, plugin update, or Block restart. See [Host UI Contributions](ui-contributions.md) for directory name rules.

### 4.3 System Integration

Desktop hosts advertise `"system_integration"` in `ui_capabilities`. Call `host.system.capabilities {}` (no permission) to query live boolean support for `notify`, `notify_click`, `secrets`, `open_url`, `reveal`, `open_dialog`, `save_dialog`, `clipboard_read`, `clipboard_write`, `trash`, `badge`, `wake`, `network`, and `focus`.

| Method | Permission | Contract |
| --- | --- | --- |
| `host.system.notify` | Default-allowed | `{"title", "body", "tone", "notebook_root"?, "rel_path"?, "timeout_ms"?, "dedup_key"?}`. Sends OS native notification (`title <= 80`, `body <= 240`). `notebook_root`/`rel_path` require `notes:read`. Rate limit: 5 distinct notifications per 10s (`-32006`); 2-minute dedup window (`suppressed: true`). |
| `host.system.open_url` | `system:open` | `{"url": "https://example.com"}` (HTTP/HTTPS without embedded credentials). |
| `host.system.reveal` | `system:open`, `notes:read` | `{"rel_path", "notebook_root"?}`: reveals file in OS file manager. |
| `host.system.open_dialog`, `host.system.save_dialog` | `system:dialogs` | Optional `title`, `default_path`, `extensions` (plus `directory`, `multiple` for open). Returns `{"ok": true, "paths": [...], "canceled": false}`. Canceled/timed-out actions return `-32007`. |
| `host.clipboard.read`, `host.clipboard.write` | `system:clipboard_read` / `write` | UTF-8 text up to 64 KiB. |
| `host.system.trash` | `system:trash`, `notes:write` | Regular file only; shows native confirmation dialog before moving to OS Trash. |
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

<!-- ai-guide:skip-start -->
---

Next: [4. Host UI Contributions →](ui-contributions.md)
<!-- ai-guide:skip-end -->
