use gpui::{FontWeight, div, prelude::*, px};
use super::chat::AiChat;
use crate::{icons::{Icon, icon}, theme::{Theme, tr}, ui::ai_controls};

pub(super) fn view(_chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    let theme = Theme::ocean();
    let mut actions = div().flex().flex_wrap().justify_center().gap(px(8.)).max_w(px(540.));
    for (id, label, prompt) in [
        ("ai-empty-polish", tr("润色并精简当前笔记", "Polish and condense note"), "请帮我润色并精简当前笔记的结构与表述"),
        ("ai-empty-table", tr("整理内容为对齐表格", "Format content as table"), "请将当前笔记中的结构化条目整理为对齐的 Markdown 表格"),
        ("ai-empty-summary", tr("提炼核心要点与待办", "Extract key points and todos"), "请提炼当前笔记的核心要点，并整理出可执行的待办事项"),
        ("ai-empty-proofread", tr("检查错别字与语病", "Check typos and phrasing"), "请检查当前笔记中的错别字、标点与语病并给出修改建议"),
    ] {
        actions = actions.child(ai_controls::action_chip(id, None, label, false, false, cx.listener(move |chat, _, window, cx| {
            cx.stop_propagation(); chat.input.update(cx, |input, cx| { input.set_value(prompt, window, cx); input.focus(window, cx); }); cx.notify();
        })).border_1().border_color(theme.btn_border));
    }
    div().id("ai-empty-state").py(px(36.)).px(px(16.)).flex().flex_col().items_center().gap(px(14.))
        .child(div().size(px(48.)).rounded(px(14.)).bg(theme.shell).border_1().border_color(theme.hairline).flex().items_center().justify_center()
            .child(icon(Icon::Sparkles, px(24.), theme.icon_colors(theme.accent))))
        .child(div().text_size(px(16.)).font_weight(FontWeight(560.)).text_color(theme.text).child(tr("有什么可以帮你处理？", "How can I help with this note?")))
        .child(actions)
}
