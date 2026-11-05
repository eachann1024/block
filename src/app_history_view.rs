//! View rendering helpers for Page History in the Workspace.
use gpui::{
    Context, FontWeight, IntoElement, ParentElement, Styled, Window, div, prelude::*, px,
};
use crate::{
    icons::Icon,
    page_history::{HistoryError, render_history_panel, render_preview_surface, render_restore_modal},
    theme::{Theme, tr},
    ui::{icon_button::icon_button, radius, text_action::plain_action},
};
use super::Workspace;

impl Workspace {
    pub(super) fn render_history_main(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> gpui::AnyElement {
        let Some(hist) = &self.history else { return div().into_any_element(); };
        let note_path = Some(hist.path.as_path());
        let selected_version = hist.selected_id.as_ref().and_then(|id| hist.versions.iter().find(|v| &v.id == id));
        let selected_content = if hist.content_loading { None } else { hist.selected_content.as_deref() };
        let diff_blocks = hist.diff_blocks.as_deref();
        let preview_key = hist.preview_key.as_str();
        let list_focus = hist.list_focus.clone();
        let panel_error = hist.error.as_ref().and_then(|err| match err {
            HistoryError::LoadFailed(_) => Some(err.message()),
            _ => None,
        });
        let entity_mode = cx.entity();
        let entity_select = cx.entity();
        let preview = render_preview_surface(
            note_path, selected_version, selected_content, &hist.current_content, hist.mode, hist.is_readonly,
            hist.error.as_ref(), diff_blocks, preview_key,
            move |mode, _, cx| { entity_mode.update(cx, |this, cx| this.toggle_history_mode(mode, cx)); },
            cx.listener(|this, _, window, cx| this.open_restore_confirm(window, cx)),
            cx.listener(|this, _, _, cx| this.recompare_history(cx)),
            cx.listener(|this, _, window, cx| this.confirm_restore(window, cx)),
        );
        let sidebar = render_history_panel(
            &hist.versions, hist.selected_id.as_deref(), hist.loading, panel_error.as_deref(),
            move |id, _, cx| {
                let id_owned = id.to_string();
                entity_select.update(cx, |this, cx| this.select_history_version(&id_owned, cx));
            },
            cx.listener(|this, _, window, cx| this.close_page_history_focused(window, cx)),
            cx.listener(|this, _, _, cx| this.retry_history_load(cx)),
        );
        div().flex_1().min_w_0().h_full().flex()
            .child(preview)
            .child(
                div().id("history-list-focus").key_context("Workspace").h_full().track_focus(&list_focus)
                    .on_key_down(cx.listener(|this, event: &gpui::KeyDownEvent, _, cx| {
                        if this.history.as_ref().is_some_and(|hist| hist.confirm_open || hist.restoring) { return; }
                        if event.keystroke.key == "up" { cx.stop_propagation(); this.navigate_history(-1, cx); }
                        else if event.keystroke.key == "down" { cx.stop_propagation(); this.navigate_history(1, cx); }
                    }))
                    .child(sidebar),
            )
            .into_any_element()
    }

    pub(super) fn render_history_toast(&self, cx: &mut Context<Self>) -> Option<gpui::AnyElement> {
        let toast = self.history_toast.as_ref()?;
        let theme = Theme::ocean();
        let msg = toast.message.clone();
        let can_undo = toast.can_undo;
        Some(div().mx(px(28.)).mt(px(12.)).p(px(10.)).rounded(radius::CONTROL).bg(theme.selected).border_1().border_color(theme.accent)
            .flex().items_center().justify_between().gap(px(12.))
            .child(div().text_size(px(14.)).font_weight(FontWeight(460.)).text_color(theme.text).child(msg))
            .child(div().flex().items_center().gap(px(8.))
                .when(can_undo, |row| row.child(plain_action(
                    "history-toast-undo", tr("撤销恢复", "Undo Restore"), 28., 12., 13.,
                    cx.listener(|this, _, window, cx| this.undo_restore_action(window, cx)),
                )))
                .child(icon_button("history-toast-dismiss", Icon::Close, true, cx.listener(|this, _, _, cx| this.dismiss_history_toast(cx)))))
            .into_any_element())
    }

    pub(super) fn render_history_confirm_modal(&self, cx: &mut Context<Self>) -> Option<gpui::AnyElement> {
        let hist = self.history.as_ref()?;
        if !hist.confirm_open { return None; }
        let modal_focus = hist.modal_focus.clone();
        let target_time = hist.selected_id.as_ref().and_then(|id| hist.versions.iter().find(|v| &v.id == id)).map(|v| v.display_time.as_str()).unwrap_or("");
        Some(div().key_context("Workspace").track_focus(&modal_focus)
            .on_key_down(cx.listener(|_, event: &gpui::KeyDownEvent, _, cx| {
                if event.keystroke.key == "up" || event.keystroke.key == "down" { cx.stop_propagation(); }
            }))
            .child(render_restore_modal(
                target_time,
                cx.listener(|this, _, window, cx| this.cancel_restore_confirm(window, cx)),
                cx.listener(|this, _, window, cx| this.confirm_restore(window, cx)),
            ))
            .into_any_element())
    }
}
