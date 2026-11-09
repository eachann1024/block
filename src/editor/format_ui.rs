//! Link field and colour palette popovers for the selection toolbar.
use gpui::{AnyElement, Entity, MouseButton, div, prelude::*, px};
use gpui_component::input::{Escape, InputState};
use crate::{dropdown::{self, RowSpec}, icons::Icon, theme::tr, ui::popover as ui_popover};
use super::{format_cmds::{FormatPop, palette::PalettePopover}, view::{Cancel, Editor}};

pub(super) fn popover(editor: Entity<Editor>, pop: &FormatPop, phase: crate::ui::motion::PopupMotion, pop_gen: u64) -> AnyElement {
    match pop {
        FormatPop::Ai(session) => super::ai::panel::render(editor, session, phase, pop_gen),
        FormatPop::Link { input, existing, spec, .. } => dropdown::anchored_motion(
            ("format-pop", pop_gen), *spec,
            crate::ui::popover::card("format-link-card", None).child(link_field(editor, input, *existing)),
            phase, 2,
        ),
        FormatPop::SelectionMenu { spec } => dropdown::anchored_motion(("format-pop", pop_gen), *spec, selection_menu(editor), phase, 2),
        FormatPop::Palette { spec } => {
            let fallback = *spec;
            let placed = editor.clone();
            dropdown::anchored_motion_with(("format-pop", pop_gen), move |_, cx| {
                toolbar_drop_spec(tr("文本与高亮颜色", "Text and highlight color"), fallback, &placed, cx)
            }, PalettePopover { editor }, phase, 2)
        }
        FormatPop::Kinds { spec, current } => {
            let fallback = *spec;
            let current = *current;
            let placed = editor.clone();
            dropdown::anchored_motion_with(("format-pop", pop_gen), move |_, cx| {
                toolbar_drop_spec(tr("文字类型", "Text type"), fallback, &placed, cx)
            }, super::toolbar::menu(editor, true, current), phase, 2)
        }
        FormatPop::Lists { spec, current } => {
            let fallback = *spec;
            let current = *current;
            let placed = editor.clone();
            dropdown::anchored_motion_with(("format-pop", pop_gen), move |_, cx| {
                toolbar_drop_spec(tr("列表", "Lists"), fallback, &placed, cx)
            }, super::toolbar::menu(editor, false, current), phase, 2)
        }
    }
}

/// Live button bounds while the selection toolbar is painted; the frozen spec covers
/// the frame before the button has measured, and keeps that frame's viewport current.
fn toolbar_drop_spec(
    key: &'static str, fallback: dropdown::PlacementSpec, editor: &Entity<Editor>, cx: &gpui::App,
) -> dropdown::PlacementSpec {
    let viewport = editor.read(cx).list.viewport_bounds();
    super::toolbar::tracked_button(key)
        .map(|trigger| dropdown::PlacementSpec::below(trigger).align_x(dropdown::AlignX::Left).viewport(viewport).gap(px(4.)))
        .unwrap_or_else(|| fallback.viewport(viewport))
}

/// Right-click menu over a selection: export it as an image, annotate it, or copy it.
fn selection_menu(editor: Entity<Editor>) -> AnyElement {
    let mac = cfg!(any(target_os = "macos", target_family = "wasm"));
    let row = |id: &'static str, icon: Icon, label: &'static str, hint: &'static str, run: fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>)| {
        let editor = editor.clone();
        dropdown::row_with(id, RowSpec { icon: Some(icon), label: label.into(), hint: Some(hint.into()), ..Default::default() })
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                editor.update(cx, |this, cx| { this.format = None; run(this, window, cx); cx.notify(); });
                cx.stop_propagation();
            })
    };
    dropdown::shell("selection-context-menu").w(px(236.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .child(row("sel-export-img", Icon::Image, tr("选中内容生成图片…", "Export Selection as Image…"), if mac { "⇧⌘E" } else { "Ctrl+Shift+E" }, |this, w, cx| this.export_image(w, cx)))
        .child(row("sel-annotate", Icon::TbComment, tr("添加批注", "Add annotation"), if mac { "⌥⌘M" } else { "Ctrl+Alt+M" }, |this, w, cx| this.start_annotation(w, cx)))
        .child(dropdown::separator())
        .child(row("sel-copy", Icon::Copy, tr("复制", "Copy"), if mac { "⌘C" } else { "Ctrl+C" }, |this, w, cx| this.handle(&super::view::EditorAction::Copy, w, cx)))
        .into_any_element()
}

fn link_field(editor: Entity<Editor>, input: &Entity<InputState>, existing: bool) -> AnyElement {
    let e_save = editor.clone();
    let e_remove = editor.clone();
    div().id("link-field").flex().items_center().gap(px(6.))
        .on_action({ let e = editor.clone(); move |_: &Escape, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .on_action({ let e = editor.clone(); move |_: &Cancel, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .child(ui_popover::inline_input_slot(input, Some(px(260.))))
        .child(ui_popover::inline_action_button(
            "link-save",
            tr("确定", "OK"),
            true,
            move |_, window, cx| {
                e_save.update(cx, |this, cx| this.link_save(window, cx));
                cx.stop_propagation();
            },
        ))
        .when(existing, |d| {
            d.child(ui_popover::inline_action_button(
                "link-remove",
                tr("移除", "Remove"),
                false,
                move |_, window, cx| {
                    e_remove.update(cx, |this, cx| this.link_remove(window, cx));
                    cx.stop_propagation();
                },
            ))
        })
        .into_any_element()
}
