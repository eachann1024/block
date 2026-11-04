//! Block menu rendering: the floating slash/handle dropdown list and separators.
use gpui::{AnyElement, Entity, MouseButton, prelude::*, px};
use crate::{
    dropdown::{self, RowSpec},
    editor::view::Editor,
};
use super::{BlockMenu, Cmd, SLASH_MENU_WIDTH};

pub(in crate::editor) fn overlay(editor: Entity<Editor>, menu: BlockMenu, entries: Vec<Option<Cmd>>) -> AnyElement {
    let id = menu.block;
    let mode = menu.mode;
    let selectable_count = entries.iter().flatten().count();
    let active_idx = menu.index.min(selectable_count.saturating_sub(1));
    let mut list = dropdown::shell("block-menu")
        .min_w(px(SLASH_MENU_WIDTH))
        .w(px(SLASH_MENU_WIDTH));
    let mut position = 0;
    for (index, entry) in entries.iter().copied().enumerate() {
        match entry {
            None => list = list.child(dropdown::separator()),
            Some(cmd) => {
                let active = position == active_idx;
                position += 1;
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
    dropdown::deferred_popover(menu.anchor, list, 2).into_any_element()
}
