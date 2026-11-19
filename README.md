# Block

使用 MyGo 构建的跨平台原生块级 Markdown 笔记应用，支持 macOS、Windows 与 Linux。它直接读写本地文件夹中的 `.md` 文件，不绑定私有格式，兼顾即时排版的阅读感与纯文本的可迁移性。

- **官方网站（中文）**：[https://block.cooool.top/](https://block.cooool.top/)
- **Official Website (English)**：[https://block.cooool.top/en/](https://block.cooool.top/en/)
- **主流 Markdown 与块笔记横向对比**：[https://block.cooool.top/#compare](https://block.cooool.top/#compare) · [Comparison](https://block.cooool.top/en/#compare)
- [插件开发指南（Plugin Development Guide）](docs/plugin-dev-for-ai.md)

## 核心特性

- **本地文件夹直读**：打开任意本地文件夹即可按树状结构浏览与搜索 Markdown 笔记（文件夹优先、自然序排列），最近打开的工作区保存在应用数据目录的 `state.json`。
- **块级直觉编辑**：新建笔记（默认 `未命名.md`，重名自动递增），将段落、H1–H6 标题、无序/有序列表、待办清单、引用、分割线、代码块、表格与图片作为结构化块编辑；行首输入 `# `、`- `、`1. `、`> ` 或 `[] ` 即时转换块类型，编辑后自动防抖写回磁盘。
- **跨块选区与把手操作**：悬停或聚焦块时显示块把手，`+` 在下方插入空块，拖拽手柄重排顺序；支持跨块文本选区、复制剪切、加粗/斜体/删除线/行内代码、批注与完整撤销/重做栈。
- **代码块与结构化表格**：围栏代码块支持语法高亮、Mermaid 与 LaTeX 预览；管道表格与受支持的 HTML 表格提供单元格级光标移动（`Tab` / `Shift+Tab`）、行列拖拽排序与列宽调整。
- **图片与文档导出**：支持将笔记或所选文字导出为高清 PNG、HTML、Markdown 等格式（720px 标准正文列宽）。
- **原子保存与草稿恢复**：文件保存采用原子替换并检测外部改动；遇到保存冲突或原文件被删除时，自动将未保存内容检查点写入 `Recovery` 目录，可在空编辑器中一键恢复。


## 开源许可与第三方致谢

本项目采用 [Apache License 2.0](LICENSE) 开源许可（Copyright 2026 eachann1024）。使用或修改本项目时请保留 NOTICE，并标明来自 Block / eachann1024。

When using or modifying this project, keep NOTICE and credit Block / eachann1024.

第三方组件与字体许可说明详见 [NOTICE](NOTICE) 与 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
