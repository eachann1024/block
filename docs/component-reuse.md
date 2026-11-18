# GPUI 组件复用决策

本次编辑器修正保留现有 Markdown 文档、跨块事务和草稿持久化，复用与 GPUI 0.2.2 兼容的组件。源码调查只能确认 API 和依赖声明；编译及真实 macOS 编辑体验另行记录于 [交互验收](interaction-audit.md)。

| 项目 | 已核实事实 | 采用范围 |
| --- | --- | --- |
| [Wu](https://github.com/dstrml/wu) | 旧地址 farshed/wu 转向此仓库；editor crate 使用 GPL-3.0-or-later，依赖多个 workspace crate 和自带 GPUI | 参考交互和架构，不复制 editor 实现 |
| [gpui-component 0.5.1](https://github.com/longbridge/gpui-component/tree/v0.5.1) | Apache-2.0；Rust 2024；依赖 GPUI 0.2.2。0.6.0 已改用 gpui-pre | 固定 `=0.5.1`；复用语法高亮和 Tooltip。表格通过本应用的结构化模型与布局适配接入统一输入链 |
| [gpui-flowtext](https://github.com/realartists-gitmo/gpui-flowtext/tree/058a0d1edcd190a3abac9212edb85475d2d7a7a6) | MIT；有跨段落选区、撤销、富文本和表格编辑。固定 revision 的输入链走 `on_key_down`/`key_char`，未找到 IME marked text/UTF-16 协议接入；serializer/event sink 声明尚未接入实际编辑与恢复链 | 暂不替换文档内核。Markdown 适配可自建，但还需补中文 IME、保存事件和恢复格式，不能视为可直接接入的完整编辑器 |
| [guise](https://github.com/wess/guise) | MIT；MarkdownEditor 提供 live preview，表格仍为源码；其编辑器的中文 IME 输入协议未确认 | 暂不采用为正文编辑器 |

`gpui-component` 的 `TextView::markdown/html` 是渲染组件，即使可以选择文字也不是可编辑块文档。`InputState` 有独立字符串、选区和历史，不能直接让每块各持一个状态来代替全文跨块事务。表格组件提供列布局和单元格渲染接口，文档语义、保存及撤销仍需接回本应用。

0.5.1 的高亮接口为 `SyntaxHighlighter::new(language)`、`update(edit, &rope)`、`styles(&range, &theme)`。接入时以固定版本源码为准，不照搬新版本 API。

对 gpui-flowtext 的深入核对固定在表中 revision，结论来自源码而非宿主运行：

- [输入处理](https://github.com/realartists-gitmo/gpui-flowtext/blob/058a0d1edcd190a3abac9212edb85475d2d7a7a6/src/rich_text/editor/action_handlers.rs) 通过 key_char 插入，未找到已接入的 IME 组合输入协议。
- [表格布局](https://github.com/realartists-gitmo/gpui-flowtext/blob/058a0d1edcd190a3abac9212edb85475d2d7a7a6/src/rich_text/layout/table_layout.rs) 使用 col_span，未使用 row_span；嵌套表格命中和合并网格增删仍有缺口。
- 文档节点只有 Paragraph/Image/Equation/Table。Markdown 标题、列表、任务、代码语言、链接目的地和未知源码需附加语义，并使其跟随分割、合并、复制及撤销变化。
- 公开的段落选区和 rich fragment 可用于导出，但表格单元格选择另有私有状态，不能直接获得统一的完整选区。
- [恢复写盘](https://github.com/realartists-gitmo/gpui-flowtext/blob/058a0d1edcd190a3abac9212edb85475d2d7a7a6/src/rich_text/editor/recovery.rs) 使用原生 GPTX。宿主可观察 edit_generation/document 并接管 Markdown 保存，但不能把 .md 路径直接交给内置保存；还需要接管 dirty 状态和恢复元数据。

因此不把“缺少现成 Markdown 导入导出”作为唯一排除依据。实际迁移还需重建当前已有的 IME、语义事务和持久化能力，本轮保留现有内核，仅参考其基础表格设计。

所有表格与内联格式转换都必须保留可恢复的原始内容。对尚不能表达的结构应明确保留源码，不能把去掉标签后的文字误称为完成了可视化编辑。选区、拖拽、hover、中文输入、宽度切换与保存往返，最终均需要真实宿主操作确认。
