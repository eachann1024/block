//! Block menu rendering: the floating slash/handle dropdown list and separators.
use gpui::{AnyElement, Entity, MouseButton, Window, prelude::*, px};
use crate::{
    dropdown::{self, RowSpec},
    editor::view::Editor,
    ui::menu_nav::{self, Slot},
};
use super::{BlockMenu, Cmd, SLASH_MENU_WIDTH, live_block_bounds};

pub(in crate::editor) fn overlay(
    editor_ref: &Editor,
    editor: Entity<Editor>,
    menu: BlockMenu,
    entries: Vec<Option<Cmd>>,
    phase: crate::ui::motion::PopupMotion,
    pop_gen: u64,
    window: &Window,
) -> AnyElement {
    let id = menu.block;
    let mode = menu.mode;
    let editor_id = editor.entity_id();
    let init_trigger = live_block_bounds(editor_id, id)
        .or_else(|| editor_ref.layouts.get(&id).map(|l| l.bounds))
        .unwrap_or(menu.trigger);
    let init_spec = dropdown::PlacementSpec::below(init_trigger)
        .viewport(editor_ref.list.viewport_bounds())
        .gap(px(4.))
        .reserve_height(px(dropdown::MAX_HEIGHT));
    let max_h = dropdown::max_height_for_spec(&init_spec, window.viewport_size(), px(dropdown::MAX_HEIGHT));

    let slots: Vec<Slot> = entries.iter().map(|entry| if entry.is_some() { Slot::Item } else { Slot::Skip }).collect();
    let active_idx = menu_nav::clamp_index(menu.index, &slots);
    let mut list = dropdown::shell("block-menu")
        .track_scroll(&editor_ref.block_menu_scroll)
        .on_scroll(crate::ui::scrollbar::contain_wheel(&editor_ref.block_menu_scroll))
        .min_w(px(SLASH_MENU_WIDTH))
        .w(px(SLASH_MENU_WIDTH))
        .max_h(max_h);
    for (index, entry) in entries.iter().copied().enumerate() {
        match entry {
            None => list = list.child(dropdown::separator()),
            Some(cmd) => {
                let active = index == active_idx;
                let item_editor = editor.clone();
                let item = dropdown::row_with(
                    ("block-menu-item", index),
                    RowSpec {
                        icon: cmd.leading(),
                        label: cmd.label().into(),
                        checked: cmd.checked(),
                        highlighted: active,
                        danger: cmd.danger(),
                        disabled: false,
                        hint: cmd.hint().map(Into::into),
                        chevron: false,
                    },
                )
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    item_editor.update(cx, |this, cx| this.run_block_cmd(id, mode, cmd, window, cx));
                    cx.stop_propagation();
                });
                list = list.child(item);
            }
        }
    }
    let pos_editor = editor.clone();
    dropdown::anchored_motion_with(
        ("block-menu-pop", pop_gen),
        move |_, cx| {
            let ed = pos_editor.read(cx);
            let trigger = live_block_bounds(editor_id, id)
                .or_else(|| ed.layouts.get(&id).map(|l| l.bounds))
                .unwrap_or(init_trigger);
            dropdown::PlacementSpec::below(trigger)
                .viewport(ed.list.viewport_bounds())
                .gap(px(4.))
                .reserve_height(px(dropdown::MAX_HEIGHT))
        },
        list,
        phase,
        2,
    )
}
