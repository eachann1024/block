//! Preloaded Chinese demo note for the Yu web hero stage.

pub const DEMO_NOTE: &str = r#"# Yu · 文字之隅，落笔即成

欢迎体验直接在浏览器中运行的 **Yu** 真实编辑器（基于 Rust + GPUI 编译为 WebAssembly）。将鼠标悬停在任意段落旁，即可唤出左侧的拖拽把手；空行旁还会出现 + 插入按钮。

## 块把手、拖拽重排与块转换

点击标题左侧的折叠箭头可以收起整节内容；按住左侧拖拽把手上下拖动即可重排块顺序；在空行输入 / 可快速插入或转换块类型。

- [x] 悬停段落显示左侧拖拽把手，空行显示 + 插入按钮
- [x] 按住拖拽把手移动任意块，实时预览落点指示线并完成重排
- [ ] 在空行输入 / 打开斜杠菜单，转换为标题、引用、待办或代码块

> 写作应当像在纸面推演一样克制而顺手：结构化块与纯文本 Markdown 实时同步，不打断思考流。

## 快捷键与结构化块一览

| 交互动作 | 触发方式 | 效果说明 |
| --- | --- | --- |
| 插入块 | 点击空行旁的 + 按钮 | 弹出菜单并插入新块 |
| 拖拽重排 | 按住左侧拖拽把手上下拖动 | 跨段落调整文档结构顺序 |
| 斜杠指令 | 在空行输入 / | 快速插入标题、表格、代码块与任务项 |
| 删除至行首 | ⌘ + Backspace | 删除光标到行首的内容（⌘← / ⌘→ 跳到行首 / 行尾） |
| 切换块类型 | ⌥⌘1 … ⌥⌘9 | 标题、待办、列表、代码块与引用 |
| 查找 | ⌘F | 在笔记中查找并高亮 |
| 生成图片 | ⇧⌘E | 把笔记导出为图片、PDF、Word、Markdown 或 HTML |

```rust
fn reorder_block(blocks: &mut Vec<Block>, from: usize, to: usize) {
    if from < blocks.len() && to <= blocks.len() {
        let item = blocks.remove(from);
        blocks.insert(to.min(blocks.len()), item);
    }
}
```
"#;
