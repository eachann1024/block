use gpui::{CursorStyle, FontWeight, MouseButton, div, prelude::*, px};
use super::chat::AiChat;
use crate::{icons::{Icon, icon}, theme::{Theme, tr}, ui::ai_controls};

pub(super) fn builtin_blocked(chat: &AiChat) -> bool {
    chat.selected_backend() == super::backend::BackendKind::Builtin
        && super::provider::validate_request(&super::provider::config()).is_err()
}

pub(super) fn enable_cover(cx: &mut gpui::Context<AiChat>) -> gpui::AnyElement {
    let theme = Theme::ocean();
    let cfg = super::provider::config();
    let title = if !cfg.enabled {
        tr("先启用 AI 助手", "Enable AI Assistant")
    } else {
        tr("先完成 AI 配置", "Configure AI Assistant")
    };
    let detail = if !cfg.enabled {
        tr("内置 AI 当前处于关闭状态，点击前往设置开启后即可提问、润色与整理笔记。", "Built-in AI is currently off. Click to open Settings and enable it for this workspace.")
    } else {
        tr("内置 AI 尚未完成服务地址、密钥或模型配置，点击前往设置完成连接。", "Built-in AI is not configured yet. Click to open Settings and finish setup.")
    };
    let cta = if !cfg.enabled { tr("前往启用 AI", "Enable in AI Settings") } else { tr("前往配置 AI", "Open AI Settings") };
    div().id("ai-empty-state").py(px(44.)).px(px(20.)).flex().flex_col().items_center()
        .child(div().id("ai-enable-cover").relative().children(crate::ui::smoke_overlay::probe("ai-enable-cover")).w_full().max_w(px(336.)).p(px(28.)).rounded(px(16.)).bg(theme.shell)
            .border_1().border_color(theme.btn_border).cursor(CursorStyle::PointingHand)
            .hover(move |s| s.border_color(theme.accent).bg(theme.hover))
            .on_mouse_down(MouseButton::Left, cx.listener(|chat, _, window, cx| {
                cx.stop_propagation();
                chat.fullscreen = false;
                chat.input.update(cx, |i, cx| i.focus(window, cx));
                window.dispatch_action(Box::new(super::OpenAiSettings), cx);
                cx.notify();
            }))
            .flex().flex_col().items_center().gap(px(14.))
            .child(div().size(px(56.)).rounded(px(16.)).bg(theme.paper).border_1().border_color(theme.hairline).flex().items_center().justify_center()
                .child(icon(Icon::Sparkles, px(28.), theme.icon_colors(theme.accent))))
            .child(div().text_size(px(18.)).font_weight(FontWeight(600.)).text_color(theme.text).child(title))
            .child(div().text_size(px(14.)).line_height(px(22.)).text_color(theme.text_secondary).child(detail))
            .child(ai_controls::action_chip("ai-cover-settings", None, cta, true, false, cx.listener(|chat, _, window, cx| {
                cx.stop_propagation();
                chat.fullscreen = false;
                chat.input.update(cx, |i, cx| i.focus(window, cx));
                window.dispatch_action(Box::new(super::OpenAiSettings), cx);
                cx.notify();
            })).h(px(36.)).px(px(18.))))
        .into_any_element()
}

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
