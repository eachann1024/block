//! 全局界面控件圆角令牌：以选区浮动工具栏（`floating_bar` 外壳 14px、内部控件 8px）为统一基准。
use gpui::{Pixels, px};

/// 外层浮层表面圆角（14px）：浮动工具栏、下拉/右键菜单、弹层卡片、对话框、设置分组卡片、AI 卡片与复合输入容器。
pub const SURFACE: Pixels = px(14.);

/// 外层浮层 1px 边框内嵌贴边子视图裁剪圆角（13px = 14px - 1px），保证内外同心不露底角。
pub const SURFACE_INNER: Pixels = px(13.);

/// 标准交互控件圆角（8px）：普通按钮、图标按钮、输入框、列表/文件树/菜单项、标签页、分段控制器外槽与内项、气泡提示。
pub const CONTROL: Pixels = px(8.);

/// 22px 级紧凑徽标与行内尾随小按钮圆角（5px）：录制键帽单元、Esc 徽标、文件树悬停更多按钮。
pub const MICRO_MD: Pixels = px(5.);

/// 16–22px 级微型控件圆角（4px）：18px 快捷键帽、16px 色块与待办复选框、18–22px 行内关闭/删除/动作微按钮、表格贴边加号条。
pub const MICRO_SM: Pixels = px(4.);

/// 10–14px 级超微几何指示圆角（3px）：10×10 图片四角缩放手柄、行内差异删除/新增微型标记。
pub const MICRO_XS: Pixels = px(3.);

/// 语义胶囊圆角（999px）：状态胶囊、表情反应胶囊、圆点徽标与通知条。
pub const PILL: Pixels = px(999.);
