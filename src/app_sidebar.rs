//! Sidebar chrome: resizable edge, notebook switcher and window-level overlays.
use crate::ui::tooltip::TooltipExt;
use gpui::{
    AnyElement, Context, CursorStyle, FontWeight, MouseButton, Pixels, SharedString,
    Window, WindowControlArea, div, point, prelude::*, px,
};
use crate::icons::{Icon, stateful_icon};
use crate::theme::{Theme, format_hotkey, tr};
use super::{Workspace, file_ops::display_name, tabs::hover_tinted_icon, update_state_width};
use super::menu::{ItemId, menu_item, menu_surface, separator};

pub(super) const MIN_WIDTH: f32 = 240.;
pub(super) const MAX_WIDTH: f32 = 420.;
const TITLEBAR_LEFT_PAD: f32 = if cfg!(any(target_os = "macos", target_family = "wasm")) { 82. } else { 10. };
const TITLEBAR_RIGHT_PAD: f32 = 0.;
const TOGGLE_SIZE: f32 = 26.;
const NAV_BUTTON_SIZE: f32 = 24.;
const TITLEBAR_GAP: f32 = 2.;

/// Collapsed header hugs its content: traffic lights, optional nav buttons, then the toggle.
fn collapsed_header_width(nav_buttons: usize) -> f32 {
    let nav = nav_buttons as f32;
    TITLEBAR_LEFT_PAD + TITLEBAR_RIGHT_PAD + TOGGLE_SIZE + nav * (NAV_BUTTON_SIZE + TITLEBAR_GAP)
}

pub(super) fn clamp_width(width: Pixels) -> Pixels { width.clamp(px(MIN_WIDTH), px(MAX_WIDTH)) }

thread_local! { static SWITCHER_BOUNDS: std::rc::Rc<std::cell::Cell<Option<gpui::Bounds<Pixels>>>> = Default::default(); }

impl Workspace {
    /// Open the sidebar even when the window has no column for it.
    pub(crate) fn present_sidebar(&mut self) {
        self.sidebar_collapsed = false;
        self.sidebar_pinned = true;
    }

    pub(crate) fn toggle_sidebar(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.is_history_open() || self.history_restore_busy { return; }
        self.switcher_open = false;
        self.menu = None;
        self.resizing = None;
        let split = self.shell_split(window, cx);
        let showing = !self.sidebar_collapsed && (!split.sidebar_yielded || split.sidebar_drawer);
        if showing {
            self.sidebar_collapsed = true;
            self.sidebar_pinned = false;
        } else {
            self.sidebar_collapsed = false;
            self.sidebar_pinned = true;
        }
        let collapsed = self.sidebar_collapsed;
        cx.background_executor().spawn(async move {
            let _ = crate::files::update_state(|state| state.sidebar_collapsed = collapsed);
        }).detach();
        cx.notify();
    }

    /// Top-left titlebar cell housing traffic lights and the Version B sidebar toggle button.
    pub(super) fn titlebar_left(&self, wide: bool, auto_hidden: bool, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let immersive = self.ai_immersive(cx);
        let width = if immersive {
            px(TITLEBAR_LEFT_PAD)
        } else if wide {
            self.sidebar_width
        } else {
            px(collapsed_header_width(self.nav_button_count()))
        };
        let glyph = if wide { Icon::SidebarCollapse } else { Icon::SidebarExpand };
        let btn_group: SharedString = "sidebar-toggle-btn".into();
        let tip = if auto_hidden {
            if self.settings.is_some() {
                tr("窗口较窄，设置导航已让出。点击以浮层打开", "Settings navigation stepped aside. Click to open it over the page").to_string()
            } else {
                tr("窗口较窄，侧栏已让出。点击打开，笔记保持可读宽度", "The sidebar stepped aside so the note stays readable. Click to open it").to_string()
            }
        } else {
            format!("{} {}", tr("侧边栏", "Sidebar"), format_hotkey("alt-b"))
        };
        div().id("titlebar-left").w(width).h_full().flex_none().relative().flex().items_center()
            .when(!immersive, |cell| cell.pl(px(TITLEBAR_LEFT_PAD)).pr(px(TITLEBAR_RIGHT_PAD)).border_r_1().border_color(theme.sidebar_border))
            .window_control_area(WindowControlArea::Drag)
            .on_click(|event, window, _| if event.click_count() == 2 { window.titlebar_double_click(); })
            .when(cfg!(target_family = "wasm"), |d| d.child(
                div().absolute().left(px(14.)).top_0().bottom_0().flex().items_center().gap(px(8.))
                    .child(div().size(px(12.)).rounded_full().bg(gpui::rgb(0xff5f57)))
                    .child(div().size(px(12.)).rounded_full().bg(gpui::rgb(0xfebc2e)))
                    .child(div().size(px(12.)).rounded_full().bg(gpui::rgb(0x28c840)))
            ))
            .when(!immersive, |cell| cell.child(div().ml_auto().flex_none().flex().items_center().gap(px(TITLEBAR_GAP))
                .children(self.render_nav_buttons(cx))
                .child(div().id("sidebar-toggle").relative().children(crate::ui::smoke_overlay::probe("sidebar-toggle")).group(btn_group.clone()).size(px(TOGGLE_SIZE)).flex_none()
                    .flex().items_center().justify_center().rounded(crate::ui::radius::CONTROL).cursor(CursorStyle::PointingHand)
                    .hover(|s| s.bg(theme.hover))
                    .tooltip_above(crate::theme::tooltip(tip))
                    .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                        cx.stop_propagation();
                        this.toggle_sidebar(window, cx);
                    }))
                    .child(hover_tinted_icon(btn_group, glyph, 18., theme.text_secondary, theme.text, theme.icon_fill)))))
    }

    /// Invisible 8px strip over the sidebar's right border.
    pub(super) fn resize_handle(&self, cx: &mut Context<Self>) -> impl IntoElement {
        crate::ui::resize_handle::resize_handle("sidebar-resize", crate::ui::resize_handle::Edge::Right, self.resizing.is_some())
            .on_mouse_down(MouseButton::Left, cx.listener(|this, event: &gpui::MouseDownEvent, _, cx| {
                cx.stop_propagation();
                if event.click_count >= 2 {
                    this.resizing = None;
                    this.sidebar_width = Theme::ocean().sidebar_width;
                    update_state_width(Some(f32::from(this.sidebar_width)), cx);
                } else {
                    this.resizing = Some((event.position.x, this.sidebar_width));
                }
                cx.notify();
            }))
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
        update_state_width(Some(f32::from(self.sidebar_width)), cx);
        cx.notify();
    }

    pub(super) fn notebook_switcher(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let name = self.folder.as_ref().map(|path| display_name(path, true)).unwrap_or_else(|| tr("选择笔记本", "Select Notebook").into());
        let enabled = self.ui_enabled();
        let open = self.switcher_open;
        div().id("notebook-switcher").group("notebook-switcher").relative().child(crate::ui::placement::track_bounds(SWITCHER_BOUNDS.with(Clone::clone), open)).children(crate::ui::smoke_overlay::probe("notebook-switcher")).flex_1().min_w_0().h(px(30.)).px(px(8.)).flex().items_center().gap(px(7.))
            .rounded(crate::ui::radius::CONTROL).text_size(px(14.)).font_weight(FontWeight(500.)).text_color(theme.text)
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
            .child(stateful_icon("notebook-switcher", Icon::FolderOpen, px(16.), open, theme.mono_icon_colors(theme.text), theme.icon_colors(theme.text)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(name))
            .child(stateful_icon("notebook-switcher", Icon::ChevronUp, px(16.), open, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text_secondary)))
    }

    /// Window-level overlays: outside-click catcher, switcher list, tree menu, search.
    pub(super) fn overlays(&self, window: &mut Window, cx: &mut Context<Self>) -> Vec<AnyElement> {
        let mut out = Vec::new();
        if let Some((phase, pop_gen)) = crate::ui::motion::popup_motion("workspace-menus", self.switcher_open || self.menu.is_some()) {
            crate::ui::motion::schedule_popup_finish("workspace-menus", pop_gen, cx);
            let scrim = div().id("overlay-dismiss").absolute().top_0().left_0().size_full().occlude()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.close_overlays(cx); }))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.close_overlays(cx); }));
            out.push(gpui::deferred(crate::ui::motion::layer_fade(scrim, ("workspace-menus-scrim", pop_gen), phase == crate::ui::motion::PopupMotion::Enter))
                .with_priority(1)
                .into_any_element());
        }
        if let Some((phase, pop_gen)) = crate::ui::motion::popup_motion("notebook-switcher", self.switcher_open) {
            crate::ui::motion::schedule_popup_finish("notebook-switcher", pop_gen, cx);
            out.push(self.switcher_menu(window, phase, pop_gen, cx));
        }
        if let Some((phase, pop_gen, menu)) = crate::ui::motion::popup_hold("tree-menu", self.menu.clone()) {
            crate::ui::motion::schedule_popup_finish("tree-menu", pop_gen, cx);
            out.extend(self.tree_menu(&menu.target, menu.position, menu.moving, phase, pop_gen, cx));
        }
        out.extend(self.search_view(cx));
        out.extend(crate::about::view(cx));
        out.extend(crate::performance::view(window, cx));
        out.extend(crate::updater::view(cx));
        out.extend(crate::feedback_view::modal_overlay(window, cx));
        out.extend(self.render_history_confirm_modal(cx));
        out
    }

    pub(crate) fn focus_root(&self, window: &mut Window, cx: &mut Context<Self>) { window.focus(&self.root_focus, cx); }

    fn switcher_menu(&self, window: &Window, phase: crate::ui::motion::PopupMotion, pop_gen: u64, cx: &mut Context<Self>) -> AnyElement {
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
        let width = self.sidebar_width;
        let fallback = gpui::Bounds::new(point(px(0.), window.viewport_size().height - px(38.)), gpui::size(width, px(30.)));
        crate::ui::dropdown::anchored_motion_with(("notebook-switcher-pop", pop_gen), move |_, _| {
            crate::ui::dropdown::PlacementSpec::above(SWITCHER_BOUNDS.with(|b| b.get()).unwrap_or(fallback))
        }, menu_surface(width).children(items), phase, 2)
    }
}

fn forget_button(path: std::path::PathBuf, cx: &mut Context<Workspace>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let group: SharedString = format!("forget:{}", path.display()).into();
    div().id(gpui::ElementId::Name(group.clone())).group(group.clone())
        .size(px(20.)).flex_none().flex().items_center().justify_center().rounded(crate::ui::radius::MICRO_SM)
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
