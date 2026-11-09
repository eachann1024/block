//! Compact Ocean Paper inline AI bar: 360px width, 8px padding, headerless two-row layout.
use gpui::{AnyElement, Entity, SharedString, div, point, prelude::*, px, rgb};
use gpui_component::input::Input;

use super::super::view;
use super::session::{AiMenu, AiSession, AiStatus, Preset, ticker_line};
use crate::editor::view::Editor;
use crate::icons::{Icon, icon};
use crate::theme::{Theme, tr};
use crate::ui::ai_controls::{PANEL_WIDTH, action_chip, diff_legend, icon_chip, input_capsule, panel_shell, send_button};

fn preset_icon(p: Preset) -> Icon {
    match p {
        Preset::Polish => Icon::AiPolish,
        Preset::Simplify => Icon::AiSimplify,
        Preset::English => Icon::AiTransEn,
        Preset::Chinese => Icon::AiTransZh,
        Preset::Jargon => Icon::AiJargon,
        Preset::Colloquial => Icon::AiColloquial,
        Preset::List => Icon::AiList,
        Preset::Tasks => Icon::AiTasks,
    }
}

fn tag(
    id: impl Into<SharedString>,
    lead: Option<Icon>,
    label: impl Into<SharedString>,
    primary: bool,
    open: bool,
    editor: &Entity<Editor>,
    run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static,
) -> gpui_base::Button {
    let editor = editor.clone();
    action_chip(id, lead, label, primary, open, move |_, window, cx| {
        editor.update(cx, |this, cx| run(this, window, cx));
        cx.stop_propagation();
    })
}

fn menu_tag(id: &'static str, label: &'static str, which: AiMenu, current: AiMenu, editor: &Entity<Editor>) -> gpui_base::Button {
    let theme = Theme::ocean();
    tag(id, None, label, false, current == which, editor, move |this, _, cx| this.ai_toggle_menu(which, cx))
        .child(div().text_color(theme.text_secondary).text_size(px(12.)).child(if current == which { "▴" } else { "▾" }))
}

fn menu_item(p: Preset, highlighted: bool, editor: &Entity<Editor>) -> gpui_base::Button {
    let editor = editor.clone();
    crate::dropdown::row_with(SharedString::from(format!("ai-item-{p:?}")), crate::dropdown::RowSpec {
        icon: Some(preset_icon(p)), label: p.label().into(), highlighted, ..Default::default()
    })
        .on_mouse_down(gpui::MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| this.ai_preset(p, window, cx));
            cx.stop_propagation();
        })
}

fn preset_tag(p: Preset, editor: &Entity<Editor>) -> gpui_base::Button {
    tag(SharedString::from(format!("ai-preset-{p:?}")), Some(preset_icon(p)), p.label(), false, false, editor, move |this, w, cx| this.ai_preset(p, w, cx))
}

pub(in crate::editor) fn render(editor: Entity<Editor>, s: &AiSession, phase: crate::ui::motion::PopupMotion, pop_gen: u64) -> AnyElement {
    let theme = Theme::ocean();
    let busy = matches!(s.status, AiStatus::Thinking { .. });
    let del_fg = rgb(0xb42318);

    let err_row: Option<AnyElement> = match &s.status {
        AiStatus::Error { message } => Some(
            div().px(px(4.)).text_size(px(14.)).text_color(del_fg)
                .child(SharedString::from(if message.is_empty() {
                    tr("生成中断，未对正文进行修改", "Generation interrupted; text unchanged").to_owned()
                } else {
                    message.clone()
                }))
                .into_any_element(),
        ),
        _ => None,
    };

    let field = input_capsule(busy);
    let prompt_row = if let AiStatus::Thinking { tick, partial, reasoning } = &s.status {
        let line = if !reasoning.trim().is_empty() && partial.trim().is_empty() { ticker_line(reasoning) } else { ticker_line(partial) };
        let line = if !line.is_empty() {
            line
        } else if !reasoning.is_empty() {
            tr("正在思考并生成…", "Thinking and writing…").to_owned()
        } else {
            tr("思考中", "Thinking").to_owned()
        };
        field
            .child(div().size(px(8.)).flex_none().rounded_full().bg(theme.accent).opacity(0.35 + 0.65 * ((*tick % 16) as f32 / 15.)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(14.)).text_color(theme.text_secondary).child(SharedString::from(line)))
            .child(tag("ai-stop", Some(Icon::Stop), tr("停止", "Stop"), false, false, &editor, |this, w, cx| this.ai_stop(w, cx)))
    } else {
        let send_ed = editor.clone();
        field
            .child(icon(Icon::Sparkles, px(15.), theme.icon_colors(theme.accent)))
            .child(div().flex_1().min_w_0().h(px(26.)).text_size(px(14.)).child(Input::new(&s.input).appearance(false).bordered(false).focus_bordered(false).h(px(26.))))
            .child(send_button("ai-send", move |_, window, cx| {
                send_ed.update(cx, |this, cx| this.ai_submit_input(window, cx));
                cx.stop_propagation();
            }))
    };

    let mut footer = div().flex().flex_wrap().items_center().gap(px(4.));
    let mut submenu: Option<gpui::Stateful<gpui::Div>> = None;
    match &s.status {
        AiStatus::Input => {
            let set_ed = editor.clone();
            let close_ed = editor.clone();
            footer = footer
                .child(preset_tag(Preset::Polish, &editor))
                .child(preset_tag(Preset::Simplify, &editor))
                .child(menu_tag("ai-translate", tr("翻译", "Translate"), AiMenu::Translate, s.menu, &editor))
                .child(menu_tag("ai-more", tr("更多", "More"), AiMenu::More, s.menu, &editor))
                .child(div().flex_1())
                .child(icon_chip("ai-settings", Icon::Sliders, tr("打开 AI 设置", "Open AI Settings"), move |_, w, cx| {
                    set_ed.update(cx, |this, cx| this.ai_open_settings(w, cx));
                    cx.stop_propagation();
                }))
                .child(icon_chip("ai-close", Icon::Close, tr("关闭 (Esc)", "Close (Esc)"), move |_, w, cx| {
                    close_ed.update(cx, |this, cx| this.ai_close(w, cx));
                    cx.stop_propagation();
                }));
            let items: &[Preset] = match s.menu {
                AiMenu::Translate => &[Preset::English, Preset::Chinese],
                AiMenu::More => &[Preset::Jargon, Preset::Colloquial, Preset::List, Preset::Tasks],
                AiMenu::None => &[],
            };
            if !items.is_empty() {
                submenu = Some(
                    items.iter().enumerate().fold(crate::dropdown::shell("ai-submenu").w(px(176.)), |d, (ix, p)| {
                        d.child(menu_item(*p, ix == s.menu_index, &editor))
                    }),
                );
            }
        }
        AiStatus::Thinking { .. } => {}
        AiStatus::Review { .. } => {
            footer = footer
                .child(tag("ai-accept", None, tr("应用修改", "Apply Changes"), true, false, &editor, |this, w, cx| this.ai_accept(w, cx)))
                .child(tag("ai-discard", None, tr("放弃", "Discard"), false, false, &editor, |this, w, cx| this.ai_close(w, cx)))
                .child(tag("ai-retry", None, tr("重新生成", "Regenerate"), false, false, &editor, |this, w, cx| this.ai_retry(w, cx)))
                .child(div().flex_1())
                .child(diff_legend());
        }
        AiStatus::Error { .. } => {
            if !s.prompt.is_empty() {
                footer = footer.child(tag("ai-retry", None, tr("重试", "Retry"), true, false, &editor, |this, w, cx| this.ai_retry(w, cx)));
            }
            footer = footer.child(tag("ai-discard", None, tr("关闭", "Close"), false, false, &editor, |this, w, cx| this.ai_close(w, cx)));
        }
    }
    let show_footer = !busy;

    let key_editor = editor.clone();
    let target_ids: Vec<u64> = s.parts.iter().map(|(id, _)| *id).collect();
    let fallback_trigger = gpui::Bounds::new(
        point(s.anchor.x, (s.anchor.y - px(32.)).max(px(0.))),
        gpui::size(px(PANEL_WIDTH), px(24.)),
    );
    let pos_editor = editor.clone();
    let editor_id = editor.entity_id();
    gpui::deferred(
        crate::ui::placement::smart_anchored_with(move |_, cx| {
            let ed = pos_editor.read(cx);
            let trigger = target_ids
                .iter()
                .filter_map(|id| ed.block_trigger_bounds(editor_id, *id))
                .reduce(|a, b| a.union(&b))
                .unwrap_or(fallback_trigger);
            crate::ui::placement::PlacementSpec::below(trigger)
                .viewport(ed.list.viewport_bounds())
                .gap(px(6.))
                .margin(px(12.))
        })
        .child(crate::ui::motion::animate_popup(
            panel_shell("inline-ai")
                .on_action(|_: &view::Backspace, _, _| {})
                .on_action(|_: &view::Delete, _, _| {})
                .on_action(|_: &view::Left, _, _| {})
                .on_action(|_: &view::Right, _, _| {})
                .on_action(|_: &view::SelectLeft, _, _| {})
                .on_action(|_: &view::SelectRight, _, _| {})
                .on_action(|_: &view::SelectAll, _, _| {})
                .capture_action({
                    let ed = editor.clone();
                    move |_: &gpui_component::input::MoveUp, _, cx| {
                        if ed.update(cx, |this, cx| this.ai_step_menu(false, cx)) { cx.stop_propagation(); }
                    }
                })
                .capture_action({
                    let ed = editor.clone();
                    move |_: &gpui_component::input::MoveDown, _, cx| {
                        if ed.update(cx, |this, cx| this.ai_step_menu(true, cx)) { cx.stop_propagation(); }
                    }
                })
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
                .when(show_footer, |d| d.child(footer))
                .children(submenu),
            phase,
            ("inline-ai-pop", pop_gen),
        )),
    )
    .with_priority(2)
    .into_any_element()
}
