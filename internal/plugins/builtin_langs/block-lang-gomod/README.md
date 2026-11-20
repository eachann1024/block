# Go Modules

Block 官方语言扩展插件，在打开 Go 项目文件夹或编写代码块时提供语法高亮。

## 支持的文件

- `go.mod` / `*.mod`：高亮 `module`、`go`、`toolchain`、`require`、`replace`、`exclude`、`retract`、`use`、`tool` 等指令、模块路径、语义化版本号、`=>` 替换符与 `// indirect` 标记。
- `go.work` / `*.work`：与 `go.mod` 共享工作区指令与路径高亮。
- `go.sum` / `go.work.sum` / `*.sum`：区分模块路径、版本号（含 `/go.mod`）与 `h1:` 校验哈希。

## 代码块标识

在 Markdown 笔记中可使用以下语言标签启用高亮：

- `gomod`、`go.mod`、`go.work`、`gowork`、`mod`
- `gosum`、`go.sum`、`sum`

## 说明

本插件为纯声明式语言扩展，不启动后台子进程，无需任何系统或笔记读写权限。
