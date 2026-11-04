//! Preloaded Chinese demo note for the Thinkly Fast web hero stage.

pub const DEMO_NOTE: &str = r#"# Thinkly Fast · 原生块级笔记

欢迎体验直接在浏览器中运行的 **Thinkly Fast** 真实编辑器（基于 Rust + GPUI 编译为 WebAssembly）。将鼠标悬停在任意段落旁，即可唤出左侧的 `+` 与 `⠿` 块把手。

## 块把手、拖拽重排与块转换

点击标题左侧的折叠箭头可以收起整节内容；按住 `⠿` 把手上下拖动即可重排块顺序，点击 `⠿` 或在行首输入 `/` 可快速切换块类型。

- [x] 悬停段落显示 `+` 插入按钮与 `⠿` 块操作把手
- [x] 按住 `⠿` 拖拽任意块，实时预览落点指示线并完成重排
- [ ] 点击 `⠿` 打开块菜单，将段落转换为标题、引用、待办或代码块

> 写作应当像在纸面推演一样克制而顺手：结构化块与纯文本 Markdown 实时同步，不打断思考流。

## 快捷键与结构化块一览

| 交互动作 | 触发方式 | 效果说明 |
| --- | --- | --- |
| 唤出块菜单 | 点击左侧 `⠿` 把手 | 转换块类型、复制或删除当前块 |
| 拖拽重排 | 按住 `⠿` 上下拖动 | 跨段落调整文档结构顺序 |
| 斜杠指令 | 行首或句中输入 `/` | 快速插入标题、表格、代码块与任务项 |

```rust
fn reorder_block(blocks: &mut Vec<Block>, from: usize, to: usize) {
    if from < blocks.len() && to <= blocks.len() {
        let item = blocks.remove(from);
        blocks.insert(to.min(blocks.len()), item);
    }
}
```
"#;
