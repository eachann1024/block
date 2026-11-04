use gpui::{
    Context, IntoElement, MouseButton, NavigationDirection, ParentElement, Render, Window, div, prelude::*, px,
};
use crate::icons::{Icon, stateful_icon};
use crate::theme::{Theme, format_hotkey, tr};
use crate::ui::icon_button::{dock_icon_button, toolbar_icon_button};
use super::Workspace;
use super::search::{Dismiss, NewFolder, NewNote, OpenSearch, OpenSettings, kbd};

impl Render for Workspace {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        let export = crate::export::active_view(cx);
        let root = div().size_full().relative().flex().flex_col().overflow_hidden().bg(theme.shell).text_color(theme.text)
            .key_context("Workspace").track_focus(&self.root_focus)
            .on_action(cx.listener(|this, _: &OpenSearch, window, cx| this.open_search(window, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::view::Find, window, cx| this.editor.update(cx, |e, cx| e.open_find(window, cx))))
            .on_action(cx.listener(|this, _: &OpenSettings, window, cx| this.open_settings(window, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::ai::OpenAiSettings, window, cx| this.open_settings_tab(super::settings::SettingsTab::Ai, window, cx)))
            .on_action(cx.listener(|this, _: &crate::editor::ai::ToggleAiPanel, window, cx| { crate::editor::ai::chat::toggle(this.editor.downgrade(), window, cx); cx.notify(); }))
            .on_action(cx.listener(|this, _: &NewNote, window, cx| if !this.busy() { this.new_item(false, window, cx) }))
            .on_action(cx.listener(|this, _: &NewFolder, window, cx| if !this.busy() { this.new_item(true, window, cx) }))
            .on_action(cx.listener(|this, _: &Dismiss, window, cx| if !this.dismiss(window, cx) { cx.propagate() }))
            .on_mouse_down(MouseButton::Navigate(NavigationDirection::Back), cx.listener(|this, _, _, cx| this.navigate_back(cx)))
            .on_mouse_down(MouseButton::Navigate(NavigationDirection::Forward), cx.listener(|this, _, _, cx| this.navigate_forward(cx)));
        self.bind_tab_actions(root, cx)
            .child(self.titlebar(cx))
            .child(div().flex_1().min_h_0().flex()
                .when(!self.sidebar_collapsed, |d| d.child(if self.settings.is_some() { self.settings_sidebar(cx) } else { self.sidebar(cx) }))
                .child(self.settings_view(cx).or_else(|| self.mascot_view(cx)).unwrap_or_else(|| {
                    div().flex_1().min_w_0().h_full().child(self.editor.clone()).into_any_element()
                }))
                .children(crate::editor::ai::chat::panel(cx)))
            // The export view has its own title row and back button, so it covers the window.
            .children(export.map(|view| {
                div().absolute().top_0().left_0().size_full().occlude().bg(theme.shell).child(view)
            }))
            .children(self.overlays(window, cx))
            .children(self.resize_capture(cx))
            .children(crate::ui::smoke_overlay::render())
    }
}

impl Workspace {
    fn sidebar(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let enabled = self.ui_enabled();
        let new_note_tip = if cfg!(target_os = "macos") { tr("新建笔记 ⌘N", "New Note ⌘N") } else { tr("新建笔记 Ctrl+N", "New Note Ctrl+N") };
        div().w(self.sidebar_width).flex_none().h_full().relative().flex().flex_col()
            .bg(theme.sidebar).border_r_1().border_color(theme.sidebar_border)
            .child(div().flex_none().pt(px(10.)).px(px(10.)).pb(px(8.)).flex().items_center().gap(px(6.))
                .child(self.search_button(enabled, cx))
                .child(toolbar_icon_button("new-note", Icon::NotePlus, enabled, new_note_tip, cx.listener(|this, _, window, cx| {
                    this.new_item(false, window, cx);
                })))
                .child(toolbar_icon_button("new-folder", Icon::FolderPlus, enabled, tr("新建文件夹", "New Folder"), cx.listener(|this, _, window, cx| {
                    this.new_item(true, window, cx);
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
            .rounded(px(8.)).bg(theme.paper).border_1().border_color(if search_open { theme.accent } else { theme.btn_border })
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
            .child(div().w(px(1.)).h(px(16.)).flex_none().bg(theme.sidebar_border))
            .child(dock_icon_button("settings-button", Icon::Sliders, self.settings.is_some(), tip,
                cx.listener(|this, _, window, cx| this.open_settings(window, cx))))
    }
}
