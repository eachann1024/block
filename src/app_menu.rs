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
    on_click: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    menu_item_hint(id, glyph, label, checked, destructive, None, on_click)
}

fn menu_item_hint(
    id: ItemId,
    glyph: Icon,
    label: String,
    checked: bool,
    destructive: bool,
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
        let move_bounds = std::rc::Rc::new(std::cell::Cell::new(None));
        let mut items: Vec<AnyElement> = Vec::new();
        if !dir {
            let p = path.clone();
            let open_hint = if cfg!(target_os = "macos") { tr("⌘+点击", "⌘+Click") } else { tr("Ctrl+点击", "Ctrl+Click") };
            items.push(menu_item_hint(ItemId::Name("open-new-tab"), Icon::NotePlus, tr("在新标签页打开", "Open in New Tab").into(), false, false, Some(open_hint),
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
        items.push(menu_item(ItemId::Name("reveal"), Icon::FolderReveal, crate::files::ops::reveal_label().into(), false, false,
            cx.listener(move |this, _, _, cx| { this.close_overlays(cx); crate::files::ops::reveal_in_finder(&p); }))
            .on_hover(cx.listener(|this, hovered: &bool, _, cx| {
                if *hovered { if let Some(menu) = &mut this.menu { if menu.moving { menu.moving = false; cx.notify(); } } }
            }))
            .into_any_element());
        let p = path.clone();
        let rename_hint = if cfg!(target_os = "macos") { "↵" } else { "F2" };
        items.push(menu_item_hint(ItemId::Name("rename"), Icon::Pencil, tr("重命名", "Rename").into(), false, false, Some(rename_hint),
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
        items.push(menu_item_hint(ItemId::Name("trash"), Icon::Trash, crate::files::ops::trash_label().into(), false, true, Some(trash_hint),
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
        let items = choices.into_iter().map(|(folder, depth, name, is_root)| {
            let (from, dest) = (path.to_path_buf(), folder.clone());
            menu_item(ItemId::Path(folder), if is_root { Icon::FolderOpen } else { Icon::Folder }, name, false, false,
                cx.listener(move |this, _, _, cx| this.move_item(from.clone(), dest.clone(), cx)))
                .pl(px(10. + depth.min(6) as f32 * 12.)).into_any_element()
        }).collect::<Vec<_>>();
        let empty = items.is_empty();
        dropdown::shell("move-list").w(px(MENU_WIDTH)).max_h(px(300.)).overflow_y_scroll()
            .children(items)
            .when(empty, |s| s.child(div().h(px(32.)).px(px(10.)).flex().items_center()
                .text_color(Theme::ocean().text_secondary).child(tr("没有可移动到的位置", "No destination available"))))
            .into_any_element()
    }
}
