# Block Documentation

> Local-first Markdown block note-taking app · Ocean Paper design system · Open plugin ecosystem

This directory is the single source of truth for **Block** documentation, serving both GitHub browsing and the online documentation site at [docs.block.cooool.top](https://docs.block.cooool.top/) (automatically built and deployed via VitePress when `docs/**` is pushed; Chinese documentation: [中文文档](zh/README.md)).

---

## Documentation Index

### Core References
- **[Design Language & Tokens (DESIGN.md)](../DESIGN.md)**: Ocean Paper color palette, typography and geometry scale, Pluck Switch controls, and Scheme A two-tone icon system.
- **[Project Overview (README.md)](../README.md)** · **[中文 README](../README.zh-CN.md)**: Features, cross-platform downloads, and Apache 2.0 license notice.
- **[Privacy Policy](https://block.cooool.top/en/privacy/)**

### Plugin Developer Documentation (`docs/plugins/`)
External plugins live in standalone public GitHub repositories (add the `blockmd-plugin` topic; the store crawler prefers the latest published GitHub Release and falls back to a commit-pinned default-branch archive when no suitable Release exists). No PR to the main repository is required to join automatic store crawling (top 100 repositories sorted by stars, plus direct desktop installation from local directories or repository slugs):

| Chapter | File | Summary |
| --- | --- | --- |
| 1. Overview & AI Guide | [plugins/README.md](plugins/README.md) | No-PR publishing boundary, 3 development paths, 8 hard rules, validation checklist, sandbox boundaries, and runtime migration |
| 2. Manifest & Permissions | [plugins/manifest.md](plugins/manifest.md) | Directory layout, `manifest.json` required and allowlist fields, 30 valid permissions (15 sensitive across 5 groups + 3 silent events + 12 default-allowed), and `permission_reasons` |
| 3. Protocol, UI & SDK | [plugins/protocol-and-ui.md](plugins/protocol-and-ui.md) | Bidirectional JSON-RPC 2.0, [Host Method Index](plugins/protocol-and-ui.md#host-method-index), 16 `UIItem` controls, bilingual `block-sdk.js` starter skeleton, and platform behavior |
| 4. Host UI Contributions | [plugins/ui-contributions.md](plugins/ui-contributions.md) | Multi-surface model, `manifest.contributes` fields, `plugin.command` / workspace events, and host UI RPC methods |
| 5. Language Extensions | [plugins/languages.md](plugins/languages.md) | Zero-process, zero-permission declarative syntax highlighting (`contributes.languages` + `grammars/*.json`) and file associations |
| 6. Check & Store | [plugins/check-and-store.md](plugins/check-and-store.md) | `block-plugin-check` usage and coverage limits, bilingual gate, `blockmd-plugin` crawling rules (top 100 cap), and offline catalog fallback |

### Self-Contained AI Specification
- **[plugin-dev-for-ai.md](plugin-dev-for-ai.md)** · **[中文 AI 全量规范](zh/plugin-dev-for-ai.md)**: Single-file specifications automatically composed by `bun run docs:sync` from the modular chapters above. Do not edit generated files directly; edit the source chapters and run `bun run docs:sync`.
