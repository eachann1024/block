# Block 设计语言与配色规范（Ocean Paper）

> 内容优先、紧凑克制、严格对齐、精准轮廓、全局一致。本文是 Block（<code>thinkly&#45;fast</code>&#32;桌面端 GPUI 实现与&#32;<code>designs&#47;thinkly&#45;fast&#45;styles</code>&#32;画板）的强制视觉与交互基线，与&#32;<code>src&#47;theme&#46;rs</code>&#32;完全对齐。

---

## 1&#46; 视觉基调与克制原则

安静紧凑的桌面写作工具：文字为主体，导航贴边退让，控件按需响应。浅色如干净纸面，深色采用中性石墨灰分层；以&#32;<code>1px</code>&#32;精准内轮廓（<code>inset 0 0 0 1px</code>）配合轻量选中底色指示选中状态，不靠大面积浓色、发光阴影或渐变吸引注意力。

- <strong>独立视图不平铺</strong>：设置界面为独立整窗视图（左侧为设置分类侧栏&#32;<code>260px</code>，右侧为当前选中分类的设置画布），点击左侧分类切换对应单页内容，<strong>严禁将所有设置分类平铺堆叠在同一长页</strong>。
- <strong>文案极简克制</strong>：设置项与菜单只保留承载信息的名称标签，<strong>不添加冗余小字副标题、常识解释或装饰角标（eyebrow）</strong>；正文不小于&#32;<code>14px</code>。
- <strong>扁平纯色表面</strong>：卡片、按钮、输入框与分段控制器均采用单层细边框与纯色表面，禁止彩色外发光、重投影或渐变背景。

---

## 2&#46; Ocean Paper 语义色板（与&#32;<code>src&#47;theme&#46;rs</code>&#32;一致）

所有设计稿（<code>&#46;dc&#46;html</code>）与 Rust GPUI 组件（<code>src&#47;theme&#46;rs</code>）必须严格使用以下语义颜色，禁止私自硬编码偏离色相（如深海军蓝底或高饱和蓝框）：

| 语义角色 | Token 名称 | 浅色（Ocean Light） | 深色（Ocean Dark） | 用途说明 |
| --- | --- | --- | --- | --- |
| 安静壳层 | `shell` | `#f1f1f1` | `#343434` | 顶部标题栏、分段槽底、次级输入底 |
| 侧栏表面 | `sidebar` | `#fafafa` | `#222222` | 左侧文件树、独立设置分类侧栏 |
| 设置画布 | `settings-bg` | `#f8f8f8` | `#181818` | 设置主内容区画布背景 |
| 阅读纸面 / 卡片 | `paper` | `#ffffff` | `#181818`（浮层/卡片 `#222222`） | 正文纸面、设置分组卡片、白底按钮 |
| 菜单表面 | `menu` | `#ffffff` | `#222222` | 下拉菜单、右键菜单、浮层面板 |
| 主要文字 | `text-primary` | `#20242c`（编辑器 `#1f1e1d`） | `#ececec` | 标题、正文、表单主标签 |
| 次级文字 | `text-secondary` | `#526071`（编辑器 `#52514e`） | `#b4b4b4` | 辅助信息、快捷键提示、未选中标签 |
| Ocean 强调轮廓 | `accent` | `#547dd0`（悬停 `#3d63b0`） | `#779de8` | 选中 `1px` 内轮廓、图标点睛描边、链接色 |
| 双色图标填充 | `icon-fill` | `#c9d8f3` | `#31456a` | Duo 图标体系的浅蓝语义填充面 |
| 轻悬停底 | `hover` | `#eef3fc` | `#2c2c2c` | 列表行、按钮、菜单项悬停反馈 |
| 选中表面 | `selected` | `#e5edfc` | `#343434` | 选中列表行、分段项、选中卡片底色（配 `inset 0 0 0 1px accent`） |
| 细分割线 / 卡片边 | `hairline` / `border` | `#e4e6ea`（壳层分隔 `#e6e6e6`） | `#2e2e2e`（壳层分隔 `#333333`） | 设置卡片外框、区域分隔线 |
| 按钮与控件边框 | `btn-border` | `#dcdcdc` | `#444444` | 次级按钮、输入框、键帽边框 |
| 代码块背景 | `code-bg` | `#f3f1ec` | `#242424` | 行内代码与代码块底色 |
| 引用竖条 | `quote-bar` | `#dbd9d4` | `#484848` | 引用块左侧竖条 |
| 文本选区 | `selection` | `#cde2fb`（字 `#184f95`） | `#2d426b`（字 `#9dbcf5`） | 编辑器与输入框文本选区 |

---

## 3&#46; 字体、字重与几何尺度

| 角色 | 字体栈 | 尺寸与行高 | 字重 |
| --- | --- | --- | --- |
| UI 界面与控件 | `Inter, -apple-system, 'PingFang SC', 'Microsoft YaHei', system-ui, sans-serif` | `14px / 20px`（基线） | 常规 `400`、控件/导航 `460`、分组标题 `560` |
| 设置页大标题 | 同 UI 字体栈 | `24px / 32px` | `560` |
| 设置卡片标题 | 同 UI 字体栈 | `15px / 22px` | `560` |
| 编辑器正文 | 无衬线 `.SystemUIFont`；衬线 `LXGW WenKai` / `Songti SC` | `16px`（标准版心净宽 `672px`–`720px`） | `400` |
| 等宽代码与键帽 | `JetBrains Mono, Menlo, ui-monospace, monospace` | 代码 `13px–14px`；键帽 `12px / 14px` | `400` / `500` |

### 布局与控件尺寸基线

- <strong>顶栏高度</strong>：<code>40px</code>，左列宽&#32;<code>260px</code>（与侧栏对齐，含窗口红黄绿控制点与侧栏收起按钮）。
- <strong>左侧栏宽度</strong>：<code>260px</code>，列表项高度&#32;<code>32px</code>、圆角引用&#32;<code>src&#47;ui&#47;radius&#58;&#58;CONTROL</code>（<code>8px</code>）、内边距&#32;<code>0 8px</code>。
- <strong>设置内容区</strong>：背景为&#32;<code>settings&#45;bg</code>（<code>&#35;f8f8f8</code>&#32;&#47;&#32;<code>&#35;181818</code>），居中容器&#32;<code>max&#45;width&#58; 720px</code>，内边距&#32;<code>40px 28px 64px</code>。
- <strong>设置分组卡片</strong>：背景为&#32;<code>paper</code>（<code>&#35;ffffff</code>&#32;&#47;&#32;<code>&#35;222222</code>），边框&#32;<code>1px solid border</code>（<code>&#35;e4e6ea</code>&#32;&#47;&#32;<code>&#35;2e2e2e</code>），圆角引用&#32;<code>src&#47;ui&#47;radius&#58;&#58;SURFACE</code>（<code>14px</code>），内边距&#32;<code>18px 20px</code>，行间距&#32;<code>14px–16px</code>。
- <strong>分段控制器（Segmented Control）</strong>：外槽&#32;<code>background&#58; shell&#59; border&#58; 1px solid border&#59; padding&#58; 3px&#59; gap&#58; 2px</code>，外槽与内部按钮&#32;<code>&#46;seg</code>（高&#32;<code>32px–34px</code>）圆角统一引用&#32;<code>src&#47;ui&#47;radius&#58;&#58;CONTROL</code>（<code>8px</code>），选中态&#32;<code>background&#58; selected&#59; color&#58; text&#45;primary&#59; box&#45;shadow&#58; inset 0 0 0 1px accent</code>。
- <strong>三联直选卡片（AI 服务商方案 C）</strong>：仅保留&#32;<strong>DeepSeek</strong>、<strong>OpenAI 协议</strong>、<strong>Claude 协议</strong>&#32;三项，横排等宽，默认态&#32;<code>background&#58; shell&#59; border&#58; 1px solid border</code>，选中态&#32;<code>background&#58; selected&#59; border&#58; 1px solid accent&#59; box&#45;shadow&#58; inset 0 0 0 1px accent</code>。

---

## 4&#46; 双色语义图标体系（Scheme A Duo Icons）

全局菜单、侧栏与设置分组图标采用&#32;<strong>Scheme A 双色语义线稿</strong>：
- <strong>主轮廓线</strong>：<code>1&#46;5px–1&#46;6px</code>&#32;圆端描边，颜色继承&#32;<code>currentColor</code>（随正文&#47;次级文字色变化）。
- <strong>语义填充面</strong>：<code>&#35;c9d8f3</code>（深色&#32;<code>&#35;31456a</code>），用于标示文档面、面板面或核心几何体。
- <strong>强调点睛笔触</strong>：<code>&#35;547dd0</code>（深色&#32;<code>&#35;779de8</code>），用于指示箭头、加号、光标或状态焦点。


&#47;
