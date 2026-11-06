//! Shared read-only message text with native drag selection and copy.
use gpui::{SharedString, Styled, px, rems};
use gpui_component::text::{SelectionFormat, TextView, TextViewStyle};
use crate::theme::Theme;

pub fn message(id: String, source: &str, markdown: bool) -> TextView {
    let id = SharedString::from(id);
    let view = if markdown {
        TextView::markdown(id, source.to_owned())
    } else {
        // HTML escaping preserves literal Markdown in messages written by the user.
        let text = source.replace('&', "&amp;").replace('<', "&lt;")
            .replace('>', "&gt;").replace('\n', "<br>");
        TextView::html(id, format!("<p>{text}</p>"))
    };
    view.selectable(true).selection_format(SelectionFormat::Plain)
        .scrollable(false).stream_fade(false)
        .style(TextViewStyle::default().paragraph_gap(rems(0.375))
            .heading_font_size(|level, _| px(match level { 1 => 18., 2 => 16., _ => 15. })))
        .text_size(px(14.)).line_height(px(22.)).text_color(Theme::ocean().text)
        .on_link_click(|url, _, _, _| crate::editor::open_external_url(url, None))
}
