//! Quick-note header (slots, name, 保存到笔记, help), footer and help popover.
use gpui::{CursorStyle, FontWeight, MouseButton, SharedString, Window, div, prelude::*, px};
use gpui_component::input::Input;
use crate::icons::{Icon, icon};
use crate::theme::{Theme, format_hotkey, quick_shortcut, tr};
use super::{store, view::QuickView};

fn kbd(label: impl Into<SharedString>) -> gpui::Div {
    let theme = Theme::ocean();
    div().h(px(18.)).px(px(5.)).flex().items_center().rounded(px(4.)).bg(theme.shell).border_1().border_color(theme.btn_border)
        .text_size(px(11.)).text_color(theme.text_secondary).child(label.into())
}

impl QuickView {
    pub(super) fn header(&mut self, window: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let active = self.meta.active;
        let slots = div().flex().gap(px(2.)).children((0..store::SLOTS).map(|slot| {
            let on = slot == active;
            div().id(("slot", slot)).w(px(26.)).h(px(24.)).flex().items_center().justify_center().rounded(px(6.))
                .text_size(px(12.)).font_weight(FontWeight(500.)).cursor(CursorStyle::PointingHand)
                .text_color(if on { theme.text } else { theme.text_secondary })
                .when(on, |d| d.bg(theme.selected).border_1().border_color(theme.accent))
                .when(!on, |d| d.hover(move |s| s.bg(theme.hover).text_color(theme.text)))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, w, cx| this.switch(slot, w, cx)))
                .child((slot + 1).to_string())
        }));
        let name = match &self.renaming {
            Some((input, _)) => div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false)).into_any_element(),
            None => div().id("slot-name").flex_1().min_w_0().h(px(28.)).flex().items_center().justify_center()
                .text_size(px(13.)).font_weight(FontWeight(500.)).cursor(CursorStyle::IBeam)
                .overflow_hidden().whitespace_nowrap().text_ellipsis()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.begin_rename(w, cx)))
                .child(store::display_name(active, &self.meta.names[active])).into_any_element(),
        };
        let save = div().id("save-to-note").h(px(28.)).px(px(10.)).flex().items_center().gap(px(5.)).rounded(px(7.))
            .bg(theme.paper).border_1().border_color(theme.btn_border).text_size(px(12.)).font_weight(FontWeight(460.))
            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.btn_hover)).whitespace_nowrap()
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, w, cx| this.save_to_note(w, cx)))
            .child(icon(Icon::NoteCheck, px(13.), theme.icon_colors(theme.text))).child(tr("保存到笔记", "Save to Note"));
        let help = div().id("quick-help").w(px(28.)).h(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { this.help = !this.help; cx.notify(); }))
            .child(icon(Icon::HelpCircle, px(15.), theme.icon_colors(theme.text_secondary)));
        let _ = window;
        // Left padding clears the traffic lights drawn over the transparent titlebar.
        div().h(px(40.)).flex_none().flex().items_center().gap(px(8.)).pl(px(78.)).pr(px(8.))
            .bg(theme.sidebar).border_b_1().border_color(theme.hairline)
            .window_control_area(gpui::WindowControlArea::Drag)
            .child(slots).child(name).child(save).child(help)
    }

    pub(super) fn footer(&self, status: String) -> impl IntoElement {
        let theme = Theme::ocean();
        let quick_label = format_hotkey(&quick_shortcut());
        let hint = |keys: SharedString, label: &'static str| div().flex().items_center().gap(px(4.)).child(kbd(keys)).child(label);
        div().h(px(32.)).flex_none().flex().items_center().gap(px(12.)).px(px(14.)).border_t_1().border_color(theme.hairline)
            .text_size(px(11.)).text_color(theme.text_secondary)
            .child(hint(quick_label.into(), tr("呼出 / 隐藏", "Toggle"))).child(hint("⌘1–5".into(), tr("切换", "Switch"))).child(hint("⌘F".into(), tr("查找", "Find")))
            .child(div().ml_auto().overflow_hidden().whitespace_nowrap().text_ellipsis().child(status))
    }

    pub(super) fn help_popover(&self) -> impl IntoElement {
        let theme = Theme::ocean();
        let quick_label = format_hotkey(&quick_shortcut());
        let row = |keys: SharedString, label: &'static str| div().flex().items_center().justify_between().gap(px(16.))
            .child(div().child(label)).child(kbd(keys));
        div().id("quick-help-pop").absolute().top(px(44.)).right(px(8.)).w(px(236.)).p(px(12.)).flex().flex_col().gap(px(8.))
            .rounded(px(8.)).bg(theme.menu).border_1().border_color(theme.menu_border).shadow_md().occlude()
            .text_size(px(12.)).text_color(theme.text)
            .child(div().font_weight(FontWeight(600.)).child(tr("使用说明", "Quick Note Tips")))
            .child(row(quick_label.into(), tr("呼出 / 隐藏速记", "Toggle Quick Note"))).child(row("⌘1–5".into(), tr("切换槽位", "Switch Slot")))
            .child(row("Esc / ⌘W".into(), tr("隐藏小窗", "Hide Window"))).child(row("⌘F".into(), tr("查找", "Find")))
            .child(div().text_color(theme.text_secondary).child(tr("内容自动保存；点名称可重命名；保存到笔记会新建一篇笔记。", "Auto-saves changes. Click slot name to rename. Save to Note creates a new note.")))
    }
}
