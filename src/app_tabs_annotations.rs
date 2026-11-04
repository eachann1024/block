use gpui::{Context, Div, SharedString, Stateful};
use crate::{theme::tr, ui::annotation_card};
use super::Workspace;

impl Workspace {
    pub(super) fn annotation_actions(&self, cx: &mut Context<Self>) -> Option<Stateful<Div>> {
        let (drafts, generating, has_any) = self.editor.read(cx).annotation_summary();
        if !has_any { return None; }
        let label: SharedString = if generating {
            tr("生成中…", "Generating…").into()
        } else {
            tr("统一发送", "Send all").into()
        };
        let enabled = drafts > 0 && !generating;
        let editor = self.editor.clone();
        Some(annotation_card::send_all_button(
            "annotations-send-all",
            label,
            (drafts > 0 && !generating).then_some(drafts),
            generating,
            enabled,
            move |_, _, cx| {
                cx.stop_propagation();
                editor.update(cx, |editor, cx| editor.generate_pending_annotations(cx));
            },
        ))
    }
}
