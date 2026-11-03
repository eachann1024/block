# thinkly-fast

A small macOS notes app built with GPUI and Rust. A slim take on Thinkly.

- Open a folder and browse its Markdown files as a tree (folders first, then
  case-insensitive natural order). The last folder is remembered in
  `~/Library/Application Support/thinkly-fast/state.json`.
- Create a note (`未命名.md`, numbered on collision) and edit it as blocks:
  paragraphs, H1–H3, lists, quotes and to-dos. Typing `# `, `- `, `1. `,
  `> ` or `[] ` at the start of a line converts the block. Edits save back to
  the file after a short debounce.
- Hover a block for its handle: `+` inserts an empty block below, the grip
  drags to reorder. Select text for bold, italic, strike, inline code, heading
  cycling and export.
- Export the whole note (Thinkly → 导出图片) or the selected block (the `图`
  button) to a 720px-wide PNG at 2x.

One font (the system UI font with PingFang SC) and one width (720px).

```sh
cargo run
```

Release 包只通过这一条命令构建并安装：

```sh
bun run build
```

它编译 locked release，打包为 `Thinkly Fast.app`（`dev.eachann.thinkly-fast`），做 ad-hoc 签名，替换 `/Applications/Thinkly Fast.app` 后启动。已在运行的本应用会先被要求退出；超时仍在运行则停止并报错，不强制结束。`/Applications/Thinkly.app` 不在范围内。
