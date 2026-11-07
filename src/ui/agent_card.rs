//! Agent configuration navigation and explicit opt-in are separate hit targets.
use std::sync::{Arc, LazyLock};
use gpui::{App, CursorStyle, ElementId, FontWeight, Image, ImageFormat, MouseButton,
    MouseDownEvent, Stateful, Div, Window, div, img, svg, prelude::*, px};
use crate::{icons::{Icon, icon}, theme::Theme};
use super::{card_bg, selected_bg};

static BLOCK_ICON: LazyLock<Arc<Image>> = LazyLock::new(|| Arc::new(Image::from_bytes(
    ImageFormat::Png, include_bytes!("../../assets/app-icon/apple-touch-icon.png").to_vec(),
)));

pub fn agent_card(
    id: ElementId, toggle_id: ElementId, label: &'static str, logo: Option<&'static str>,
    focused: bool, enabled: bool,
    on_select: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_toggle: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let brand = div().size(px(48.)).flex_none()
        .when_some(logo, |d, path| d.child(svg().path(path).size(px(48.)).text_color(theme.text)))
        .when(logo.is_none(), |d| d.child(img(BLOCK_ICON.clone()).size(px(48.))));
    let checkbox = div().id(toggle_id.clone()).relative()
        .children(crate::ui::smoke_overlay::probe_for(&toggle_id))
        .size(px(32.)).flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, on_toggle)
        .child(div().size(px(22.)).flex().items_center().justify_center().rounded(px(6.))
            .border_1().border_color(if enabled { theme.accent } else { theme.btn_border })
            .bg(if enabled { theme.accent } else { card_bg() })
            .when(enabled, |d| d.child(icon(Icon::Check, px(16.), theme.mono_icon_colors(gpui::rgb(0xffffff).into())))));
    div().id(id.clone()).relative().children(crate::ui::smoke_overlay::probe_for(&id))
        .flex_1().min_w(px(136.)).h(px(144.)).p(px(16.)).flex().flex_col().justify_between()
        .rounded(crate::ui::radius::CONTROL).border_1()
        .border_color(if focused { theme.accent } else { theme.btn_border })
        .bg(if focused { selected_bg() } else { theme.shell })
        .cursor(CursorStyle::PointingHand).hover(|s| s.border_color(theme.accent))
        .on_mouse_down(MouseButton::Left, on_select)
        .child(div().flex().items_start().justify_between().child(brand).child(checkbox))
        .child(div().text_size(px(16.)).font_weight(FontWeight(600.)).text_color(theme.text).child(label))
}
