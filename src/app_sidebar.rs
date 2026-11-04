//! Sidebar chrome: resizable edge, notebook switcher and window-level overlays.
use gpui::{
    AnyElement, Context, CursorStyle, FontWeight, MouseButton, Pixels, SharedString,
    Window, WindowControlArea, div, point, prelude::*, px,
};
use crate::icons::{Icon, stateful_icon};
use crate::theme::{Theme, tr};
use super::{Workspace, file_ops::display_name, tabs::hover_tinted_icon, update_state_width};
use super::menu::{ItemId, menu_item, menu_surface, separator};

pub(super) const MIN_WIDTH: f32 = 242.;
pub(super) const MAX_WIDTH: f32 = 420.;
const TITLEBAR_LEFT_PAD: f32 = if cfg!(target_os = "macos") { 76. } else { 10. };
const TITLEBAR_RIGHT_PAD: f32 = 10.;
const TOGGLE_SIZE: f32 = 26.;
const NAV_BUTTON_SIZE: f32 = 24.;
const TITLEBAR_GAP: f32 = 2.;

/// Collapsed header hugs its content: traffic lights, optional nav buttons, then the toggle.
fn collapsed_header_width(nav_buttons: usize) -> f32 {
    let nav = nav_buttons as f32;
    TITLEBAR_LEFT_PAD + TITLEBAR_RIGHT_PAD + TOGGLE_SIZE + nav * (NAV_BUTTON_SIZE + TITLEBAR_GAP)
}

pub(super) fn clamp_width(width: Pixels) -> Pixels { width.clamp(px(MIN_WIDTH), px(MAX_WIDTH)) }

impl Workspace {
    pub(crate) fn toggle_sidebar(&mut self, cx: &mut Context<Self>) {
        self.switcher_open = false;
        self.menu = None;
        self.resizing = None;
        self.sidebar_collapsed = !self.sidebar_collapsed;
        cx.notify();
    }

    /// Top-left titlebar cell housing traffic lights and the Version B sidebar toggle button.
    pub(super) fn titlebar_left(&self, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let width = if self.sidebar_collapsed { px(collapsed_header_width(self.nav_button_count())) } else { self.sidebar_width };
        let glyph = if self.sidebar_collapsed { Icon::SidebarExpand } else { Icon::SidebarCollapse };
        let btn_group: SharedString = "sidebar-toggle-btn".into();
        div().id("titlebar-left").w(width).h_full().flex_none().flex().items_center()
            .pl(px(TITLEBAR_LEFT_PAD)).pr(px(TITLEBAR_RIGHT_PAD)).border_r_1().border_color(theme.sidebar_border)
            .window_control_area(WindowControlArea::Drag)
            .on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); })
            .child(div().ml_auto().flex_none().flex().items_center().gap(px(TITLEBAR_GAP))
                .children(self.render_nav_buttons(cx))
                .child(div().id("sidebar-toggle").relative().children(crate::ui::smoke_overlay::probe("sidebar-toggle")).group(btn_group.clone()).size(px(TOGGLE_SIZE)).flex_none()
                    .flex().items_center().justify_center().rounded(px(6.)).cursor(CursorStyle::PointingHand)
                    .hover(|s| s.bg(theme.hover))
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                        cx.stop_propagation();
                        this.toggle_sidebar(cx);
                    }))
                    .child(hover_tinted_icon(btn_group, glyph, 16., theme.text_secondary, theme.text, theme.icon_fill))))
    }

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
        let name = self.folder.as_ref().map(|path| display_name(path, true)).unwrap_or_else(|| tr("选择笔记本", "Select Notebook").into());
        let enabled = self.ui_enabled();
        let open = self.switcher_open;
        let note_count = self.folder.as_ref().map(|_| self.tree.read(cx).note_count().to_string());
        div().id("notebook-switcher").group("notebook-switcher").relative().children(crate::ui::smoke_overlay::probe("notebook-switcher")).flex_1().min_w_0().h(px(30.)).px(px(8.)).flex().items_center().gap(px(7.))
            .rounded(px(6.)).text_size(px(14.)).font_weight(FontWeight(500.)).text_color(theme.text)
            .when(open, |b| b.bg(theme.hover))
            .when(enabled, |b| b.cursor(CursorStyle::PointingHand).hover(|style| style.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
                    cx.stop_propagation();
                    let open = !this.switcher_open;
                    this.close_overlays(cx);
                    this.switcher_open = open;
                    cx.notify();
                })))
            .when(!enabled, |b| b.opacity(0.5))
            .child(stateful_icon("notebook-switcher", Icon::FolderOpen, px(15.), open, theme.mono_icon_colors(theme.text), theme.icon_colors(theme.text)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(name))
            .when_some(note_count, |b, count| {
                b.child(div().flex_none().text_size(px(14.)).font_weight(FontWeight::NORMAL)
                    .text_color(theme.text_secondary).child(count))
            })
            .child(stateful_icon("notebook-switcher", Icon::ChevronUp, px(12.), open, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text_secondary)))
    }

    /// Window-level overlays: outside-click catcher, switcher list, tree menu, search.
    pub(super) fn overlays(&self, window: &mut Window, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let mut out = Vec::new();
        if self.switcher_open || self.menu.is_some() {
            out.push(div().id("overlay-dismiss").absolute().top_0().left_0().size_full()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| this.close_overlays(cx)))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| this.close_overlays(cx)))
                .into_any_element());
        }
        if self.switcher_open { out.push(self.switcher_menu(window, cx)); }
        if let Some(menu) = &self.menu { out.extend(self.tree_menu(&menu.target, menu.position, menu.moving, cx)); }
        out.extend(self.search_view(cx));
        out
    }

    fn switcher_menu(&self, window: &Window, cx: &mut Context<Self>) -> AnyElement {
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
        items.push(menu_item(ItemId::Name("add-notebook"), Icon::FolderPlus, tr("选择文件夹…", "Choose Folder…").into(), false, false,
            cx.listener(|this, _, window, cx| { this.close_overlays(cx); this.choose_folder(window, cx); })).into_any_element());
        let row_count = self.notebooks.len() + 1;
        let sep_h = if self.notebooks.is_empty() { 0. } else { 11. };
        let est_h = (8. + row_count as f32 * 30. - 2. + sep_h).min(crate::ui::dropdown::MAX_HEIGHT);
        let y = (window.viewport_size().height - px(48. + est_h)).max(px(8.));
        let width = self.sidebar_width - px(16.);
        crate::ui::dropdown::deferred_popover(point(px(8.), y), menu_surface(width).children(items), 2).into_any_element()
    }
}

fn forget_button(path: std::path::PathBuf, cx: &mut Context<Workspace>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let group: SharedString = format!("forget:{}", path.display()).into();
    div().id(gpui::ElementId::Name(group.clone())).group(group.clone())
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
        .child(stateful_icon(group, Icon::Close, px(12.), false, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))
}
