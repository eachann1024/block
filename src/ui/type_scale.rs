//! 界面字号与字重令牌（DESIGN.md §3）：只用整数字号，界面字重只用 400 / 460 / 560。
//! 编辑器正文随缩放计算，不走这里。
use gpui::{FontWeight, Pixels, px};

/// 元信息、键帽、状态说明。
pub const META: Pixels = px(12.);
/// 次级说明、标签页、紧凑控件。
pub const SMALL: Pixels = px(13.);
/// 界面基线（菜单、列表、按钮、正文）。
pub const BODY: Pixels = px(14.);
/// 卡片与分组标题。
pub const CARD_TITLE: Pixels = px(15.);
/// 设置页大标题。
pub const PAGE_TITLE: Pixels = px(24.);

/// 常规文字。
pub const REGULAR: FontWeight = FontWeight(400.);
/// 控件、导航与选中项。
pub const CONTROL: FontWeight = FontWeight(460.);
/// 分组与卡片标题。
pub const TITLE: FontWeight = FontWeight(560.);
/// 仅限等宽代码与键帽。
pub const MONO_MEDIUM: FontWeight = FontWeight(500.);
