use gpui::{AnyElement, Context, ParentElement, SharedString, prelude::*, px};
use crate::{theme::{self, tr}, ui::annotation_actions::{self, BatchTone}};
use super::Workspace;

impl Workspace {
    pub(super) fn annotation_actions(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let (pending, generated, restorable, generating) = self.editor.read(cx).annotation_summary();
        if pending == 0 && generated == 0 && restorable == 0 { return None; }
        let mut actions = gpui::div().id("titlebar-annotation-actions").flex_none().flex().items_center().gap(px(4.));
        if pending > 0 || generating {
            let label: SharedString = if generating {
                tr("正在生成…", "Generating…").into()
            } else if theme::is_en() {
                format!("Ask AI to revise ({pending})").into()
            } else {
                format!("交给 AI 一起修改（{pending}）").into()
            };
            let editor = self.editor.clone();
            actions = actions.child(annotation_actions::batch_button(
                "annotations-generate", label, BatchTone::Accent, !generating,
                move |_, _, cx| {
                    cx.stop_propagation();
                    editor.update(cx, |editor, cx| editor.generate_pending_annotations(cx));
                },
            ));
        }
        if generated > 0 {
            let editor = self.editor.clone();
            let label: SharedString = if theme::is_en() {
                format!("Apply all ({generated})").into()
            } else {
                format!("一键全部应用（{generated}）").into()
            };
            actions = actions.child(annotation_actions::batch_button(
                "annotations-apply-all", label, BatchTone::Accent, true,
                move |_, window, cx| {
                    cx.stop_propagation();
                    editor.update(cx, |editor, cx| editor.apply_all_annotations(window, cx));
                },
            ));
        }
        if restorable > 0 {
            let editor = self.editor.clone();
            let label: SharedString = if theme::is_en() {
                format!("Restore all ({restorable})").into()
            } else {
                format!("全部还原（{restorable}）").into()
            };
            actions = actions.child(annotation_actions::batch_button(
                "annotations-restore-all", label, BatchTone::Neutral, true,
                move |_, window, cx| {
                    cx.stop_propagation();
                    editor.update(cx, |editor, cx| editor.restore_all_annotations(window, cx));
                },
            ));
        }
        Some(actions.into_any_element())
    }
}
