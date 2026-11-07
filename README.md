# Block

文字之隅，落笔即成。

官网：https&#58;&#47;&#47;block&#46;cooool&#46;top

Website&#58; https&#58;&#47;&#47;block&#46;cooool&#46;top

<strong>Block</strong>（仓库名&#32;<code>block</code>）是一款使用 Rust 与 GPUI 构建的原生 macOS 块级 Markdown 笔记应用，同时提供基于 WebAssembly &#43; WebGPU&#47;WebGL2 的浏览器端编辑器。它直接读写本地文件夹中的&#32;<code>&#46;md</code>&#32;文件，不绑定私有格式，兼顾即时排版的阅读感与纯文本的可迁移性。

> <strong>Block</strong>&#32;is a native macOS block&#45;based Markdown notebook built in Rust on top of GPUI&#44; with a companion WebAssembly editor&#46; It works directly over plain local&#32;<code>&#46;md</code>&#32;folders with zero lock&#45;in&#46;

## 核心特性

- <strong>本地文件夹直读</strong>：打开任意本地文件夹即可按树状结构浏览与搜索 Markdown 笔记（文件夹优先、自然序排列），最近打开的工作区保存在&#32;<code>&#126;&#47;Library&#47;Application Support&#47;Block&#47;state&#46;json</code>。
- <strong>块级直觉编辑</strong>：新建笔记（默认&#32;<code>未命名&#46;md</code>，重名自动递增），将段落、H1–H6 标题、无序&#47;有序列表、待办清单、引用、分割线、代码块、表格与图片作为结构化块编辑；行首输入&#32;<code>&#35;&#32;</code>、<code>&#45;&#32;</code>、<code>1&#46;&#32;</code>、<code>&#62;&#32;</code>&#32;或&#32;<code>&#91;&#93;&#32;</code>&#32;即时转换块类型，编辑后自动防抖写回磁盘。
- <strong>跨块选区与把手操作</strong>：悬停或聚焦块时显示块把手，<code>&#43;</code>&#32;在下方插入空块，拖拽手柄重排顺序；支持跨块文本选区、复制剪切、加粗&#47;斜体&#47;删除线&#47;行内代码、批注与完整撤销&#47;重做栈。
- <strong>代码块与结构化表格</strong>：围栏代码块使用&#32;<code>tree&#45;sitter</code>&#32;渲染语法高亮（支持 Rust、JavaScript&#47;TypeScript、JSON、Python、CSS、HTML、Markdown 等）及 Mermaid &#47; LaTeX 预览；管道表格与受支持的 HTML 表格提供单元格级光标移动（<code>Tab</code>&#32;&#47;&#32;<code>Shift&#43;Tab</code>）、行列拖拽排序与列宽调整。
- <strong>图片导出</strong>：支持将整篇笔记（<code>Block → 导出图片</code>）或所选文字导出为 2x 高清 PNG 长图（720px 标准正文列宽，后台完成排版与光栅化）。
- <strong>原子保存与草稿恢复</strong>：文件保存采用原子替换并检测外部改动；遇到保存冲突或原文件被删除时，自动将未保存内容检查点写入&#32;<code>&#126;&#47;Library&#47;Application Support&#47;Block&#47;Recovery</code>，可在空编辑器中一键恢复。

正文默认采用系统 UI 字体与&#32;<code>PingFang SC</code>、720px 标准阅读列宽。长文档通过持久化快照与可视行按需渲染保持流畅响应。交互核验与已知限制详见&#32;[docs&#47;interaction&#45;audit&#46;md](docs/interaction-audit.md)。

## 构建与运行

### 环境要求

- macOS 12&#46;0&#43;（Apple Silicon &#47; Intel）
- Rust stable 工具链（Edition 2024）
- [Bun](https://bun.sh/)（用于执行构建与打包脚本）

### 本地开发运行

```sh
cargo run
```

### 冒烟自检

<code>cargo run &#45;&#45; &#45;&#45;smoke</code>&#32;会自动启动常驻后台 worker，并将标准自检加入串行队列。每次自检使用重置后的独立沙箱，笔记和偏好与日常使用隔离；任务结束后 worker 保持运行。

- <code>cargo run &#45;&#45; &#45;&#45;smoke&#45;status</code>：查看 worker 状态及队列。
- <code>cargo run &#45;&#45; &#45;&#45;smoke&#45;stop</code>：显式请求停止 worker。
- <code>cargo run &#45;&#45; &#45;&#45;smoke&#45;once</code>：兼容旧命令，同样进入常驻队列，不单独开窗口。

开发版、安装版及临时副本共用一个入口，按请求登记顺序执行；<code>&#45;&#45;smoke&#45;status</code>&#32;的&#32;<code>pending</code>&#32;包含当前及等待中的客户端。相同二进制内容复用同一个 worker；版本变化时自动等待旧任务完成再替换。worker 使用独立可执行副本，正常重新安装应用不会终止它，且不占用 macOS Dock。旧版留下的多个 worker 会在下次自检时等待任务完成后自动退出。

### 构建并安装 macOS Release 应用

```sh
bun run build
```

该命令会编译&#32;<code>&#45;&#45;locked &#45;&#45;release</code>&#32;二进制，打包为&#32;<code>dist&#47;Block&#46;app</code>（Bundle ID：<code>dev&#46;eachann&#46;block</code>），执行 ad&#45;hoc 签名并校验系统动态库依赖，随后替换&#32;<code>&#47;Applications&#47;Block&#46;app</code>&#32;并启动应用（若旧进程正在运行会先请求优雅退出）。

仅构建&#32;<code>dist&#47;Block&#46;app</code>&#32;而不覆盖&#32;<code>&#47;Applications</code>（例如在 CI 环境中）：

```sh
bun run build:ci
```

### 构建 WebAssembly 编辑器

需先安装&#32;<code>wasm32&#45;unknown&#45;unknown</code>&#32;target、<code>wasm&#45;bindgen&#45;cli</code>&#32;与&#32;<code>wasm&#45;opt</code>（<code>binaryen</code>）：

```sh
bun run build:web
```

## 开源许可与第三方致谢

本项目采用&#32;[Apache License 2&#46;0](LICENSE)&#32;开源许可（Copyright 2026 eachann1024）。使用或修改本项目时请保留 NOTICE，并标明来自 Block &#47; eachann1024。

When using or modifying this project&#44; keep NOTICE and credit Block &#47; eachann1024&#46;

仓库中包含的第三方组件与字体许可说明详见&#32;[NOTICE](NOTICE)&#32;及&#32;<code>web&#47;vendor&#47;gpui&#45;pre&#45;wgpu&#47;LICENSE&#45;APACHE</code>。




