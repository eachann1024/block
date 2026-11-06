use gpui::{
    AnimationExt, Context, Focusable, IntoElement, MouseButton, NavigationDirection, ParentElement, Render, Window, div, prelude::*, px,
};
use crate::icons::{Icon, stateful_icon};
use crate::theme::{Theme, format_hotkey, tr};
use crate::ui::icon_button::{dock_icon_button, toolbar_icon_button};
use super::Workspace;
use super::search::{Dismiss, NewFolder, NewNote, OpenSearch, OpenSettings, kbd};

impl Render for Workspace {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.sync_history_focus(window, cx);
        let theme = Theme::ocean();
        let export = crate::export::active_view(cx);
        let ai_fullscreen = crate::editor::ai::chat::is_fullscreen(cx) && self.settings.is_none() && !self.is_history_open();
        let root = div().size_full().relative().flex().flex_col().overflow_hidden().bg(theme.shell).text_color(theme.text)
            .key_context("Workspace").track_focus(&self.root_focus)
            .on_action(cx.listener(|this, _: &OpenSearch, window, cx| this.open_search(window, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::Find, window, cx| { crate::search::window::reveal_main_window(window, cx); this.editor.update(cx, |e, cx| e.open_find(window, cx)); }))
            .on_action(cx.listener(|this, _: &OpenSettings, window, cx| { crate::search::window::reveal_main_window(window, cx); this.open_settings(window, cx); }))
            .on_action(cx.listener(|this, _: &crate::editor::ai::OpenAiSettings, window, cx| { crate::search::window::reveal_main_window(window, cx); this.open_settings_tab(super::settings::SettingsTab::Ai, window, cx); }))
            .on_action(cx.listener(|this, _: &crate::editor::ai::ToggleAiPanel, window, cx| { crate::search::window::reveal_main_window(window, cx); crate::editor::ai::chat::toggle(this.editor.downgrade(), window, cx); cx.notify(); }))
            .on_action(cx.listener(|this, _: &NewNote, window, cx| { crate::search::window::reveal_main_window(window, cx); if !this.busy() { this.new_item(false, window, cx); } }))
            .on_action(cx.listener(|this, _: &NewFolder, window, cx| { crate::search::window::reveal_main_window(window, cx); if !this.busy() { this.new_item(true, window, cx); } }))
            .on_action(cx.listener(|this, _: &crate::editor::view::Paste, w, cx| this.route_edit_action(crate::editor::view::EditorAction::Paste, w, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::Copy, w, cx| this.route_edit_action(crate::editor::view::EditorAction::Copy, w, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::Cut, w, cx| this.route_edit_action(crate::editor::view::EditorAction::Cut, w, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::SelectAll, w, cx| this.route_edit_action(crate::editor::view::EditorAction::SelectAll, w, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::Undo, w, cx| this.route_edit_action(crate::editor::view::EditorAction::Undo, w, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::Redo, w, cx| this.route_edit_action(crate::editor::view::EditorAction::Redo, w, cx)))
            .capture_action(cx.listener(|this, _: &Dismiss, window, cx| {
                if crate::updater::close_modal() { this.restore_focus(window, cx); cx.notify(); cx.stop_propagation(); }
                else if this.dismiss(window, cx) { cx.stop_propagation(); }
            }))
            .capture_action(cx.listener(|this, _: &crate::editor::view::Cancel, window, cx| {
                if crate::updater::close_modal() { this.restore_focus(window, cx); cx.notify(); cx.stop_propagation(); }
                else if this.dismiss(window, cx) { cx.stop_propagation(); }
            }))
            .capture_action(cx.listener(|this, _: &gpui_component::input::Escape, window, cx| {
                if crate::updater::close_modal() { this.restore_focus(window, cx); cx.notify(); cx.stop_propagation(); }
                else if this.dismiss(window, cx) { cx.stop_propagation(); }
            }))
            .on_mouse_down(MouseButton::Navigate(NavigationDirection::Back), cx.listener(|this, _, _, cx| this.navigate_back(cx)))
            .on_mouse_down(MouseButton::Navigate(NavigationDirection::Forward), cx.listener(|this, _, _, cx| this.navigate_forward(cx)));
        self.bind_tab_actions(root, cx)
            .child(self.titlebar(cx))
            .child(div().flex_1().min_h_0().flex()
                .when(!ai_fullscreen && !self.sidebar_collapsed && !self.is_history_open(), |d| d.child(if self.settings.is_some() { self.settings_sidebar(cx) } else { self.sidebar(cx) }))
                .when(!ai_fullscreen, |d| d.child(self.settings_view(cx).or_else(|| self.mascot_view(cx)).unwrap_or_else(|| {
                    if self.is_history_open() {
                        self.render_history_main(window, cx)
                    } else {
                        div().flex_1().min_w_0().h_full().flex().flex_col().relative()
                            .children(self.render_history_toast(cx))
                            .child(self.editor.clone())
                            .when(self.history_restore_busy, |col| col.child(div().absolute().top_0().left_0().size_full().occlude()))
                            .into_any_element()
                    }
                })))
                .children(crate::editor::ai::chat::panel(cx)))
            // The export view has its own title row and back button, so it covers the window.
            .children(export.map(|view| {
                gpui::deferred(div().absolute().top_0().left_0().size_full().occlude().bg(theme.shell).child(view)).with_priority(4)
            }))
            .children(self.overlays(window, cx))
            .children(self.resize_capture(cx))
            .children(crate::editor::ai::chat::resize_capture(cx))
            .children(crate::ui::smoke_overlay::render())
            .child(gpui::deferred(div().on_children_prepainted(|_, window, cx| sync_focused_input_ime(window, cx))).with_priority(10))
    }
}

fn sync_focused_input_ime(window: &mut Window, cx: &mut gpui::App) {
    let Some(any) = gpui_component::WindowExt::focused_input(window, cx) else { return };
    if let Some(focused_input) = any.as_input().cloned() {
        focused_input.update(cx, |state, cx| {
            if let Some(g) = state.prepaint_caret_geometry() {
                let cb = g.cursor_bounds();
                crate::sync_ime_cursor_area(f32::from(cb.origin.x), f32::from(cb.origin.y + g.scroll_offset().y), f32::from(cb.size.height.max(g.line_height())));
                return;
            }
            let ib = state.input_bounds();
            let range = gpui::EntityInputHandler::marked_text_range(state, window, cx)
                .or_else(|| gpui::EntityInputHandler::selected_text_range(state, false, window, cx).map(|s| s.range))
                .unwrap_or(0..0);
            if let Some(b) = gpui::EntityInputHandler::bounds_for_range(state, range, ib, window, cx) {
                crate::sync_ime_cursor_area(f32::from(b.origin.x), f32::from(b.origin.y), f32::from(b.size.height));
            } else if ib.size.height > px(0.) {
                crate::sync_ime_cursor_area(f32::from(ib.origin.x), f32::from(ib.origin.y), f32::from(ib.size.height));
            }
        });
    } else if let Some(focused_textarea) = any.as_textarea().cloned() {
        focused_textarea.update(cx, |state, cx| {
            if let Some(g) = state.prepaint_caret_geometry() {
                let cb = g.cursor_bounds();
                crate::sync_ime_cursor_area(f32::from(cb.origin.x), f32::from(cb.origin.y + g.scroll_offset().y), f32::from(cb.size.height.max(g.line_height())));
                return;
            }
            let ib = state.input_bounds();
            let range = gpui::EntityInputHandler::marked_text_range(state, window, cx)
                .or_else(|| gpui::EntityInputHandler::selected_text_range(state, false, window, cx).map(|s| s.range))
                .unwrap_or(0..0);
            if let Some(b) = gpui::EntityInputHandler::bounds_for_range(state, range, ib, window, cx) {
                crate::sync_ime_cursor_area(f32::from(b.origin.x), f32::from(b.origin.y), f32::from(b.size.height));
            } else if ib.size.height > px(0.) {
                crate::sync_ime_cursor_area(f32::from(ib.origin.x), f32::from(ib.origin.y), f32::from(ib.size.height));
            }
        });
    }
}

impl Workspace {
    fn route_edit_action(&mut self, action: crate::editor::view::EditorAction, window: &mut Window, cx: &mut Context<Self>) {
        if crate::quick::is_quick_panel_active() && crate::quick::dispatch_editor_action(&action, cx) { return; }
        if crate::editor::paste_clipboard::forward_focused_input_action(&action, window, cx) { return; }
        if self.settings.is_none() && !self.is_history_open() && !crate::editor::ai::chat::is_fullscreen(cx) && crate::export::active_view(cx).is_none() {
            window.focus(&self.editor.focus_handle(cx), cx);
            self.editor.update(cx, |e, cx| e.handle(&action, window, cx));
        }
    }

    fn sidebar(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let enabled = self.ui_enabled();
        let new_note_tip = if cfg!(target_os = "macos") { tr("新建笔记 ⌘N", "New Note ⌘N") } else { tr("新建笔记 Ctrl+N", "New Note Ctrl+N") };
        div().w(self.sidebar_width).flex_none().h_full().relative().flex().flex_col()
            .bg(theme.sidebar).border_r_1().border_color(theme.sidebar_border)
            .child(div().flex_none().pt(px(10.)).px(px(10.)).pb(px(8.)).flex().items_center().gap(px(6.))
                .child(self.search_button(enabled, cx))
                .child(toolbar_icon_button("new-folder", Icon::FolderPlus, enabled, tr("新建文件夹", "New Folder"), cx.listener(|this, _, window, cx| {
                    this.new_item(true, window, cx);
                })))
                .child(toolbar_icon_button("new-note", Icon::NotePlus, enabled, new_note_tip, cx.listener(|this, _, window, cx| {
                    this.new_item(false, window, cx);
                }))))
            // Workspace progress/errors float over the list bottom so showing or
            // clearing them never reflows (shakes) the file rows.
            .child(div().flex_1().min_h_0().overflow_hidden().relative().pt(px(0.)).pb(px(8.)).child(self.tree.clone())
                .when(!self.notice.is_empty(), |list| list.child(
                    div().id("workspace-status").absolute().left_0().right_0().bottom_0().max_h(px(48.)).overflow_y_scroll()
                        .px(px(20.)).py(px(4.)).bg(theme.sidebar)
                        .text_size(px(12.)).text_color(theme.text_secondary).child(self.notice.clone()))))
            .child(self.settings_footer(cx))
            .child(self.resize_handle(cx))
    }

    fn search_button(&self, enabled: bool, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let shortcut = format_hotkey("secondary-k");
        let search_open = self.search.is_some();
        div().id("search-button").group("sidebar-search-btn").relative().children(crate::ui::smoke_overlay::probe("search-button")).flex_1().min_w_0().h(px(32.)).px(px(9.)).flex().items_center().gap(px(6.))
            .rounded(crate::ui::radius::CONTROL).bg(theme.paper).border_1().border_color(if search_open { theme.accent } else { theme.btn_border })
            .text_size(px(14.)).text_color(theme.text_secondary)
            .when(enabled, |b| b.cursor(gpui::CursorStyle::PointingHand).hover(|style| style.bg(theme.btn_hover))
                .on_mouse_down(gpui::MouseButton::Left, cx.listener(|this, _, window, cx| this.open_search(window, cx))))
            .when(!enabled, |b| b.opacity(0.5))
            .child(stateful_icon("sidebar-search-btn", Icon::FindSearch, px(14.), search_open, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(theme.text)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(tr("搜索", "Search")))
            .child(kbd(shortcut))
    }

    fn settings_footer(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let tip = if cfg!(target_os = "macos") { tr("设置 ⌘,", "Settings ⌘,") } else { tr("设置 Ctrl+,", "Settings Ctrl+,") };
        div().flex_none().px(px(8.)).py(px(6.)).border_t_1().border_color(theme.hairline)
            .flex().items_center().gap(px(4.)).bg(theme.sidebar)
            .child(self.notebook_switcher(cx))
            .when(crate::updater::has_update(), |f| {
                let open_updater = std::rc::Rc::new(cx.listener(|_, _, _, cx| crate::updater::open(cx)));
                let badge = {
                    let open_updater = open_updater.clone();
                    move |y_offset: f32, btn_opacity: f32| {
                        let on_down = open_updater.clone();
                        div()
                            .id("updater-badge-button")
                            .group("updater-badge-btn")
                            .relative()
                            .size(px(30.))
                            .flex_none()
                            .flex()
                            .items_center()
                            .justify_center()
                            .rounded(crate::ui::radius::CONTROL)
                            .cursor(gpui::CursorStyle::PointingHand)
                            .bg(theme.paper)
                            .border_1()
                            .border_color(theme.btn_border)
                            .hover(|s| s.bg(theme.hover).border_color(theme.accent))
                            .tooltip(crate::ui::tooltip::tooltip(tr("有可用更新", "Update available")))
                            .on_mouse_down(gpui::MouseButton::Left, move |e, w, cx| on_down(e, w, cx))
                            .child(stateful_icon(
                                "updater-badge-btn",
                                Icon::Download,
                                px(16.),
                                false,
                                theme.mono_icon_colors(theme.text_secondary),
                                theme.icon_colors(theme.text),
                            ))
                            .child(div().absolute().top(px(3.)).right(px(3.)).size(px(5.)).rounded_full().bg(theme.accent))
                            .opacity(btn_opacity)
                            .top(px(y_offset))
                    }
                };
                if crate::updater::entrance_done() {
                    f.child(badge(0.0, 1.0))
                } else {
                f.child(
                    div()
                        .relative()
                        .size(px(30.))
                        .flex_none()
                        .with_animation(
                            "updater-badge-entrance",
                            gpui::Animation::new(std::time::Duration::from_millis(1750)),
                            move |container, delta| {
                                let frame = crate::updater::drive_entrance(delta);
                                let mut root = container;

                                // 1. 7 dual-tone bubbles spray up from the badge, then leave.
                                if frame.bubbles {
                                    const PUFFS: [(f32, f32, f32, bool); 7] = [
                                        (-18., -20., 10., true),
                                        (-7., -28., 14., false),
                                        (9., -27., 10., true),
                                        (21., -16., 13., false),
                                        (-23., -4., 9., true),
                                        (24., 2., 11., false),
                                        (0., -32., 10., true),
                                    ];
                                    let t = frame.bubble_t;
                                    let fade = frame.bubble_fade;
                                    for (dx, dy, full_sz, alt) in PUFFS {
                                        let scale = 0.2 + 0.8 * t;
                                        let sz = full_sz * scale;
                                        let x = 15. + dx * t - sz * 0.5;
                                        let y = 18. + dy * t - sz * 0.5;
                                        let fill = if alt { theme.icon_fill } else { theme.accent };
                                        let base_alpha = if alt { 0.68 } else { 0.34 };
                                        root = root.child(
                                            div()
                                                .absolute()
                                                .rounded_full()
                                                .border_1()
                                                .left(px(x))
                                                .top(px(y))
                                                .size(px(sz))
                                                .bg(fill.opacity(base_alpha * fade))
                                                .border_color(theme.accent.opacity(0.52 * fade))
                                                .opacity(fade),
                                        );
                                    }
                                }

                                // 2. Badge and its accent dot leap, bounce, then stay.
                                root = root.child(badge(frame.y_offset, frame.btn_opacity));

                                // 3. Three-layer doodle jitters in, fades out, and never returns.
                                if frame.tangle {
                                    root = root.child(
                                        div()
                                            .absolute()
                                            .size(px(56.))
                                            .left(px(-13. + frame.jx))
                                            .top(px(-13. + frame.jy))
                                            .opacity(frame.tangle_opacity)
                                            .child(crate::icons::icon(
                                                Icon::UpdateTangle,
                                                px(56.),
                                                theme.icon_colors(theme.text),
                                            )),
                                    );
                                }
                                root
                            },
                        ),
                )
                }
            })
            .child(div().w(px(1.)).h(px(16.)).flex_none().bg(theme.sidebar_border))
            .child(dock_icon_button("settings-button", Icon::Sliders, self.settings.is_some(), tip,
                cx.listener(|this, _, window, cx| this.open_settings(window, cx))))
    }
}
