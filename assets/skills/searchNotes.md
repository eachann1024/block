# 搜索与读取

## 适用

- 在本轮授权笔记本内查找页面、定位关键词或读取其他笔记内容。

## 执行

- 页面已明确时直接调用 `readPage`（传 `pageId`，`notebookRoot` 省略或传 `current`）；大文档或输出被截断时配合 `startLine`（从 1 开始）与 `lineCount` 分段读完。
- 页面不明确时调用 `searchNotes` 按关键词检索，根据返回的 `pageId`、`line` 和 `snippet` 定位后再用 `readPage` 读取对应行段；需要浏览目录时调用 `listPages`。
- 不为当前页局部润色任务额外搜索。
- 回答仅引用实际读到的内容，不把未读正文或截断片段当作全文已读。
