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
        FormatPop::BlockContextMenu { block, spec } => dropdown::anchored_motion(("format-pop", pop_gen), *spec, block_context_menu(editor, *block), phase, 2),
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
        FormatPop::More { spec } => {
            let fallback = *spec;
            let placed = editor.clone();
            dropdown::anchored_motion_with(
                ("format-pop", pop_gen),
                move |_, cx| toolbar_drop_spec(super::toolbar::more_key(), fallback, &placed, cx),
                super::toolbar::MorePopover { editor },
                phase,
                2,
            )
        }
    }
}

/// Live button bounds while the selection toolbar is painted (or `•••` when that button is overflowed);
/// the frozen spec covers the frame before the button has measured.
fn toolbar_drop_spec(
    key: &'static str, fallback: dropdown::PlacementSpec, editor: &Entity<Editor>, cx: &gpui::App,
) -> dropdown::PlacementSpec {
    let viewport = editor.read(cx).list.viewport_bounds();
    super::toolbar::tracked_button(key)
        .or_else(|| super::toolbar::tracked_button(super::toolbar::more_key()))
        .map(|trigger| dropdown::PlacementSpec::below(trigger).align_x(dropdown::AlignX::Left).viewport(viewport).gap(px(4.)))
        .unwrap_or_else(|| fallback.viewport(viewport))
}

pub(super) fn selection_menu_slots() -> Vec<crate::ui::menu_nav::Slot> {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    vec![Item, Item, Skip, Item, Item, Item, Skip, Item]
}

pub(super) fn block_context_slots() -> Vec<crate::ui::menu_nav::Slot> {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    vec![Item, Item, Skip, Item, Item, Item]
}

fn insert_below_block(editor: &mut Editor, block_id: u64, window: &mut gpui::Window, cx: &mut gpui::Context<Editor>) {
    let Some(index) = editor.doc.index_of(block_id) else { return };
    let insert_at = editor.list_subtree_end(index) + 1;
    if editor.insert_paragraph_at(insert_at, cx) {
        window.focus(&editor.focus, cx);
    }
}

pub(super) fn run_selection(editor: &mut Editor, index: usize, window: &mut gpui::Window, cx: &mut gpui::Context<Editor>) {
    editor.format = None;
    match index {
        0 => editor.export_image(window, cx),
        1 => editor.start_annotation(window, cx),
        3 => editor.handle(&super::view::EditorAction::Cut, window, cx),
        4 => editor.handle(&super::view::EditorAction::Copy, window, cx),
        5 => editor.handle(&super::view::EditorAction::Paste, window, cx),
        7 => {
            let (_, _, _, last_idx) = editor.ordered();
            if let Some(b) = editor.doc.blocks.get(last_idx) {
                let id = b.id;
                insert_below_block(editor, id, window, cx);
            }
        }
        _ => {}
    }
    cx.notify();
}

pub(super) fn run_block_context(editor: &mut Editor, block: u64, index: usize, window: &mut gpui::Window, cx: &mut gpui::Context<Editor>) {
    editor.format = None;
    match index {
        0 => {
            if editor.insert_above(block, cx) { window.focus(&editor.focus, cx); }
        }
        1 => insert_below_block(editor, block, window, cx),
        3 => editor.handle(&super::view::EditorAction::Paste, window, cx),
        4 => {
            if let Some(ix) = editor.doc.index_of(block) { editor.duplicate_block_at(ix, window, cx); }
        }
        5 => {
            editor.run_block_cmd(block, super::block_menu::Mode::Handle, super::block_menu::Cmd::Delete, window, cx);
        }
        _ => {}
    }
    cx.notify();
}

/// Right-click menu over a selection: export, annotate, clipboard actions, or insert paragraph below.
fn selection_menu(editor: Entity<Editor>) -> AnyElement {
    let mac = cfg!(any(target_os = "macos", target_family = "wasm"));
    let active = crate::ui::menu_nav::clamp_index(super::menu_keys::format_index(), &selection_menu_slots());
    let row = |id: &'static str, slot: usize, icon: Icon, label: &'static str, hint: Option<&'static str>| {
        let editor = editor.clone();
        dropdown::row_with(id, RowSpec { icon: Some(icon), label: label.into(), hint: hint.map(Into::into), highlighted: active == slot, ..Default::default() })
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                editor.update(cx, |this, cx| run_selection(this, slot, window, cx));
                cx.stop_propagation();
            })
    };
    dropdown::shell("selection-context-menu").w(px(236.))
        .track_scroll(&crate::ui::menu_nav::scroll("format"))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .child(row("sel-export-img", 0, Icon::Image, tr("选中内容生成图片…", "Export Selection as Image…"), Some(if mac { "⇧⌘E" } else { "Ctrl+Shift+E" })))
        .child(row("sel-annotate", 1, Icon::TbComment, tr("添加批注", "Add annotation"), Some(if mac { "⌥⌘M" } else { "Ctrl+Alt+M" })))
        .child(dropdown::separator())
        .child(row("sel-cut", 3, Icon::Cut, tr("剪切", "Cut"), Some(if mac { "⌘X" } else { "Ctrl+X" })))
        .child(row("sel-copy", 4, Icon::Copy, tr("复制", "Copy"), Some(if mac { "⌘C" } else { "Ctrl+C" })))
        .child(row("sel-paste", 5, Icon::NotePlus, tr("粘贴", "Paste"), Some(if mac { "⌘V" } else { "Ctrl+V" })))
        .child(dropdown::separator())
        .child(row("sel-insert-below", 7, Icon::Plus, tr("在块下方插入段落", "Insert paragraph below"), None))
        .into_any_element()
}

/// Right-click menu over a block with no active selection.
fn block_context_menu(editor: Entity<Editor>, block: u64) -> AnyElement {
    let mac = cfg!(any(target_os = "macos", target_family = "wasm"));
    let active = crate::ui::menu_nav::clamp_index(super::menu_keys::format_index(), &block_context_slots());
    let row = |id: &'static str, slot: usize, icon: Icon, label: &'static str, hint: Option<&'static str>, danger: bool| {
        let editor = editor.clone();
        dropdown::row_with(id, RowSpec { icon: Some(icon), label: label.into(), hint: hint.map(Into::into), highlighted: active == slot, danger, ..Default::default() })
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                editor.update(cx, |this, cx| run_block_context(this, block, slot, window, cx));
                cx.stop_propagation();
            })
    };
    dropdown::shell("block-context-menu").w(px(224.))
        .track_scroll(&crate::ui::menu_nav::scroll("format"))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .child(row("ctx-insert-above", 0, Icon::ArrowUp, tr("在上方插入", "Insert above"), None, false))
        .child(row("ctx-insert-below", 1, Icon::Plus, tr("在块下方插入段落", "Insert paragraph below"), None, false))
        .child(dropdown::separator())
        .child(row("ctx-paste", 3, Icon::NotePlus, tr("粘贴", "Paste"), Some(if mac { "⌘V" } else { "Ctrl+V" }), false))
        .child(row("ctx-duplicate", 4, Icon::Duplicate, tr("创建副本", "Duplicate"), Some(if mac { "⌘D" } else { "Ctrl+D" }), false))
        .child(row("ctx-delete", 5, Icon::Trash, tr("删除", "Delete"), Some("⌫"), true))
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
