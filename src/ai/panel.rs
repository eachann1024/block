//! The inline AI card, styled after thinkly's `inlineAiSurface.css`: 16px radius,
//! 18px padding, 40px controls, transparent tag buttons and a quiet shadow.
use gpui::{AnyElement, BoxShadow, CursorStyle, Entity, FontWeight, Hsla, MouseButton, SharedString, div, hsla, point, prelude::*, px, rgb, transparent_black};
use gpui_component::input::Input;

use super::super::view;
use super::session::{AiMenu, AiSession, AiStatus, Preset, ticker_line};
use crate::editor::view::Editor;
use crate::icons::{Icon, IconColors, icon};
use crate::theme::{Theme, tr};


fn glyph(i: Icon, size: f32, color: Hsla) -> impl IntoElement {
    icon(i, px(size), IconColors { line: color, fill: transparent_black(), accent: color })
}

fn press(el: gpui::Stateful<gpui::Div>, editor: &Entity<Editor>, run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static) -> gpui::Stateful<gpui::Div> {
    let editor = editor.clone();
    el.on_mouse_down(MouseButton::Left, move |_, window, cx| {
        editor.update(cx, |this, cx| run(this, window, cx)); cx.stop_propagation();
    })
}

/// Transparent tag button (thinkly `.inline-ai-tag`); `primary` is the filled 应用修改.
fn tag(id: impl Into<SharedString>, label: impl Into<SharedString>, primary: bool, open: bool, editor: &Entity<Editor>,
    run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let el = div().id(id.into()).h(px(32.)).px(px(12.)).flex_none().flex().items_center().gap(px(4.)).rounded(px(8.))
        .text_size(px(14.4)).cursor(CursorStyle::PointingHand)
        .when(primary, |d| d.bg(theme.primary).text_color(gpui::white()).font_weight(FontWeight(500.)).hover(move |s| s.bg(theme.primary_hover)))
        .when(!primary, |d| d.text_color(theme.text).border_1().border_color(theme.hairline).hover(move |s| s.bg(theme.hover)))
        .when(open, |d| d.bg(theme.hover))
        .child(label.into());
    press(el, editor, run)
}

fn menu_tag(id: &'static str, label: &'static str, which: AiMenu, current: AiMenu, editor: &Entity<Editor>) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean();
    tag(id, label, false, current == which, editor, move |this, _, cx| this.ai_toggle_menu(which, cx))
        .child(div().text_color(theme.text_secondary).text_size(px(11.)).child(if current == which { "▴" } else { "▾" }))
}

fn menu_item(p: Preset, editor: &Entity<Editor>) -> gpui::Stateful<gpui::Div> {
    press(
        crate::dropdown::row(SharedString::from(format!("ai-item-{p:?}")), None, p.label(), false),
        editor,
        move |this, w, cx| this.ai_preset(p, w, cx),
    )
}

fn preset_tag(p: Preset, editor: &Entity<Editor>) -> gpui::Stateful<gpui::Div> {
    tag(SharedString::from(format!("ai-preset-{p:?}")), p.label(), false, false, editor, move |this, w, cx| this.ai_preset(p, w, cx))
}

pub(in crate::editor) fn render(editor: Entity<Editor>, s: &AiSession) -> AnyElement {
    let theme = Theme::ocean();
    let title = match &s.status {
        AiStatus::Input => tr("编辑所选内容", "Edit Selection"),
        AiStatus::Thinking { .. } => tr("正在改写", "Rewriting"),
        AiStatus::Review { .. } => tr("审阅修改", "Review Changes"),
        AiStatus::Error { .. } => tr("改写未完成", "Rewrite Incomplete"),
    };
    let busy = matches!(s.status, AiStatus::Thinking { .. });
    let (del_fg, del_bg) = (rgb(0xb42318), hsla(0., 0.75, 0.5, 0.12));
    let (ins_fg, ins_bg) = (rgb(0x087443), hsla(150. / 360., 0.6, 0.4, 0.14));

    let header = div().flex().items_center().gap(px(8.))
        .child(glyph(Icon::Sparkles, 18., theme.accent))
        .child(div().flex_1().text_size(px(16.)).font_weight(FontWeight(600.)).child(title))
        .child(press(div().id("ai-close").size(px(28.)).flex().items_center().justify_center().rounded(px(6.))
            .cursor(CursorStyle::PointingHand).hover(move |st| st.bg(theme.hover))
            .tooltip(crate::theme::tooltip(tr("关闭", "Close")))
            .child(glyph(Icon::Close, 16., theme.text_secondary)), &editor, |this, w, cx| this.ai_close(w, cx)));

    // Review shows only the legend; the diff itself is drawn inline in the document.
    let body: Option<AnyElement> = match &s.status {
        AiStatus::Review { .. } => Some(div().flex().items_center().gap(px(8.)).text_size(px(13.))
            .child(div().px(px(4.)).rounded(px(3.)).bg(del_bg).text_color(del_fg).line_through().child(tr("删除", "Removed")))
            .child(div().px(px(4.)).rounded(px(3.)).bg(ins_bg).text_color(ins_fg).child(tr("新增", "Added")))
            .into_any_element()),
        AiStatus::Error { message } => Some(div().text_size(px(13.)).text_color(del_fg)
            .child(SharedString::from(if message.is_empty() { tr("生成中断，未对正文进行修改", "Generation interrupted; text unchanged").to_owned() } else { message.clone() }))
            .into_any_element()),
        _ => None,
    };

    // Prompt row: the IME-capable input, or while busy the ticker and 停止.
    let field = div().min_h(px(40.)).px(px(12.)).flex().items_center().gap(px(8.)).rounded(px(10.))
        .border_1().border_color(if busy { theme.hairline } else { theme.btn_border }).bg(theme.paper);
    let prompt_row = if let AiStatus::Thinking { tick, partial, reasoning } = &s.status {
        let line = if !reasoning.trim().is_empty() && partial.trim().is_empty() { ticker_line(reasoning) } else { ticker_line(partial) };
        let line = if !line.is_empty() { line } else if !reasoning.is_empty() { tr("正在思考并生成…", "Thinking and writing…").to_owned() } else { tr("思考中", "Thinking").to_owned() };
        field.child(div().size(px(8.)).flex_none().rounded_full().bg(theme.accent).opacity(0.35 + 0.65 * ((*tick % 16) as f32 / 15.)))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(15.)).text_color(theme.text_secondary).child(SharedString::from(line)))
            .child(press(div().id("ai-stop").h(px(28.)).px(px(10.)).flex_none().flex().items_center().gap(px(4.)).rounded(px(6.))
                .text_size(px(13.)).cursor(CursorStyle::PointingHand).hover(move |st| st.bg(theme.hover))
                .tooltip(crate::theme::tooltip(tr("停止", "Stop")))
                .child(glyph(Icon::Stop, 14., theme.text)).child(tr("停止", "Stop")), &editor, |this, w, cx| this.ai_stop(w, cx)))
    } else {
        field.child(div().flex_1().min_w_0().h(px(28.)).text_size(px(15.)).child(Input::new(&s.input).appearance(false).bordered(false).focus_bordered(false).h(px(28.))))
            .child(press(div().id("ai-send").h(px(28.)).px(px(10.)).flex_none().flex().items_center().gap(px(4.)).rounded(px(8.))
                .bg(theme.primary).text_color(gpui::white()).text_size(px(13.)).cursor(CursorStyle::PointingHand)
                .tooltip(crate::theme::tooltip(tr("发送（回车）", "Send (Enter)")))
                .child(tr("发送", "Send")).child(glyph(Icon::ArrowUp, 14., gpui::white())), &editor, |this, w, cx| this.ai_submit_input(w, cx)))
    };

    let mut footer = div().flex().flex_wrap().items_center().gap(px(6.));
    let mut submenu: Option<gpui::Stateful<gpui::Div>> = None;
    match &s.status {
        AiStatus::Input => {
            footer = footer.child(preset_tag(Preset::Polish, &editor)).child(preset_tag(Preset::Simplify, &editor))
                .child(menu_tag("ai-translate", tr("翻译", "Translate"), AiMenu::Translate, s.menu, &editor))
                .child(menu_tag("ai-more", tr("更多", "More"), AiMenu::More, s.menu, &editor))
                .child(div().flex_1())
                .child(press(div().id("ai-settings").size(px(32.)).flex().items_center().justify_center().rounded(px(8.))
                    .cursor(CursorStyle::PointingHand).hover(move |st| st.bg(theme.hover))
                    .tooltip(crate::theme::tooltip(tr("打开 AI 设置", "Open AI Settings")))
                    .child(glyph(Icon::Sliders, 16., theme.text_secondary)), &editor, |this, w, cx| this.ai_open_settings(w, cx)));
            let items: &[Preset] = match s.menu {
                AiMenu::Translate => &[Preset::English, Preset::Chinese],
                AiMenu::More => &[Preset::Jargon, Preset::Colloquial, Preset::List, Preset::Tasks],
                AiMenu::None => &[],
            };
            if !items.is_empty() {
                submenu = Some(items.iter().fold(crate::dropdown::shell("ai-submenu").w(px(180.)),
                    |d, p| d.child(menu_item(*p, &editor))));
            }
        }
        AiStatus::Thinking { .. } => {}
        AiStatus::Review { .. } => {
            footer = footer.child(tag("ai-accept", tr("应用修改", "Apply Changes"), true, false, &editor, |this, w, cx| this.ai_accept(w, cx)))
                .child(tag("ai-discard", tr("放弃", "Discard"), false, false, &editor, |this, w, cx| this.ai_close(w, cx)))
                .child(tag("ai-retry", tr("重新生成", "Regenerate"), false, false, &editor, |this, w, cx| this.ai_retry(w, cx)));
        }
        AiStatus::Error { .. } => {
            if !s.prompt.is_empty() {
                footer = footer.child(tag("ai-retry", tr("重试", "Retry"), false, false, &editor, |this, w, cx| this.ai_retry(w, cx)));
            }
        }
    }
    let show_footer = !busy;

    let key_editor = editor.clone();
    gpui::deferred(gpui::anchored().position(s.anchor).snap_to_window_with_margin(px(12.)).child(
        div().id("inline-ai").w(px(460.)).p(px(18.)).flex().flex_col().gap(px(14.)).rounded(px(16.))
            .border_1().border_color(theme.menu_border).bg(theme.menu).text_color(theme.text).text_size(px(16.))
            .shadow(vec![BoxShadow { color: hsla(0., 0., 0., 0.12), offset: point(px(0.), px(12.)), blur_radius: px(32.), spread_radius: px(0.), inset: false }])
            .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            // Keys the prompt field lets bubble must not reach the document underneath.
            .on_action(|_: &view::Backspace, _, _| {})
            .on_action(|_: &view::Delete, _, _| {})
            .on_action(|_: &view::Left, _, _| {})
            .on_action(|_: &view::Right, _, _| {})
            .on_action(|_: &view::SelectLeft, _, _| {})
            .on_action(|_: &view::SelectRight, _, _| {})
            .on_action(|_: &view::SelectAll, _, _| {})
            .on_action(|_: &view::Up, _, _| {})
            .on_action(|_: &view::Down, _, _| {})
            .on_action(|_: &view::SelectUp, _, _| {})
            .on_action(|_: &view::SelectDown, _, _| {})
            .on_action(|_: &view::Enter, _, _| {})
            .on_action(|_: &view::Copy, _, _| {})
            .on_action(|_: &view::Cut, _, _| {})
            .on_action(|_: &view::Paste, _, _| {})
            .on_action(|_: &view::Bold, _, _| {})
            .on_action(|_: &view::Italic, _, _| {})
            .on_action(|_: &view::Strike, _, _| {})
            .on_action(|_: &view::Code, _, _| {})
            .on_action(|_: &view::Undo, _, _| {})
            .on_action(|_: &view::Redo, _, _| {})
            .on_action(|_: &view::Cancel, _, _| {})
            .on_action(|_: &view::NextCell, _, _| {})
            .on_action(|_: &view::PreviousCell, _, _| {})
            .on_action(|_: &view::TableMenuKey, _, _| {})
            .on_action(|_: &view::SetParagraph, _, _| {})
            .on_action(|_: &view::SetH1, _, _| {})
            .on_action(|_: &view::SetH2, _, _| {})
            .on_action(|_: &view::SetH3, _, _| {})
            .on_action(|_: &view::MoveBlockUp, _, _| {})
            .on_action(|_: &view::MoveBlockDown, _, _| {})
            .on_action(|_: &view::Underline, _, _| {})
            .on_action(|_: &view::EditLink, _, _| {})
            .on_action(|_: &view::ExitBlock, _, _| {})
            .on_action(|_: &view::DocStart, _, _| {})
            .on_action(|_: &view::DocEnd, _, _| {})
            .on_action(|_: &view::Find, _, _| {})
            .on_action(|_: &view::ImageGrow, _, _| {})
            .on_action(|_: &view::ImageShrink, _, _| {})
            .on_key_down(move |e, window, cx| {
                if e.keystroke.key == "escape" { key_editor.update(cx, |this, cx| this.ai_close(window, cx)); cx.stop_propagation(); }
            })
            .child(header)
            .children(body)
            .child(prompt_row)
            .when(show_footer, |d| d.child(footer))
            .children(submenu)))
        .with_priority(2).into_any_element()
}
