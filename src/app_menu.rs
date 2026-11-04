//! Menu surfaces: the tree context menu with its "移动到…" list, plus the
//! shared surface/item/separator used by the notebook switcher.
use std::path::{Path, PathBuf};
use gpui::{
    AnyElement, App, Context, CursorStyle, Hsla, MouseButton, Pixels, Window, anchored, deferred, div, point,
    prelude::*, px,
};
use crate::icons::{Icon, icon};
use crate::theme::Theme;
use super::{Workspace, file_ops::display_name};

const MENU_WIDTH: f32 = 188.;
const ITEM_GAP: f32 = 1.;

pub(super) fn danger() -> Hsla { gpui::rgb(0xc2362b).into() }

pub(super) enum ItemId { Path(PathBuf), Name(&'static str) }

pub(super) fn menu_surface(width: Pixels) -> gpui::Div {
    let theme = Theme::ocean();
    div().w(width).p(px(4.)).flex().flex_col().gap(px(ITEM_GAP)).rounded(px(8.)).bg(theme.menu)
        .border_1().border_color(theme.menu_border).shadow_md().occlude()
        .text_size(px(13.)).text_color(theme.text)
}

pub(super) fn separator() -> AnyElement {
    div().mx(px(6.)).my(px(3.)).h(px(1.)).bg(Theme::ocean().hairline).into_any_element()
}

fn shortcut_hint(label: &'static str) -> gpui::Div {
    let muted: Hsla = if crate::theme::is_dark() {
        Theme::ocean().text_secondary
    } else {
        gpui::rgb(0x6d7784).into()
    };
    div().ml_auto().flex_none().text_size(px(12.)).text_color(muted).child(label)
}

pub(super) fn menu_item(id: ItemId, glyph: Icon, label: String, checked: bool, destructive: bool,
    on_click: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let fg = if destructive { danger() } else { theme.text };
    let id: gpui::ElementId = match id {
        ItemId::Path(path) => gpui::ElementId::Name(format!("menu:{}", path.display()).into()),
        ItemId::Name(name) => name.into(),
    };
    div().id(id).h(theme.menu_item_height).px(px(10.)).flex().items_center().gap(px(8.)).rounded(px(6.))
        .text_color(fg).cursor(CursorStyle::PointingHand)
        .when(checked, |b| b.bg(theme.selected))
        .hover(|style| style.bg(theme.hover))
        .on_mouse_down(MouseButton::Left, move |event, window, cx| { cx.stop_propagation(); on_click(event, window, cx); })
        .child(div().flex_none().child(icon(glyph, px(15.), theme.icon_colors(if destructive { fg } else { theme.text_secondary }))))
        .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(label))
        .when(checked, |b| b.child(div().flex_none().child(icon(Icon::Check, px(15.), theme.icon_colors(theme.accent)))))
}

impl Workspace {
    pub(super) fn tree_menu(&self, target: &(PathBuf, bool), position: gpui::Point<Pixels>, moving: bool,
        cx: &mut Context<Self>) -> Vec<AnyElement> {
        let (path, dir) = target.clone();
        let mut items: Vec<AnyElement> = Vec::new();
        let p = path.clone();
        items.push(menu_item(ItemId::Name("reveal"), Icon::FolderReveal, "在访达中显示".into(), false, false,
            cx.listener(move |this, _, _, cx| { this.close_overlays(cx); crate::files::ops::reveal_in_finder(&p); }))
            .into_any_element());
        let p = path.clone();
        items.push(menu_item(ItemId::Name("rename"), Icon::Pencil, "重命名".into(), false, false,
            cx.listener(move |this, _, window, cx| this.start_rename(p.clone(), dir, window, cx)))
            .child(shortcut_hint("↵"))
            .into_any_element());
        items.push(menu_item(ItemId::Name("move"), Icon::ArrowRight, "移动到…".into(), moving, false,
            cx.listener(|this, _, _, cx| {
                if let Some(menu) = &mut this.menu { menu.moving = !menu.moving; }
                cx.notify();
            })).into_any_element());
        items.push(separator());
        let p = path.clone();
        items.push(menu_item(ItemId::Name("trash"), Icon::Trash, "移到废纸篓".into(), false, true,
            cx.listener(move |this, _, _, cx| this.trash(p.clone(), cx)))
            .child(shortcut_hint("⌘⌫"))
            .into_any_element());
        let mut out = vec![deferred(anchored().position(position).snap_to_window_with_margin(px(8.))
            .child(menu_surface(px(MENU_WIDTH)).children(items))).with_priority(2).into_any_element()];
        if moving {
            // Third row of the parent menu: 4px padding + two rows and gaps.
            let at = point(position.x + px(MENU_WIDTH + 4.), position.y + px(4. + 2. * (32. + ITEM_GAP)));
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
                .text_color(Theme::ocean().text_secondary).child("没有可移动到的位置")))
            .into_any_element()
    }
}
