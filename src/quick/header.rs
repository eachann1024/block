//! Minimalist Quick-note header (centered dot pager + single icon action) and transient notice pill.
use gpui::{FontWeight, Window, div, prelude::*, px};
use crate::{
    icons::Icon,
    theme::{Theme, tr},
    ui::{
        dot_pager::dot_pager,
        icon_button::header_action_button,
        window_controls::window_controls,
    },
};
use super::{store, view::QuickView};

impl QuickView {
    pub(super) fn header(&mut self, _window: &mut Window, cx: &mut gpui::Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let active = self.meta.active;
        let entity = cx.entity().clone();
        let pager = dot_pager(
            "quick-slot",
            store::SLOTS,
            active,
            tr("切换便签（Shift + 上下滚动）", "Switch Note (Shift + Scroll)"),
            move |slot, window, cx| {
                entity.update(cx, |this, cx| this.switch(slot, window, cx));
            },
        );

        let save_icon = if self.saved_flash { Icon::Check } else { Icon::NoteCheck };
        let save_tip = if self.saved_flash {
            tr("已存入正式笔记", "Saved to Note")
        } else {
            tr("存入正式笔记", "Save to Note")
        };
        let save = header_action_button(
            "save-to-note",
            save_icon,
            self.saved_flash,
            save_tip,
            cx.listener(|this, _, w, cx| this.save_to_note(w, cx)),
        );

        let mac = cfg!(target_os = "macos");
        div()
            .h(px(44.))
            .flex_none()
            .relative()
            .flex()
            .items_center()
            .justify_end()
            .gap(px(4.))
            .pl(px(if mac { 78. } else { 16. }))
            .pr(px(if mac { 12. } else { 4. }))
            .bg(theme.paper)
            .window_control_area(gpui::WindowControlArea::Drag)
            .child(
                div()
                    .absolute()
                    .inset_0()
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(pager),
            )
            .child(save)
            .child(window_controls("quick-win", false, false))
    }

    pub(super) fn notice_pill(&self) -> impl IntoElement {
        let theme = Theme::ocean();
        div()
            .absolute()
            .right(px(16.))
            .bottom(px(14.))
            .max_w(px(360.))
            .h(px(30.))
            .px(px(12.))
            .rounded(px(999.))
            .bg(theme.menu)
            .border_1()
            .border_color(theme.menu_border)
            .shadow_sm()
            .flex()
            .items_center()
            .text_size(px(14.))
            .font_weight(FontWeight(460.))
            .text_color(theme.text_secondary)
            .overflow_hidden()
            .whitespace_nowrap()
            .text_ellipsis()
            .child(self.notice.clone())
    }
}
