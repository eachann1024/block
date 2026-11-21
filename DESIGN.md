# Block 设计语言与配色规范（Ocean Paper）

> 内容优先、紧凑克制、严格对齐、精准轮廓、全局一致。本文是 Block 桌面端 MyGo 实现的强制视觉与交互基线，与 `internal/theme/theme.go` 完全对齐。

---

## 1. 视觉基调与克制原则

安静紧凑的桌面写作工具：文字为主体，导航贴边退让，控件按需响应。浅色如干净纸面，深色采用中性石墨灰分层；以 `1px` 精准内轮廓（`inset 0 0 0 1px`）配合轻量选中底色指示选中状态，不靠大面积浓色、发光阴影或渐变吸引注意力。

- **独立视图不平铺**：设置界面为独立整窗视图（左侧为设置分类侧栏 `260px`，右侧为当前选中分类的设置画布），点击左侧分类切换对应单页内容，**严禁将所有设置分类平铺堆叠在同一长页**。
- **文案极简克制**：设置项与菜单只保留承载信息的名称标签，**不添加冗余小字副标题、常识解释或装饰角标（eyebrow）**；正文不小于 `14px`。
- **扁平纯色表面**：卡片、按钮、输入框与分段控制器均采用单层细边框与纯色表面，禁止彩色外发光、重投影或渐变背景。

---

## 2. Ocean Paper 语义色板（与 `internal/theme/theme.go` 一致）

所有设计稿（`.dc.html`）与 Go MyGo 组件（`internal/theme/theme.go`）必须严格使用以下语义颜色，禁止私自硬编码偏离色相（如深海军蓝底或高饱和蓝框）：

| 语义角色 | Token 名称 | 浅色（Ocean Light） | 深色（Ocean Dark） | 用途说明 |
| --- | --- | --- | --- | --- |
| 安静壳层 | `shell` | `#f1f1f1` | `#262626` | 顶部标题栏、分段槽底、次级输入底 |
| 侧栏表面 | `sidebar` | `#fafafa` | `#222222` | 左侧文件树、独立设置分类侧栏 |
| 设置画布 | `settings-bg` | `#f8f8f8` | `#181818` | 设置主内容区画布背景 |
| 阅读纸面 / 卡片 | `paper` | `#ffffff` | `#181818`（浮层/卡片 `#222222`） | 正文纸面、设置分组卡片、白底按钮 |
| 菜单表面 | `menu` | `#ffffff` | `#222222` | 下拉菜单、右键菜单、浮层面板 |
| 主要文字 | `text-primary` | `#20242c`（编辑器 `#1f1e1d`） | `#ececec` | 标题、正文、表单主标签 |
| 次级文字 | `text-secondary` | `#526071`（编辑器 `#52514e`） | `#b4b4b4` | 辅助信息、快捷键提示、未选中标签 |
| Ocean 强调轮廓 | `accent` | `#547dd0`（悬停 `#3d63b0`） | `#779de8` | 光标、文本选区、链接、拖拽插入线、输入框聚焦、主按钮与开关 |
| 双色图标填充 | `icon-fill` | `#dcdcd9` | `#474747` | Duo 图标体系的中性灰填充面 |
| 轻悬停底 | `hover` | `#f0f0ef` | `#2c2c2c` | 列表行、按钮、菜单项悬停反馈 |
| 选中表面 | `selected` | `#e5e5e3` | `#3a3a3a` | 选中列表行、分段项、选中卡片底色 |
| 选中轮廓 | `selected-border` | `rgba(0,0,0,.10)` | `rgba(255,255,255,.12)` | 需要与悬停区分的选中项 `1px` 中性内轮廓 |
| 细分割线 / 卡片边 | `hairline` / `border` | `#e4e6ea`（壳层分隔 `#e6e6e6`） | `#292929`（壳层分隔 `#292929`） | 设置卡片外框、区域分隔线 |
| 按钮与控件边框 | `btn-border` | `#dcdcdc` | `#444444` | 次级按钮、输入框、键帽边框 |
| 代码块背景 | `code-bg` | `#f3f1ec` | `#242424` | 行内代码与代码块底色 |
| 引用竖条 | `quote-bar` | `#dbd9d4` | `#484848` | 引用块左侧竖条 |
| 文本选区 | `selection` | `#cde2fb`（字 `#184f95`） | `#2d426b`（字 `#9dbcf5`） | 编辑器与输入框文本选区 |

---

## 3. 字体、字重与几何尺度

| 角色 | 字体栈 | 尺寸与行高 | 字重 |
| --- | --- | --- | --- |
| UI 界面与控件 | macOS `SF Pro`（`.SystemUIFont`），中文回退 `PingFang SC`；Windows `Segoe UI` | `14px / 20px`（基线） | 常规 `400`、控件/导航 `460`、分组标题 `560` |
| 设置页大标题 | 同 UI 字体栈 | `24px / 32px` | `560` |
| 设置卡片标题 | 同 UI 字体栈 | `15px / 22px` | `560` |
| 编辑器正文 | 无衬线 `.SystemUIFont`；衬线 `LXGW WenKai` / `Songti SC` | `16px`（标准版心净宽 `672px`–`720px`） | `400` |
| 等宽代码与键帽 | `JetBrains Mono, Menlo, ui-monospace, monospace` | 代码 `13px–14px`；键帽 `12px / 14px` | `400` / `500` |

### 布局与控件尺寸基线

- **顶栏高度**：`40px`，左列宽 `260px`（与侧栏对齐，含窗口红黄绿控制点与侧栏收起按钮）。
- **左侧栏宽度**：`260px`，列表项高度 `32px`、圆角引用 `theme.RadiusControl`（`8px`）、内边距 `0 8px`。
- **设置内容区**：背景为 `settings-bg`（`#f8f8f8` / `#181818`），居中容器 `max-width: 720px`，内边距 `40px 28px 64px`。
- **设置分组卡片**：背景为 `paper`（`#ffffff` / `#222222`），边框 `1px solid border`（`#e4e6ea` / `#292929`），圆角引用 `theme.RadiusSurface`（`14px`），内边距 `18px 20px`，行间距 `14px–16px`。
- **分段控制器（Segmented Control）**：外槽 `background: shell; border: 1px solid border; padding: 3px; gap: 2px`，外槽与内部按钮 `.seg`（高 `32px–34px`）圆角统一引用 `theme.RadiusControl`（`8px`），选中态 `background: selected; color: text-primary; box-shadow: inset 0 0 0 1px accent`。
- **三联直选卡片（AI 服务商方案 C）**：仅保留 **DeepSeek**、**OpenAI 协议**、**Claude 协议** 三项，横排等宽，默认态 `background: shell; border: 1px solid border`，选中态 `background: selected; border: 1px solid accent; box-shadow: inset 0 0 0 1px accent`。
- **全局拨弦开关（Pluck Switch）**：全应用所有布尔开关统一调用 `internal/ui.Switch` / `SwitchWithOptions` / `TogglePill`（禁止在业务包直调 `ui.SwitchBase`）。所有尺寸共用轻量轨道 `28×16`：开启为浅强调色底（`Accent` 24% 透明度）+ `12px` 强调色圆点，关闭为 `SwitchTrackOff` 浅灰底 + `10px` `SwitchKnobOff` 灰点，圆点无阴影无描边；主控位（如插件页头部）用 `SwitchHero`（同款轨道，仅点击高度 `44px`），旁边用「运行中 / 已停用」文字说明状态，不再配 `PluckDivider`。完整覆盖停用、悬停、双向按压拉伸、启动中转圈、失败红框抖动、禁用与键盘焦点环，静止零重绘且遵守减弱动态效果。
- **复选列表行（插件 checklist）**：选中状态只由强调色复选框表达，行本身保持透明底与 `1px solid border`，悬停/按下才用 `hover` 底色，**禁止选中后整行铺 `selected` 灰底**。

---

## 4. 双色语义图标体系（Scheme A Duo Icons）

全局菜单、侧栏与设置分组图标采用 **Scheme A 双色语义线稿**：
- **主轮廓线**：`1.5px–1.6px` 圆端描边，颜色继承 `currentColor`（随正文/次级文字色变化）。
- **语义填充面**：`#c9d8f3`（深色 `#31456a`），用于标示文档面、面板面或核心几何体。
- **强调点睛笔触**：`#547dd0`（深色 `#779de8`），用于指示箭头、加号、光标或状态焦点。
