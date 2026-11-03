use gpui::{
    App, Context, IntoElement, ParentElement, Render, Window, div, prelude::*, px,
};
use crate::icons::{Icon, icon};
use crate::theme::{Theme, format_hotkey, search_shortcut};
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
            .on_action(cx.listener(|this, _: &NewNote, window, cx| if !this.busy() { this.new_item(false, window, cx) }))
            .on_action(cx.listener(|this, _: &NewFolder, window, cx| if !this.busy() { this.new_item(true, window, cx) }))
            .on_action(cx.listener(|this, _: &Dismiss, window, cx| if !this.dismiss(window, cx) { cx.propagate() }));
        self.bind_tab_actions(root, cx)
            .child(self.titlebar(cx))
            .child(div().flex_1().min_h_0().flex()
                .child(self.sidebar(cx))
                .child(div().flex_1().min_w_0().h_full().child(self.editor.clone())))
            // The export view has its own title row and back button, so it covers the window.
            .children(export.map(|view| {
                div().absolute().top_0().left_0().size_full().occlude().bg(theme.shell).child(view)
            }))
            .children(self.overlays(window, cx))
            .children(self.resize_capture(cx))
    }
}

impl Workspace {
    fn sidebar(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let available = !self.busy();
        div().w(self.sidebar_width).flex_none().h_full().relative().flex().flex_col()
            .bg(theme.sidebar).border_r_1().border_color(theme.sidebar_border)
            .child(div().flex().gap(px(8.)).pt(px(12.)).px(px(12.)).pb(px(8.))
                .child(self.notebook_switcher(cx)))
            .child(self.search_button(available && self.folder.is_some(), cx))
            // Workspace progress/errors are only reported here, so keep a compact line.
            .when(!self.notice.is_empty(), |side| side.child(
                div().id("workspace-status").max_h(px(48.)).overflow_y_scroll().px(px(20.)).pb(px(4.))
                    .text_size(px(12.)).text_color(theme.text_secondary).child(self.notice.clone())))
            .child(self.group_row(available && self.folder.is_some(), cx))
            .child(div().flex_1().min_h_0().overflow_hidden().px(px(8.)).pb(px(8.)).child(self.tree.clone()))
            .child(self.settings_footer(cx))
            .child(self.resize_handle(cx))
    }

    fn search_button(&self, enabled: bool, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let shortcut = format_hotkey(&search_shortcut());
        div().id("search-button").mx(px(12.)).mb(px(8.)).h(px(32.)).flex_none().px(px(10.)).flex().items_center().gap(px(8.))
            .rounded(px(8.)).bg(theme.paper).border_1().border_color(theme.btn_border)
            .text_size(px(13.)).text_color(theme.text_secondary)
            .when(enabled, |b| b.cursor(gpui::CursorStyle::PointingHand).hover(|style| style.bg(theme.btn_hover))
                .on_mouse_down(gpui::MouseButton::Left, cx.listener(|this, _, window, cx| this.open_search(window, cx))))
            .when(!enabled, |b| b.opacity(0.5))
            .child(icon(Icon::FindSearch, px(14.), theme.icon_colors(theme.text_secondary)))
            .child(div().flex_1().child("搜索笔记"))
            .child(kbd(shortcut))
    }

    fn settings_footer(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        div().flex_none().px(px(8.)).py(px(6.)).border_t_1().border_color(theme.hairline)
            .child(div().id("settings-button").h(px(32.)).px(px(10.)).flex().items_center().gap(px(8.))
                .rounded(px(6.)).text_size(px(14.)).text_color(theme.text_secondary)
                .cursor(gpui::CursorStyle::PointingHand)
                .hover(move |s| s.bg(theme.hover).text_color(theme.text))
                .on_mouse_down(gpui::MouseButton::Left, cx.listener(|this, _, window, cx| this.open_settings(window, cx)))
                .child(icon(Icon::Sliders, px(15.), theme.icon_colors(theme.text_secondary)))
                .child(div().flex_1().child("偏好设置"))
                .child(kbd("⌘,")))
    }

    /// Notebook name with the two quick-create buttons on the right.
    fn group_row(&self, enabled: bool, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let name = self.folder.as_ref().map(|p| super::file_ops::display_name(p, true)).unwrap_or_default();
        div().flex_none().flex().items_center().gap(px(2.)).pl(px(20.)).pr(px(12.)).pt(px(4.)).pb(px(6.))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(12.)).text_color(theme.text_secondary).child(name))
            .child(icon_button("new-note", Icon::NotePlus, enabled, cx.listener(|this, _, window, cx| {
                this.new_item(false, window, cx);
            })))
            .child(icon_button("new-folder", Icon::FolderPlus, enabled, cx.listener(|this, _, window, cx| {
                this.new_item(true, window, cx);
            })))
    }
}

fn icon_button(id: &'static str, glyph: Icon, enabled: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(id).size(px(24.)).flex_none().flex().items_center().justify_center().rounded(px(6.))
        .when(enabled, |b| b.cursor(gpui::CursorStyle::PointingHand).hover(|style| style.bg(theme.hover))
            .on_mouse_down(gpui::MouseButton::Left, on_down))
        .when(!enabled, |b| b.opacity(0.5))
        .child(icon(glyph, px(14.), theme.icon_colors(theme.text_secondary)))
}
