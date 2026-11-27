<!-- ai-guide:skip-start -->
# Local Protocol Check & App Store Publishing

> [← Language Extensions](languages.md) · [Docs Home](../README.md) · [Self-Contained AI Spec](../plugin-dev-for-ai.md)

---
<!-- ai-guide:skip-end -->

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
