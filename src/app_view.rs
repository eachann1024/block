use gpui::{App, Context, IntoElement, ParentElement, Render, Window, div, prelude::*, px};
use crate::theme::Theme;
use super::Workspace;

impl Render for Workspace {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let theme = Theme::ocean();
        div().size_full().flex().bg(theme.shell).text_color(theme.text)
            .child(self.sidebar(cx))
            .child(div().flex_1().min_w_0().h_full().child(self.editor.clone()))
    }
}

impl Workspace {
    fn sidebar(&self, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        let title = self.folder.as_ref().map(|path| {
            path.file_name().unwrap_or(path.as_os_str()).to_string_lossy().into_owned()
        }).unwrap_or_else(|| "未打开文件夹".into());
        let available = !self.busy();
        div().w(theme.sidebar_width).flex_none().h_full().flex().flex_col()
            .bg(theme.sidebar).border_r_1().border_color(theme.hairline)
            .child(div().px_3().pt_8().pb_2().flex().flex_col().gap_1()
                .child(div().text_size(theme.ui_font).child(title))
                .child(div().flex().gap_1()
                    .child(action_button("选择文件夹", available, cx.listener(|this, _, window, cx| {
                        this.choose_folder(window, cx);
                    })))
                    .child(action_button("新建笔记", available && self.folder.is_some(), cx.listener(|this, _, _, cx| {
                        this.new_note(cx);
                    }))))
                .child(div().id("workspace-status").max_h(px(96.)).overflow_y_scroll()
                    .text_size(px(12.)).text_color(theme.text_secondary).child(self.notice.clone())))
            .child(div().flex_1().min_h_0().overflow_hidden().child(self.tree.clone()))
    }
}

fn action_button(label: &'static str, enabled: bool,
    on_down: impl Fn(&gpui::MouseDownEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    div().id(label).h(px(28.)).px_2().flex().items_center().rounded_lg()
        .bg(theme.paper).border_1().border_color(theme.hairline).text_size(px(13.))
        .when(enabled, |button| button.cursor(gpui::CursorStyle::PointingHand)
            .hover(|style| style.bg(theme.hover)).on_mouse_down(gpui::MouseButton::Left, on_down))
        .when(!enabled, |button| button.text_color(theme.text_secondary).opacity(0.5))
        .child(label)
}
