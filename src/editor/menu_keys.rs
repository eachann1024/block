//! Routes editor Up, Down, and Enter to the menu that is actually open.
//! Link fields, image alt text, and the inline AI prompt keep their own keys.
use gpui::{Context, Window};
use crate::ui::menu_nav::{self, Slot};
use super::view::Editor;

impl Editor {
    pub(super) fn step_open_menu(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        if super::code_block::step_lang_picker(down, cx) { return true; }
        if self.block_menu_step(down, cx) { return true; }
        if self.step_table_menu(down, cx) { return true; }
        if crate::ui::callout_controls::step_menu(down) { cx.notify(); return true; }
        if self.step_image_menu(down, cx) { return true; }
        if self.step_format_menu(down, cx) { return true; }
        false
    }

    pub(super) fn activate_open_menu(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if let Some((id, tag, same)) = super::code_block::take_picked_lang(&self.doc) {
            self.run_block_cmd(id, super::block_menu::Mode::Handle, super::block_menu::Cmd::SetLanguage(tag, same), window, cx);
            return true;
        }
        if self.block_menu_pick(window, cx) { return true; }
        if self.activate_table_menu(window, cx) { return true; }
        if crate::ui::callout_controls::activate_menu(self, window, cx) { return true; }
        if self.activate_image_menu(window, cx) { return true; }
        if self.activate_format_menu(window, cx) { return true; }
        false
    }

    fn step_format_menu(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        let Some(slots) = self.format_slots() else { return false; };
        if !menu_nav::has_item(&slots) { return false; }
        let next = menu_nav::step(format_index(), &slots, down);
        let changed = next != menu_nav::clamp_index(format_index(), &slots);
        set_format_index(next);
        if changed { menu_nav::reveal_or_defer(&menu_nav::scroll("format"), next, slots.len()); }
        cx.notify();
        true
    }

    fn activate_format_menu(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        let Some(slots) = self.format_slots() else { return false; };
        let index = menu_nav::clamp_index(format_index(), &slots);
        set_format_index(index);
        match self.format.clone() {
            Some(super::format_cmds::FormatPop::Kinds { .. }) => {
                let Some(kind) = super::toolbar::menu_kinds(true).get(index).copied() else { return true; };
                self.pick_kind(kind, false, window, cx);
            }
            Some(super::format_cmds::FormatPop::Lists { .. }) => {
                let Some(kind) = super::toolbar::menu_kinds(false).get(index).copied() else { return true; };
                self.pick_kind(kind, true, window, cx);
            }
            Some(super::format_cmds::FormatPop::SelectionMenu { .. }) => super::format_ui::run_selection(self, index, window, cx),
            Some(super::format_cmds::FormatPop::Palette { .. }) => super::format_cmds::palette::activate_swatch(self, index, window, cx),
            _ => return false,
        }
        true
    }

    fn format_slots(&self) -> Option<Vec<Slot>> {
        let item = || Slot::Item;
        let skip = || Slot::Skip;
        match self.format {
            Some(super::format_cmds::FormatPop::Kinds { .. }) => Some(vec![item(); super::toolbar::menu_kinds(true).len()]),
            Some(super::format_cmds::FormatPop::Lists { .. }) => Some(vec![item(); super::toolbar::menu_kinds(false).len()]),
            Some(super::format_cmds::FormatPop::SelectionMenu { .. }) => Some(vec![item(), item(), skip(), item()]),
            Some(super::format_cmds::FormatPop::Palette { .. }) => Some(super::format_cmds::palette::palette_slots()),
            _ => None,
        }
    }
}

thread_local! {
    static FORMAT_INDEX: std::cell::Cell<usize> = const { std::cell::Cell::new(0) };
    static TABLE_INDEX: std::cell::Cell<usize> = const { std::cell::Cell::new(0) };
}

pub(super) fn format_index() -> usize { FORMAT_INDEX.with(|cell| cell.get()) }
pub(super) fn set_format_index(index: usize) { FORMAT_INDEX.with(|cell| cell.set(index)); }
pub(super) fn arm_format(index: usize) {
    set_format_index(index);
    menu_nav::reset_scroll("format");
}

pub(super) fn table_index() -> usize { TABLE_INDEX.with(|cell| cell.get()) }
pub(super) fn set_table_index(index: usize) { TABLE_INDEX.with(|cell| cell.set(index)); }

pub(super) fn arm_table(slots: &[Slot]) {
    TABLE_INDEX.with(|cell| cell.set(menu_nav::clamp_index(0, slots)));
    menu_nav::reset_scroll("table");
}
