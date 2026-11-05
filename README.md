# Block

文字之隅，落笔即成。

官网：https://block.cooool.top

Website: https://block.cooool.top

**Block**（仓库名 `block`）是一款使用 Rust 与 GPUI 构建的原生 macOS 块级 Markdown 笔记应用，同时提供基于 WebAssembly + WebGPU/WebGL2 的浏览器端编辑器。它直接读写本地文件夹中的 `.md` 文件，不绑定私有格式，兼顾即时排版的阅读感与纯文本的可迁移性。

> **Block** is a native macOS block-based Markdown notebook built in Rust on top of GPUI, with a companion WebAssembly editor. It works directly over plain local `.md` folders with zero lock-in.

## 核心特性

- **本地文件夹直读**：打开任意本地文件夹即可按树状结构浏览与搜索 Markdown 笔记（文件夹优先、自然序排列），最近打开的工作区保存在 `~/Library/Application Support/Block/state.json`。
- **块级直觉编辑**：新建笔记（默认 `未命名.md`，重名自动递增），将段落、H1–H6 标题、无序/有序列表、待办清单、引用、分割线、代码块、表格与图片作为结构化块编辑；行首输入 `# `、`- `、`1. `、`> ` 或 `[] ` 即时转换块类型，编辑后自动防抖写回磁盘。
- **跨块选区与把手操作**：悬停或聚焦块时显示块把手，`+` 在下方插入空块，拖拽手柄重排顺序；支持跨块文本选区、复制剪切、加粗/斜体/删除线/行内代码、批注与完整撤销/重做栈。
- **代码块与结构化表格**：围栏代码块使用 `tree-sitter` 渲染语法高亮（支持 Rust、JavaScript/TypeScript、JSON、Python、CSS、HTML、Markdown 等）及 Mermaid / LaTeX 预览；管道表格与受支持的 HTML 表格提供单元格级光标移动（`Tab` / `Shift+Tab`）、行列拖拽排序与列宽调整。
- **图片导出**：支持将整篇笔记（`Block → 导出图片`）或所选文字导出为 2x 高清 PNG 长图（720px 标准正文列宽，后台完成排版与光栅化）。
- **原子保存与草稿恢复**：文件保存采用原子替换并检测外部改动；遇到保存冲突或原文件被删除时，自动将未保存内容检查点写入 `~/Library/Application Support/Block/Recovery`，可在空编辑器中一键恢复。

正文默认采用系统 UI 字体与 `PingFang SC`、720px 标准阅读列宽。长文档通过持久化快照与可视行按需渲染保持流畅响应。交互核验与已知限制详见 [docs/interaction-audit.md](docs/interaction-audit.md)。

## 构建与运行

### 环境要求

- macOS 12.0+（Apple Silicon / Intel）
- Rust stable 工具链（Edition 2024）
- [Bun](https://bun.sh/)（用于执行构建与打包脚本）

### 本地开发运行

```sh
cargo run
```

### 构建并安装 macOS Release 应用

```sh
bun run build
```

该命令会编译 `--locked --release` 二进制，打包为 `dist/Block.app`（Bundle ID：`dev.eachann.block`），执行 ad-hoc 签名并校验系统动态库依赖，随后替换 `/Applications/Block.app` 并启动应用（若旧进程正在运行会先请求优雅退出）。

仅构建 `dist/Block.app` 而不覆盖 `/Applications`（例如在 CI 环境中）：

```sh
bun run build:ci
```

### 构建 WebAssembly 编辑器

需先安装 `wasm32-unknown-unknown` target、`wasm-bindgen-cli` 与 `wasm-opt`（`binaryen`）：

```sh
bun run build:web
```

## 开源许可与第三方致谢

本项目采用 [Apache License 2.0](LICENSE) 开源许可（Copyright 2026 eachann1024）。使用或修改本项目时请保留 NOTICE，并标明来自 Block / eachann1024。

When using or modifying this project, keep NOTICE and credit Block / eachann1024.

仓库中包含的第三方组件与字体许可说明详见 [NOTICE](NOTICE) 及 `web/vendor/gpui-pre-wgpu/LICENSE-APACHE`。
