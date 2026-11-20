# Property List

Block 官方语言扩展插件，在打开 macOS 项目文件夹或编写代码块时为 `.plist` 属性列表文件提供语法高亮。

## 支持的格式

- **XML Property List**：专门区分 `<key>` 键名、`<string>` 字符串、`<integer>` / `<real>` 数值、`<true/>` / `<false/>` 布尔值、`<date>` / `<data>` 数据、XML 声明与注释。
- **OpenStep ASCII Property List**：支持 `key = "value";` 键值对、十六进制 `<data>` 字面量、数字及 `/* ... */`、`//` 注释。

## 匹配文件与代码块

- 文件匹配：`*.plist`、`Info.plist`
- 代码块标签：`plist`、`property-list`、`apple-plist`

## 说明

本插件为纯声明式语言扩展，不启动后台子进程，无需任何系统或笔记读写权限。
