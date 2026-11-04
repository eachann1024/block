//! Menu surfaces: the tree context menu with its "移动到…" list, plus the
//! shared surface/item/separator used by the notebook switcher.
use std::path::{Path, PathBuf};
use gpui::{
    AnyElement, App, Context, Hsla, MouseButton, Pixels, Stateful, Div, Window, anchored, deferred, div, point,
    prelude::*, px,
};
use crate::{
    dropdown::{self, RowSpec},
    icons::Icon,
    theme::{Theme, tr},
};
use super::{Workspace, file_ops::display_name};

const MENU_WIDTH: f32 = 188.;
const ITEM_GAP: f32 = 0.;

pub(super) enum ItemId { Path(PathBuf), Name(&'static str) }

pub(super) fn menu_surface(width: Pixels) -> Stateful<Div> {
    dropdown::shell("workspace-menu").w(width)
}

pub(super) fn separator() -> AnyElement {
    dropdown::separator().into_any_element()
}

fn shortcut_hint(label: &'static str) -> gpui::Div {
    let muted: Hsla = dropdown::palette().muted;
    div().ml_auto().pl(px(8.)).flex_none().text_size(px(12.)).text_color(muted).child(label)
}

pub(super) fn menu_item(
    id: ItemId,
    glyph: Icon,
    label: String,
    checked: bool,
    destructive: bool,
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
        cx: &mut Context<Self>) -> Vec<AnyElement> {
        let (path, dir) = target.clone();
        let mut items: Vec<AnyElement> = Vec::new();
        if !dir {
            let p = path.clone();
            items.push(menu_item(ItemId::Name("open-new-tab"), Icon::NotePlus, tr("在新标签页打开", "Open in New Tab").into(), false, false,
                cx.listener(move |this, _, window, cx| {
                    this.close_overlays(cx);
                    this.close_settings(window, cx);
                    this.open_file_in_new_tab(p.clone(), cx);
                }))
                .child(shortcut_hint(if cfg!(target_os = "macos") { tr("⌘+点击", "⌘+Click") } else { tr("Ctrl+点击", "Ctrl+Click") }))
                .into_any_element());
            let p = path.clone();
            items.push(menu_item(ItemId::Name("export-image"), Icon::Image, tr("生成图片…", "Export as Image…").into(), false, false,
                cx.listener(move |this, _, window, cx| this.export_note(p.clone(), window, cx)))
                .into_any_element());
            items.push(separator());
        }
        let p = path.clone();
        items.push(menu_item(ItemId::Name("reveal"), Icon::FolderReveal, crate::files::ops::reveal_label().into(), false, false,
            cx.listener(move |this, _, _, cx| { this.close_overlays(cx); crate::files::ops::reveal_in_finder(&p); }))
            .into_any_element());
        let p = path.clone();
        items.push(menu_item(ItemId::Name("rename"), Icon::Pencil, tr("重命名", "Rename").into(), false, false,
            cx.listener(move |this, _, window, cx| this.start_rename(p.clone(), dir, window, cx)))
            .child(shortcut_hint(if cfg!(target_os = "macos") { "↵" } else { "F2" }))
            .into_any_element());
        items.push(menu_item(ItemId::Name("move"), Icon::ArrowRight, tr("移动到…", "Move to…").into(), moving, false,
            cx.listener(|this, _, _, cx| {
                if let Some(menu) = &mut this.menu { menu.moving = !menu.moving; }
                cx.notify();
            })).into_any_element());
        items.push(separator());
        let p = path.clone();
        items.push(menu_item(ItemId::Name("trash"), Icon::Trash, crate::files::ops::trash_label().into(), false, true,
            cx.listener(move |this, _, _, cx| this.trash(p.clone(), cx)))
            .child(shortcut_hint(if cfg!(target_os = "macos") { "⌘⌫" } else { "Del" }))
            .into_any_element());
        let mut out = vec![deferred(anchored().position(position).snap_to_window_with_margin(px(8.))
            .child(menu_surface(px(MENU_WIDTH)).children(items))).with_priority(2).into_any_element()];
        if moving {
            let move_row = if dir { 2. } else { 4. };
            let sep_offset = if dir { 0. } else { 9. };
            let at = point(position.x + px(MENU_WIDTH + 4.), position.y + px(4. + sep_offset + move_row * (32. + ITEM_GAP)));
            out.push(deferred(anchored().position(at).snap_to_window_with_margin(px(8.))
                .child(self.move_list(&path, dir, cx))).with_priority(3).into_any_element());
        }
        out
    }

    /// Destinations: the notebook root, then every workspace folder except the
    /// item itself, its descendants and its current parent.
    fn move_list(&self, path: &Path, dir: bool, cx: &mut Context<Self>) -> AnyElement {
        let Some(root) = self.folder.clone() else { return div().into_any_element() };
        let current = path.parent().map(Path::to_path_buf);
        let mut choices = vec![(root.clone(), 0usize, display_name(&root, true))];
        for (folder, depth, name) in self.tree.read(cx).folders() {
            if dir && folder.starts_with(path) { continue; }
            choices.push((folder, depth + 1, name));
        }
        let items = choices.into_iter().filter(|(folder, ..)| current.as_deref() != Some(folder.as_path()))
            .map(|(folder, depth, name)| {
                let (from, dest) = (path.to_path_buf(), folder.clone());
                menu_item(ItemId::Path(folder), if depth == 0 { Icon::FolderOpen } else { Icon::Folder }, name, false, false,
                    cx.listener(move |this, _, _, cx| this.move_item(from.clone(), dest.clone(), cx)))
                    .pl(px(10. + depth.min(6) as f32 * 12.)).into_any_element()
            }).collect::<Vec<_>>();
        let empty = items.is_empty();
        menu_surface(px(MENU_WIDTH)).id("move-list").max_h(px(300.)).overflow_y_scroll()
            .children(items)
            .when(empty, |s| s.child(div().h(px(32.)).px(px(10.)).flex().items_center()
                .text_color(Theme::ocean().text_secondary).child(tr("没有可移动到的位置", "No destination available"))))
            .into_any_element()
    }
}
