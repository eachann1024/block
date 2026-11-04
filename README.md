# Yu

Yu 是 macOS 原生的 Markdown 块编辑器（基于 GPUI 与 Rust 构建）。文字之隅，落笔即成——打开、输入、拖动，都在毫秒内完成。

- Open a folder and browse its Markdown files as a tree (folders first, then
  case-insensitive natural order). The last folder is remembered in
  `~/Library/Application Support/thinkly-fast/state.json`.
- Create a note (`未命名.md`, with a unique fallback name on collision) and edit it as blocks:
  paragraphs, H1–H3, lists, quotes and to-dos. Typing `# `, `- `, `1. `,
  `> ` or `[] ` at the start of a line converts the block. Edits save back to
  the file after a short debounce.
- Hover a block for its handle: `+` inserts an empty block below, the grip
  drags to reorder. Select text for bold, italic, strike, inline code, heading
  cycling and export.
- Select text across blocks, copy/cut it, and undo or redo editing transactions.
  Wrapped text, headings and input-method ranges share the text layout.
- Fenced code keeps literal newlines and uses gpui-component's tree-sitter
  highlighting for Rust, JavaScript/TypeScript, JSON, Python, CSS, HTML and Markdown.
  Markdown import also retains H4–H6, setext headings and links.
- Pipe tables and supported HTML tables use structured cells with shared caret,
  selection, undo and save state. HTML cell edits retain the surrounding source,
  column widths and merged-cell attributes. Tab and Shift-Tab move between cells.
  Unsupported Markdown or HTML structures remain editable source with a visible
  explanation; arbitrary browser CSS is not supported.
- The persistent width switch chooses a standard 720px body or the available
  editor width. Block menus provide insertion, duplication, movement and deletion.
  Mermaid and math entries currently create code blocks, not rendered diagrams.
- Export the whole note (Yu → 导出图片) or the exact selected text (the `图`
  button) to a PNG at 2x with a 720px text column. Layout, rasterization and
  writing run in the background. Export accepts at most 256 blocks, 128 KiB of
  text and metadata, 8,192 formatting ranges and 16 megapixels; larger requests
  fail explicitly. The confirmed output path is used unchanged.
- Saves use an atomic replacement and check for external changes. Failed or
  conflicting saves retain a recovery draft under
  `~/Library/Application Support/Thinkly Fast/Recovery`. The empty editor can
  recover the latest draft, including one whose original file was deleted.
  “Keep draft and close” checkpoints unsaved work before closing, so a conflicting
  or deleted original does not prevent opening another document.

The main text uses the system UI font with PingFang SC and a 720px column.
Large documents use persistent model snapshots and visible-row rendering.
Million-line parsing, full serialization and single very long paragraphs still
have substantial costs; background work does not guarantee a 50ms total latency.

Verification methods, measured results and remaining gaps are recorded in
[the interaction audit](docs/interaction-audit.md). GPUI simulation and isolated
filesystem checks do not establish native macOS IME, dialog or visual behavior.

```sh
cargo run
```

Release 包只通过这一条命令构建并安装：

```sh
bun run build
```

它编译 locked release，打包为 `Thinkly Fast.app`（`dev.eachann.thinkly-fast`），做 ad-hoc 签名，替换 `/Applications/Thinkly Fast.app` 后启动。已在运行的本应用会先被要求退出；超时仍在运行则停止并报错，不强制结束。`/Applications/Thinkly.app` 不在范围内。
