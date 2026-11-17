//! Minimalist Ocean Paper inline AI floating bar (396px):
//! - No model switcher, no `@`, no gear or close icons (Esc / click outside closes).
//! - Input mode: unified prompt chips (`<=12 chars…` + hover Tooltip + `☆/★`), starred by default, max 3 rows.
//! - Streaming mode: single clean row (`● 正在生成… [停止]`), while blocks stream live on paper.
//! - Review mode: Enter accepts (`完成 ↵` / `替换 ↵`), typing refines in place, plus `插入下方` / `重写` / `放弃`.
use gpui::{AnyElement, Entity, FontWeight, SharedString, div, point, prelude::*, px, rgb};

use super::super::view;
use super::inline_prompts::{self, PromptScope};
use super::session::{AiSession, AiStatus};
use crate::editor::view::Editor;
use crate::icons::{Icon, icon};
use crate::theme::{Theme, tr};
use crate::ui::ai_controls::{
    PANEL_WIDTH, action_chip_hint, inline_text_input, input_capsule, panel_shell,
    saved_prompt_chip, send_button_enabled,
};

fn tag_button(
    id: impl Into<SharedString>,
    label: impl Into<SharedString>,
    hint: Option<&'static str>,
    primary: bool,
    open: bool,
    editor: &Entity<Editor>,
    run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static,
) -> gpui_base::Button {
    let editor = editor.clone();
    action_chip_hint(id, None, label, hint, primary, open, move |_, window, cx| {
        editor.update(cx, |this, cx| run(this, window, cx));
        cx.stop_propagation();
    })
}

pub(in crate::editor) fn render(
    editor: Entity<Editor>,
    s: &AiSession,
    phase: crate::ui::motion::PopupMotion,
    pop_gen: u64,
) -> AnyElement {
    let theme = Theme::ocean();
    let busy = matches!(s.status, AiStatus::Thinking { .. });
    let empty_ctx = s.is_empty_ctx();
    let del_fg = rgb(0xb42318);

    let err_row: Option<AnyElement> = s.error.as_ref().map(|message| {
        div()
            .px(px(4.))
            .text_size(px(13.))
            .line_height(px(18.))
            .text_color(del_fg)
            .child(SharedString::from(message.clone()))
            .into_any_element()
    });

    let field = input_capsule(busy);
    let prompt_row = if let AiStatus::Thinking { tick, reasoning, partial, .. } = &s.status {
        let phase_wave = (((*tick % 24) as f32 / 24.0) * std::f32::consts::TAU).cos() * 0.5 + 0.5;
        let label = if partial.is_empty() && !reasoning.is_empty() {
            tr("正在思考…", "Thinking…")
        } else {
            tr("正在生成…", "Generating…")
        };
        field
            .pl(px(12.))
            .pr(px(8.))
            .child(
                div()
                    .size(px(8.))
                    .flex_none()
                    .rounded_full()
                    .bg(theme.accent)
                    .opacity(0.42 + 0.58 * phase_wave),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .overflow_hidden()
                    .whitespace_nowrap()
                    .text_ellipsis()
                    .text_size(px(14.))
                    .font_weight(FontWeight(460.))
                    .text_color(theme.text.opacity(0.65 + 0.35 * phase_wave))
                    .child(label),
            )
            .child(
                tag_button("ai-stop", tr("停止", "Stop"), None, false, false, &editor, |this, w, cx| {
                    this.ai_stop(w, cx)
                })
                .h(px(26.))
                .text_color(theme.text_secondary),
            )
    } else {
        let send_ed = editor.clone();
        let is_review = matches!(s.status, AiStatus::Review { .. });
        let can_send = is_review || s.has_input_text;
        field
            .child(
                div()
                    .size(px(16.))
                    .flex_none()
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(icon(Icon::Sparkles, px(crate::ui::icon_size::ROW), theme.icon_colors(theme.accent))),
            )
            .child(inline_text_input(&s.input))
            .child(send_button_enabled("ai-send", can_send, move |_, window, cx| {
                send_ed.update(cx, |this, cx| this.ai_submit_input(window, cx));
                cx.stop_propagation();
            }))
    };

    let mut bottom_area: Option<AnyElement> = None;

    match &s.status {
        AiStatus::Input => {
            let scope = PromptScope::from_empty_ctx(empty_ctx);
            let entries = inline_prompts::list(scope);
            if !entries.is_empty() {
                let mut grid = div()
                    .w_full()
                    .max_h(px(inline_prompts::CHIP_AREA_MAX_H))
                    .overflow_hidden()
                    .flex()
                    .flex_wrap()
                    .items_center()
                    .gap(px(inline_prompts::CHIP_GAP));
                for (ix, item) in entries.into_iter().enumerate() {
                    let short = item.target.short_label();
                    let full = item.target.full_label();
                    let run_target = item.target.clone();
                    let star_target = item.target;
                    let run_ed = editor.clone();
                    let star_ed = editor.clone();
                    grid = grid.child(saved_prompt_chip(
                        ix,
                        short,
                        full,
                        item.starred,
                        move |_, window, cx| {
                            let target = run_target.clone();
                            run_ed.update(cx, |this, cx| this.ai_run_prompt_target(target, window, cx));
                            cx.stop_propagation();
                        },
                        move |_, _, cx| {
                            let target = star_target.clone();
                            star_ed.update(cx, |this, cx| this.ai_toggle_prompt_star(scope, &target, cx));
                            cx.stop_propagation();
                        },
                    ));
                }
                bottom_area = Some(grid.into_any_element());
            }
        }
        AiStatus::Thinking { .. } => {}
        AiStatus::Review { .. } => {
            let accept_label = if empty_ctx {
                tr("完成", "Done")
            } else {
                tr("替换", "Replace")
            };
            let mut row = div()
                .flex()
                .flex_wrap()
                .items_center()
                .gap(px(4.))
                .child(tag_button(
                    "ai-accept",
                    accept_label,
                    Some("↵"),
                    true,
                    false,
                    &editor,
                    |this, w, cx| this.ai_accept(w, cx),
                ));
            if !empty_ctx {
                row = row.child(tag_button(
                    "ai-insert-below",
                    tr("插入下方", "Insert Below"),
                    None,
                    false,
                    false,
                    &editor,
                    |this, w, cx| this.ai_insert_below(w, cx),
                ));
            }
            row = row
                .child(tag_button(
                    "ai-retry",
                    tr("重写", "Regenerate"),
                    None,
                    false,
                    false,
                    &editor,
                    |this, w, cx| this.ai_retry(w, cx),
                ))
                .child(div().flex_1())
                .child(
                    tag_button(
                        "ai-discard",
                        tr("放弃", "Discard"),
                        None,
                        false,
                        false,
                        &editor,
                        |this, w, cx| this.ai_close(w, cx),
                    )
                    .text_color(theme.text_secondary),
                );
            bottom_area = Some(row.into_any_element());
        }
    }

    let key_editor = editor.clone();
    let target_ids: Vec<u64> = s.parts.iter().map(|(id, _)| *id).collect();
    let anchor_x = s.anchor.x;
    let fallback_trigger = gpui::Bounds::new(
        point(anchor_x, (s.anchor.y - px(32.)).max(px(0.))),
        gpui::size(px(PANEL_WIDTH), px(24.)),
    );
    let pos_editor = editor.clone();
    let editor_id = editor.entity_id();
    crate::ui::placement::anchored_motion_with(
        ("inline-ai-pop", pop_gen),
        move |_, cx| {
            let ed = pos_editor.read(cx);
            let vp = ed.list.viewport_bounds();
            let trigger = super::preview::live_preview_bounds(editor_id)
                .or_else(|| {
                    target_ids
                        .iter()
                        .filter_map(|id| ed.block_trigger_bounds(editor_id, *id))
                        .reduce(|a, b| a.union(&b))
                })
                .or_else(|| {
                    let first_id = *target_ids.first()?;
                    let idx = ed.doc.index_of(first_id)?;
                    let scroll_top = ed.list.logical_scroll_top().item_ix;
                    let x = (vp.left() + px(crate::editor::chrome::HANDLE_WIDTH + crate::editor::chrome::ROW_GAP - 8.)).max(anchor_x);
                    let y = if idx < scroll_top { vp.top() } else { vp.bottom() };
                    Some(gpui::Bounds::new(point(x, y), gpui::size(px(PANEL_WIDTH), px(2.))))
                })
                .unwrap_or(fallback_trigger);
            crate::ui::placement::PlacementSpec::below(trigger)
                .viewport(vp)
                .gap(px(10.))
                .margin(px(12.))
        },
        panel_shell("inline-ai")
            .p(px(8.))
            .gap(px(6.))
            .on_action(|_: &view::Backspace, _, _| {})
            .on_action(|_: &view::Delete, _, _| {})
            .on_action(|_: &view::Left, _, _| {})
            .on_action(|_: &view::Right, _, _| {})
            .on_action(|_: &view::SelectLeft, _, _| {})
            .on_action(|_: &view::SelectRight, _, _| {})
            .on_action(|_: &view::SelectAll, _, _| {})
            .on_action(|_: &view::Up, _, _| {})
            .on_action(|_: &view::Down, _, _| {})
            .on_action(|_: &view::SelectUp, _, _| {})
            .on_action(|_: &view::SelectDown, _, _| {})
            .on_action(|_: &view::Enter, _, _| {})
            .on_action(|_: &view::Copy, _, _| {})
            .on_action(|_: &view::Cut, _, _| {})
            .on_action(|_: &view::Paste, _, _| {})
            .on_action(|_: &view::Bold, _, _| {})
            .on_action(|_: &view::Italic, _, _| {})
            .on_action(|_: &view::Strike, _, _| {})
            .on_action(|_: &view::Code, _, _| {})
            .on_action(|_: &view::Undo, _, _| {})
            .on_action(|_: &view::Redo, _, _| {})
            .on_action(|_: &view::Cancel, _, _| {})
            .on_action(|_: &view::NextCell, _, _| {})
            .on_action(|_: &view::PreviousCell, _, _| {})
            .on_action(|_: &view::TableMenuKey, _, _| {})
            .on_action(|_: &view::SetParagraph, _, _| {})
            .on_action(|_: &view::SetH1, _, _| {})
            .on_action(|_: &view::SetH2, _, _| {})
            .on_action(|_: &view::SetH3, _, _| {})
            .on_action(|_: &view::MoveBlockUp, _, _| {})
            .on_action(|_: &view::MoveBlockDown, _, _| {})
            .on_action(|_: &view::Underline, _, _| {})
            .on_action(|_: &view::EditLink, _, _| {})
            .on_action(|_: &view::ExitBlock, _, _| {})
            .on_action(|_: &view::DocStart, _, _| {})
            .on_action(|_: &view::DocEnd, _, _| {})
            .on_action(|_: &view::Find, _, _| {})
            .on_action(|_: &view::ImageGrow, _, _| {})
            .on_action(|_: &view::ImageShrink, _, _| {})
            .on_key_down(move |e, window, cx| {
                if e.keystroke.key == "escape" {
                    key_editor.update(cx, |this, cx| this.ai_escape(window, cx));
                    cx.stop_propagation();
                }
            })
            .children(err_row)
            .child(prompt_row)
            .children(bottom_area),
        phase,
        2,
    )
}
