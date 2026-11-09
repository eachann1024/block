//! Menu surfaces: the tree context menu with its "移动到…" list, plus the
//! shared surface/item/separator used by the notebook switcher.
use std::path::{Path, PathBuf};
use gpui::{
    AnyElement, App, Context, MouseButton, Pixels, Stateful, Div, Window, div,
    prelude::*, px,
};
use crate::{
    dropdown::{self, RowSpec},
    icons::Icon,
    theme::{Theme, tr},
};
use super::{Workspace, file_ops::display_name};

const MENU_WIDTH: f32 = 232.;

pub(super) enum ItemId { Path(PathBuf), Name(&'static str) }

pub(super) fn menu_surface(width: Pixels) -> Stateful<Div> {
    dropdown::shell("workspace-menu").w(width)
}

pub(super) fn separator() -> AnyElement {
    dropdown::separator().into_any_element()
}

pub(super) fn menu_item(
    id: ItemId,
    glyph: Icon,
    label: String,
    checked: bool,
    destructive: bool,
    highlighted: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    menu_item_hint(id, glyph, label, checked, destructive, highlighted, None, on_click)
}

fn menu_item_hint(
    id: ItemId,
    glyph: Icon,
    label: String,
    checked: bool,
    destructive: bool,
    highlighted: bool,
    hint: Option<&'static str>,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let id: gpui::ElementId = match id {
        ItemId::Path(path) => gpui::ElementId::Name(format!("menu:{}", path.display()).into()),
        ItemId::Name(name) => name.into(),
    };
    dropdown::row_with(
        id,
        RowSpec {
            icon: Some(glyph),
            label: label.into(),
            checked,
            danger: destructive,
            highlighted,
            hint: hint.map(Into::into),
            ..Default::default()
        },
    )
    .on_mouse_down(MouseButton::Left, move |event, window, cx| {
        cx.stop_propagation();
        on_click(event, window, cx);
    })
}

impl Workspace {
    pub(super) fn tree_menu(&self, target: &(PathBuf, bool), position: gpui::Point<Pixels>, moving: bool,
        phase: crate::ui::motion::PopupMotion, pop_gen: u64, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let (path, dir) = target.clone();
        let cursor = sync_tree_nav(pop_gen);
        let reveal_ix = if dir { 0 } else { 2 };
        let move_bounds = std::rc::Rc::new(std::cell::Cell::new(None));
        let mut items: Vec<AnyElement> = Vec::new();
        if !dir {
            let p = path.clone();
            let open_hint = if cfg!(target_os = "macos") { tr("⌘+点击", "⌘+Click") } else { tr("Ctrl+点击", "Ctrl+Click") };
            items.push(menu_item_hint(ItemId::Name("open-new-tab"), Icon::NotePlus, tr("在新标签页打开", "Open in New Tab").into(), false, false, cursor == 0, Some(open_hint),
                cx.listener(move |this, _, window, cx| {
                    this.close_overlays(cx);
                    this.close_settings(window, cx);
                    this.open_file_in_new_tab(p.clone(), cx);
                }))
                .on_hover(cx.listener(|this, hovered: &bool, _, cx| {
                if *hovered { if let Some(menu) = &mut this.menu { if menu.moving { menu.moving = false; cx.notify(); } } }
            }))
            .into_any_element());
            items.push(separator());
        }
        let p = path.clone();
        items.push(menu_item(ItemId::Name("reveal"), Icon::FolderReveal, crate::files::ops::reveal_label().into(), false, false, cursor == reveal_ix,
            cx.listener(move |this, _, _, cx| { this.close_overlays(cx); crate::files::ops::reveal_in_finder(&p); }))
            .on_hover(cx.listener(|this, hovered: &bool, _, cx| {
                if *hovered { if let Some(menu) = &mut this.menu { if menu.moving { menu.moving = false; cx.notify(); } } }
            }))
            .into_any_element());
        let p = path.clone();
        let rename_hint = if cfg!(target_os = "macos") { "↵" } else { "F2" };
        items.push(menu_item_hint(ItemId::Name("rename"), Icon::Pencil, tr("重命名", "Rename").into(), false, false, cursor == reveal_ix + 1, Some(rename_hint),
            cx.listener(move |this, _, window, cx| this.start_rename(p.clone(), dir, window, cx)))
            .on_hover(cx.listener(|this, hovered: &bool, _, cx| {
                if *hovered { if let Some(menu) = &mut this.menu { if menu.moving { menu.moving = false; cx.notify(); } } }
            }))
            .into_any_element());
        items.push(dropdown::row_with(
            "move",
            RowSpec {
                icon: Some(Icon::ArrowRight),
                label: tr("移动到…", "Move to…").into(),
                chevron: true,
                highlighted: cursor == reveal_ix + 2,
                ..Default::default()
            },
        )
        .relative().children(crate::ui::smoke_overlay::probe("tree-move-row")).child(crate::ui::placement::track_bounds(move_bounds.clone(), false))
        .on_hover(cx.listener(|this, hovered: &bool, _, cx| {
            if *hovered { if let Some(menu) = &mut this.menu { if !menu.moving { menu.moving = true; cx.notify(); } } }
        }))
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
            cx.stop_propagation();
            if let Some(menu) = &mut this.menu { menu.moving = true; }
            cx.notify();
        }))
        .into_any_element());
        items.push(separator());
        let p = path.clone();
        let trash_hint = if cfg!(target_os = "macos") { "⌘⌫" } else { "Del" };
        items.push(menu_item_hint(ItemId::Name("trash"), Icon::Trash, crate::files::ops::trash_label().into(), false, true, cursor == reveal_ix + 4, Some(trash_hint),
            cx.listener(move |this, _, _, cx| this.trash(p.clone(), cx)))
            .on_hover(cx.listener(|this, hovered: &bool, _, cx| {
                if *hovered { if let Some(menu) = &mut this.menu { if menu.moving { menu.moving = false; cx.notify(); } } }
            }))
            .into_any_element());
        let main_spec = crate::dropdown::PlacementSpec::point(position);
        let mut out = vec![crate::dropdown::anchored_motion(
            ("tree-menu-pop", pop_gen),
            main_spec,
            menu_surface(px(MENU_WIDTH)).children(items),
            phase,
            2,
        )];
        let move_open = moving && phase == crate::ui::motion::PopupMotion::Enter;
        if let Some((move_phase, move_gen)) = crate::ui::motion::popup_motion("tree-move-menu", move_open) {
            crate::ui::motion::schedule_popup_finish("tree-move-menu", move_gen, cx);
            out.push(crate::dropdown::anchored_motion_with(("tree-move-pop", move_gen), move |_, _| {
                let row = move_bounds.get().unwrap_or(gpui::Bounds::new(position, gpui::size(px(MENU_WIDTH), px(28.))));
                crate::dropdown::PlacementSpec::below(row)
                    .align_x(crate::dropdown::AlignX::CascadeRight { gap: px(8.) })
            }, self.move_list(&path, dir, cx), move_phase, 3));
        }
        out
    }

    /// Destinations within the current notebook: the notebook root (when moving
    /// out of a subfolder), plus every folder except the item itself, its
    /// descendants and its current parent.
    fn move_list(&self, path: &Path, dir: bool, cx: &mut Context<Self>) -> AnyElement {
        let Some(root) = self.folder.clone() else { return div().into_any_element() };
        let current = path.parent().map(Path::to_path_buf);
        let mut choices = Vec::new();
        if current.as_deref() != Some(root.as_path()) {
            choices.push((root.clone(), 0usize, display_name(&root, true), true));
        }
        for (folder, depth, name) in self.tree.read(cx).folders() {
            if dir && folder.starts_with(path) { continue; }
            if current.as_deref() == Some(folder.as_path()) { continue; }
            choices.push((folder, depth, name, false));
        }
        let move_cursor = move_nav_cursor();
        let items = choices.into_iter().enumerate().map(|(index, (folder, depth, name, is_root))| {
            let (from, dest) = (path.to_path_buf(), folder.clone());
            menu_item(ItemId::Path(folder), if is_root { Icon::FolderOpen } else { Icon::Folder }, name, false, false, index == move_cursor,
                cx.listener(move |this, _, _, cx| this.move_item(from.clone(), dest.clone(), cx)))
                .pl(px(10. + depth.min(6) as f32 * 12.)).into_any_element()
        }).collect::<Vec<_>>();
        let empty = items.is_empty();
        dropdown::shell("move-list").w(px(MENU_WIDTH)).max_h(px(300.)).overflow_y_scroll()
            .track_scroll(&move_scroll())
            .children(items)
            .when(empty, |s| s.child(div().h(px(32.)).px(px(10.)).flex().items_center()
                .text_color(Theme::ocean().text_secondary).child(tr("没有可移动到的位置", "No destination available"))))
            .into_any_element()
    }

    pub(super) fn close_tree_submenu(&mut self, cx: &mut Context<Self>) -> bool {
        if self.menu.as_ref().is_some_and(|menu| menu.moving) {
            if let Some(menu) = &mut self.menu { menu.moving = false; }
            cx.notify();
            true
        } else {
            false
        }
    }

    pub(super) fn step_surface_menu(&mut self, down: bool, cx: &mut Context<Self>) -> bool {
        if self.renaming.is_some() { return false; }
        if self.menu.is_some() { self.step_tree_menu(down, cx); return true; }
        if self.tab_menu_open() { self.step_tab_menu(down, cx); return true; }
        if self.switcher_open { self.step_switcher(down, cx); return true; }
        false
    }

    pub(super) fn activate_surface_menu(&mut self, window: &mut Window, cx: &mut Context<Self>) -> bool {
        if self.renaming.is_some() { return false; }
        if self.menu.is_some() { self.activate_tree_menu(window, cx); return true; }
        if self.tab_menu_open() { self.activate_tab_menu(window, cx); return true; }
        if self.switcher_open { self.activate_switcher(window, cx); return true; }
        false
    }

    fn step_tree_menu(&mut self, down: bool, cx: &mut Context<Self>) {
        let Some(menu) = self.menu.clone() else { return };
        if menu.moving {
            let count = self.move_choices(&menu.target.0, menu.target.1, cx).len();
            if count == 0 { cx.notify(); return; }
            let cur = move_nav_cursor();
            let next = crate::ui::menu_nav::step_uniform(cur, count, down);
            if next != cur.min(count - 1) { crate::ui::menu_nav::reveal_or_defer(&move_scroll(), next, count); }
            set_move_cursor(next);
            cx.notify();
            return;
        }
        let slots = tree_slots(menu.target.1);
        set_tree_cursor(crate::ui::menu_nav::step(tree_cursor(), &slots, down));
        cx.notify();
    }

    fn activate_tree_menu(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(menu) = self.menu.clone() else { return };
        let (path, dir) = menu.target.clone();
        if menu.moving {
            let choices = self.move_choices(&path, dir, cx);
            if let Some((dest, _, _, _)) = choices.get(move_nav_cursor().min(choices.len().saturating_sub(1))) {
                let dest = dest.clone();
                self.move_item(path, dest, cx);
            }
            return;
        }
        let index = crate::ui::menu_nav::clamp_index(tree_cursor(), &tree_slots(dir));
        let reveal_ix = if dir { 0 } else { 2 };
        if !dir && index == 0 {
            self.close_overlays(cx);
            self.close_settings(window, cx);
            self.open_file_in_new_tab(path, cx);
        } else if index == reveal_ix {
            self.close_overlays(cx);
            crate::files::ops::reveal_in_finder(&path);
        } else if index == reveal_ix + 1 {
            self.start_rename(path, dir, window, cx);
        } else if index == reveal_ix + 2 {
            if let Some(menu) = &mut self.menu { menu.moving = true; }
            set_move_cursor(0);
            move_scroll().set_offset(Default::default());
            cx.notify();
        } else if index == reveal_ix + 4 {
            self.trash(path, cx);
        }
    }

    fn move_choices(&self, path: &Path, dir: bool, cx: &gpui::App) -> Vec<(PathBuf, usize, String, bool)> {
        let Some(root) = self.folder.clone() else { return Vec::new() };
        let current = path.parent().map(Path::to_path_buf);
        let mut choices = Vec::new();
        if current.as_deref() != Some(root.as_path()) {
            choices.push((root.clone(), 0usize, display_name(&root, true), true));
        }
        for (folder, depth, name) in self.tree.read(cx).folders() {
            if dir && folder.starts_with(path) { continue; }
            if current.as_deref() == Some(folder.as_path()) { continue; }
            choices.push((folder, depth, name, false));
        }
        choices
    }

    fn step_switcher(&mut self, down: bool, cx: &mut Context<Self>) {
        let slots = switcher_slots(self.notebooks.len());
        if !crate::ui::menu_nav::has_item(&slots) { return; }
        let next = crate::ui::menu_nav::step(switcher_cursor(), &slots, down);
        let changed = next != crate::ui::menu_nav::clamp_index(switcher_cursor(), &slots);
        set_switcher_cursor(next);
        if changed { crate::ui::menu_nav::reveal_or_defer(&switcher_scroll(), next, slots.len()); }
        cx.notify();
    }

    fn activate_switcher(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let n = self.notebooks.len();
        let index = crate::ui::menu_nav::clamp_index(switcher_cursor(), &switcher_slots(n));
        if index < n {
            let target = self.notebooks[index].clone();
            self.close_overlays(cx);
            if self.folder.as_ref() != Some(&target) { self.use_folder(target, cx); }
        } else {
            self.close_overlays(cx);
            self.choose_folder(window, cx);
        }
    }
}

struct SurfaceNav {
    tree_gen: u64,
    cursor: usize,
    move_cursor: usize,
    move_scroll: gpui::ScrollHandle,
    switcher_gen: u64,
    switcher_cursor: usize,
    switcher_scroll: gpui::ScrollHandle,
}

impl Default for SurfaceNav {
    fn default() -> Self {
        Self {
            tree_gen: u64::MAX, cursor: 0, move_cursor: 0, move_scroll: gpui::ScrollHandle::new(),
            switcher_gen: u64::MAX, switcher_cursor: 0, switcher_scroll: gpui::ScrollHandle::new(),
        }
    }
}

thread_local! {
    static SURFACE: std::cell::RefCell<SurfaceNav> = std::cell::RefCell::new(SurfaceNav::default());
}

fn tree_slots(dir: bool) -> Vec<crate::ui::menu_nav::Slot> {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    if dir { vec![Item, Item, Item, Skip, Item] } else { vec![Item, Skip, Item, Item, Item, Skip, Item] }
}

fn switcher_slots(notebooks: usize) -> Vec<crate::ui::menu_nav::Slot> {
    use crate::ui::menu_nav::Slot::{Item, Skip};
    if notebooks == 0 { return vec![Item]; }
    let mut slots = vec![Item; notebooks];
    slots.push(Skip);
    slots.push(Item);
    slots
}

fn sync_tree_nav(pop_gen: u64) -> usize {
    SURFACE.with(|nav| {
        let mut nav = nav.borrow_mut();
        if nav.tree_gen != pop_gen {
            nav.tree_gen = pop_gen;
            nav.cursor = 0;
            nav.move_cursor = 0;
            nav.move_scroll.set_offset(Default::default());
        }
        nav.cursor
    })
}

fn tree_cursor() -> usize { SURFACE.with(|nav| nav.borrow().cursor) }
fn set_tree_cursor(index: usize) { SURFACE.with(|nav| nav.borrow_mut().cursor = index); }
fn move_nav_cursor() -> usize { SURFACE.with(|nav| nav.borrow().move_cursor) }
fn set_move_cursor(index: usize) { SURFACE.with(|nav| nav.borrow_mut().move_cursor = index); }
fn move_scroll() -> gpui::ScrollHandle { SURFACE.with(|nav| nav.borrow().move_scroll.clone()) }

pub(super) fn sync_switcher_nav(pop_gen: u64) -> usize {
    SURFACE.with(|nav| {
        let mut nav = nav.borrow_mut();
        if nav.switcher_gen != pop_gen {
            nav.switcher_gen = pop_gen;
            nav.switcher_cursor = 0;
            nav.switcher_scroll.set_offset(Default::default());
        }
        nav.switcher_cursor
    })
}

fn switcher_cursor() -> usize { SURFACE.with(|nav| nav.borrow().switcher_cursor) }
fn set_switcher_cursor(index: usize) { SURFACE.with(|nav| nav.borrow_mut().switcher_cursor = index); }
pub(super) fn switcher_scroll() -> gpui::ScrollHandle { SURFACE.with(|nav| nav.borrow().switcher_scroll.clone()) }
