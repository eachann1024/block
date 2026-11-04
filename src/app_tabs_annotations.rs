use gpui::{AnyElement, Context, ParentElement, SharedString, prelude::*, px};
use crate::{theme::{self, tr}, ui::annotation_card};
use super::Workspace;

impl Workspace {
    pub(super) fn annotation_actions(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (drafts, generating, has_any) = self.editor.read(cx).annotation_summary();
        if !has_any { return None; }
        let label: SharedString = if generating {
            tr("生成中…", "Generating…").into()
        } else if drafts > 0 {
            if theme::is_en() {
                format!("Send all ({drafts})").into()
            } else {
                format!("统一发送（{drafts}）").into()
            }
        } else {
            tr("统一发送", "Send all").into()
        };
        let enabled = drafts > 0 && !generating;
        let editor = self.editor.clone();
        let actions = gpui::div()
            .id("titlebar-annotation-actions")
            .flex_none()
            .flex()
            .items_center()
            .gap(px(4.))
            .child(annotation_card::send_all_button(
                "annotations-send-all",
                label,
                generating,
                enabled,
                move |_, _, cx| {
                    cx.stop_propagation();
                    editor.update(cx, |editor, cx| editor.generate_pending_annotations(cx));
                },
            ));
        Some(actions.into_any_element())
    }
}
