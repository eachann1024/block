use gpui::{FontWeight, div, prelude::*, px};
use super::{backend_ui, chat::AiChat};
use crate::{icons::{Icon, icon}, theme::{Theme, is_en, tr}, ui::ai_controls};

pub(super) fn view(chat: &AiChat, cx: &mut gpui::Context<AiChat>) -> impl IntoElement {
    let theme = Theme::ocean();
    let note = chat.agent_ctx(cx).current;
    let backend = backend_ui::backend_name(chat.selected_backend());
    let model = chat.model_label(&chat.selected_model());
    let status = if is_en() { format!("Using {backend} ({model})") } else { format!("当前使用 {backend} ({model})") };
    let subtitle = note.map(|note| if is_en() { format!("Linked @{note} · {status}") } else { format!("已关联 @{note} · {status}") }).unwrap_or(status);
    let mut actions = div().flex().flex_wrap().justify_center().gap(px(8.)).max_w(px(540.));
    for (id, label, prompt) in [
        ("ai-empty-polish", tr("润色并精简当前笔记", "Polish and condense note"), "请帮我润色并精简当前笔记的结构与表述"),
        ("ai-empty-table", tr("整理内容为对齐表格", "Format content as table"), "请将当前笔记中的结构化条目整理为对齐的 Markdown 表格"),
    ] {
        actions = actions.child(ai_controls::action_chip(id, None, label, false, false, cx.listener(move |chat, _, window, cx| {
            cx.stop_propagation(); chat.input.update(cx, |input, cx| { input.set_value(prompt, window, cx); input.focus(window, cx); }); cx.notify();
        })).border_1().border_color(theme.btn_border));
    }
    actions = actions.child(ai_controls::action_chip("ai-empty-deck", None, tr("切换 Codex / Claude 后端", "Switch Codex / Claude backend"), false, false,
        cx.listener(|chat, _, window, cx| { cx.stop_propagation(); backend_ui::open_deck(chat, window, cx); })).border_1().border_color(theme.btn_border))
        .child(ai_controls::action_chip("ai-empty-dsh", None, tr("配置 dsh 认证来源", "Configure dsh auth source"), false, false,
            cx.listener(|chat, _, window, cx| { cx.stop_propagation(); backend_ui::open_dsh_in_deck(chat, window, cx); })).border_1().border_color(theme.btn_border));
    div().id("ai-empty-state").py(px(36.)).px(px(16.)).flex().flex_col().items_center().gap(px(14.))
        .child(div().size(px(48.)).rounded(px(14.)).bg(theme.shell).border_1().border_color(theme.hairline).flex().items_center().justify_center()
            .child(icon(Icon::Sparkles, px(24.), theme.icon_colors(theme.accent))))
        .child(div().text_size(px(16.)).font_weight(FontWeight(560.)).text_color(theme.text).child(tr("有什么可以帮你处理？", "How can I help with this note?")))
        .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(subtitle))
        .child(actions)
}
