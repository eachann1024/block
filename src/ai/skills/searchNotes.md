# 搜索与读取

## 适用

- 跨笔记本、目录和页面查找内容，或目标页不明确需要先定位；来源引用不限制读取范围。

## 执行

- 页面已明确时直接 `readPage`，带真实 notebookId 和 pageId。目录来源用 listFolder 分页枚举，不能把索引或未读正文当作全文已读。
- 否则调用一次 `searchNotes`，只读回答所需页面。
- 不为当前页润色任务搜索。
- 搜索结果的 location 标识命中段落；随后 readPage 时传入其 blockId，无 ID 时传 blockIndex。
- 回答引用笔记时，使用实际支持该结论的 blocks 条目返回的 citationHref 生成 Markdown 链接，标签为“文件名 · locationLabel”；只引用实际读到的段落，不编造块 ID 或链接。
