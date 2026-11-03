//! Sidebar chrome: resizable edge, notebook switcher and window-level overlays.
use gpui::{
    AnyElement, Context, CursorStyle, FontWeight, MouseButton, Pixels, Window,
    anchored, deferred, div, point, prelude::*, px,
};
use crate::icons::{Icon, icon};
use crate::theme::Theme;
use super::{Workspace, file_ops::display_name, update_state_width};
use super::menu::{ItemId, menu_item, menu_surface, separator};

pub(super) const MIN_WIDTH: f32 = 200.;
pub(super) const MAX_WIDTH: f32 = 420.;
const SWITCHER_TOP: f32 = 40. + 12. + 32. + 4.;

pub(super) fn clamp_width(width: Pixels) -> Pixels { width.clamp(px(MIN_WIDTH), px(MAX_WIDTH)) }

impl Workspace {
    /// Invisible 8px strip over the sidebar's right border.
    pub(super) fn resize_handle(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let active = self.resizing.is_some();
        div().id("sidebar-resize").absolute().top_0().right(px(-4.)).w(px(8.)).h_full()
            .cursor(CursorStyle::ResizeLeftRight).group("sidebar-resize")
            .on_mouse_down(MouseButton::Left, cx.listener(|this, event: &gpui::MouseDownEvent, _, cx| {
                cx.stop_propagation();
                if event.click_count >= 2 {
                    this.resizing = None;
                    this.sidebar_width = Theme::ocean().sidebar_width;
                    update_state_width(None, cx);
                } else {
                    this.resizing = Some((event.position.x, this.sidebar_width));
                }
                cx.notify();
            }))
            .child(div().absolute().top_0().left(px(3.)).w(px(2.)).h_full()
                .when(active, |line| line.bg(theme.accent))
                .when(!active, |line| line.group_hover("sidebar-resize", |style| style.bg(theme.menu_border))))
    }

    /// Full-window capture while dragging, so the cursor and moves never get lost.
    pub(super) fn resize_capture(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        self.resizing?;
        Some(div().id("resize-capture").absolute().top_0().left_0().size_full().occlude()
            .cursor(CursorStyle::ResizeLeftRight)
            .on_mouse_move(cx.listener(|this, event: &gpui::MouseMoveEvent, _, cx| {
                let Some((start, width)) = this.resizing else { return };
                if event.pressed_button != Some(MouseButton::Left) { this.finish_resize(cx); return; }
                this.sidebar_width = clamp_width(width + (event.position.x - start));
                cx.notify();
            }))
            .on_mouse_up(MouseButton::Left, cx.listener(|this, _, _, cx| this.finish_resize(cx)))
            .into_any_element())
    }

    fn finish_resize(&mut self, cx: &mut Context<Self>) {
        if self.resizing.take().is_none() { return; }
        let width = self.sidebar_width;
        let default = Theme::ocean().sidebar_width;
        update_state_width(if width == default { None } else { Some(f32::from(width)) }, cx);
        cx.notify();
    }

    pub(super) fn notebook_switcher(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let name = self.folder.as_ref().map(|path| display_name(path, true)).unwrap_or_else(|| "选择笔记本".into());
        let enabled = !self.busy();
        let open = self.switcher_open;
        div().id("notebook-switcher").flex_1().min_w_0().h(px(36.)).px(px(10.)).flex().items_center().gap(px(8.))
            .rounded(px(8.)).border_1().border_color(theme.btn_border).bg(if open { theme.hover } else { theme.paper })
            .text_size(px(13.)).font_weight(FontWeight(460.))
            .when(enabled, |b| b.cursor(CursorStyle::PointingHand).hover(|style| style.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                    cx.stop_propagation();
                    let open = !this.switcher_open;
                    this.close_overlays(cx);
                    this.switcher_open = open;
                    cx.notify();
                })))
            .when(!enabled, |b| b.opacity(0.5))
            .child(icon(Icon::FolderOpen, px(18.), theme.icon_colors(theme.text_secondary)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(name))
            .child(icon(Icon::ChevronDown, px(14.), theme.icon_colors(theme.text_secondary)))
    }

    /// Window-level overlays: outside-click catcher, switcher list, tree menu, search.
    pub(super) fn overlays(&self, _: &mut Window, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let mut out = Vec::new();
        if self.switcher_open || self.menu.is_some() {
            out.push(div().id("overlay-dismiss").absolute().top_0().left_0().size_full()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.close_overlays(cx)))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| this.close_overlays(cx)))
                .into_any_element());
        }
        if self.switcher_open { out.push(self.switcher_menu(cx)); }
        if let Some(menu) = &self.menu { out.extend(self.tree_menu(&menu.target, menu.position, menu.moving, cx)); }
        out.extend(self.search_view(cx));
        out.extend(self.settings_view(cx));
        out
    }

    fn switcher_menu(&self, cx: &mut Context<Self>) -> AnyElement {
        let current = self.folder.clone();
        let mut items: Vec<AnyElement> = self.notebooks.iter().map(|path| {
            let checked = current.as_ref() == Some(path);
            let target = path.clone();
            menu_item(ItemId::Path(path.clone()), Icon::Folder, display_name(path, true), checked, false,
                cx.listener(move |this, _, _, cx| {
                    this.close_overlays(cx);
                    if this.folder.as_ref() != Some(&target) { this.use_folder(target.clone(), cx); }
                }))
                .group("notebook-item")
                // Other notebooks can be dropped from the recent list (the folder itself is untouched).
                .when(!checked, |item| item.child(forget_button(path.clone(), cx)))
                .into_any_element()
        }).collect();
        if !items.is_empty() { items.push(separator()); }
        items.push(menu_item(ItemId::Name("add-notebook"), Icon::Plus, "选择文件夹…".into(), false, false,
            cx.listener(|this, _, window, cx| { this.close_overlays(cx); this.choose_folder(window, cx); })).into_any_element());
        let width = self.sidebar_width - px(24.);
        deferred(anchored().position(point(px(12.), px(SWITCHER_TOP))).snap_to_window_with_margin(px(8.))
            .child(menu_surface(width).children(items))).with_priority(2).into_any_element()
    }
}

fn forget_button(path: std::path::PathBuf, cx: &mut Context<Workspace>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(gpui::ElementId::Name(format!("forget:{}", path.display()).into()))
        .size(px(20.)).flex_none().flex().items_center().justify_center().rounded(px(4.))
        .invisible().group_hover("notebook-item", |style| style.visible())
        .hover(|style| style.bg(theme.btn_hover))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
            cx.stop_propagation();
            this.notebooks.retain(|known| known != &path);
            let path = path.clone();
            cx.background_executor().spawn(async move {
                let _ = crate::files::update_state(|state| state.notebooks.retain(|known| known != &path));
            }).detach();
            cx.notify();
        }))
        .child(icon(Icon::Close, px(12.), theme.icon_colors(theme.text_secondary)))
}
