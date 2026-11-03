//! The inline AI card, styled after thinkly's `inlineAiSurface.css`: 16px radius,
//! 18px padding, 40px controls, transparent tag buttons and a quiet shadow.
use gpui::{AnyElement, BoxShadow, CursorStyle, Entity, FontWeight, Hsla, MouseButton, SharedString, div, hsla, point, prelude::*, px, rgb, transparent_black};
use gpui_component::input::Input;

use super::provider::SETTINGS_HINT;
use super::super::view;
use super::session::{AiMenu, AiSession, AiStatus, Preset};
use crate::editor::view::Editor;
use crate::icons::{Icon, IconColors, icon};
use crate::theme::Theme;

type Run = fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>);

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

fn preset_tag(p: Preset, editor: &Entity<Editor>) -> gpui::Stateful<gpui::Div> {
    tag(SharedString::from(format!("ai-preset-{p:?}")), p.label(), false, false, editor, move |this, w, cx| this.ai_preset(p, w, cx))
}

fn open_settings(this: &mut Editor, _: &mut gpui::Window, cx: &mut gpui::Context<Editor>) {
    let Some(home) = std::env::var_os("HOME") else { return };
    let path = std::path::PathBuf::from(home).join("Library/Application Support/thinkly-fast/ai.json");
    if !path.exists() {
        let _ = std::fs::create_dir_all(path.parent().unwrap_or(&path));
        let _ = std::fs::write(&path, format!("{{\n  \"base_url\": \"{}\",\n  \"api_key\": \"\",\n  \"model\": \"\"\n}}\n", super::provider::DEFAULT_BASE_URL));
    }
    cx.open_with_system(&path);
    this.status = format!("已打开 AI 设置：{}", path.display());
    cx.notify();
}

pub(in crate::editor) fn render(editor: Entity<Editor>, s: &AiSession) -> AnyElement {
    let theme = Theme::ocean();
    let writing = s.original.is_empty();
    let title = match &s.status {
        AiStatus::Input if writing => "AI 写作",
        AiStatus::Input => "编辑所选内容",
        AiStatus::Thinking { .. } => "正在改写",
        AiStatus::Review { .. } => "审阅修改",
        AiStatus::Error { .. } => "改写未完成",
    };
    let busy = matches!(s.status, AiStatus::Thinking { .. });

    let header = div().flex().items_center().gap(px(8.))
        .child(glyph(Icon::Sparkles, 18., theme.accent))
        .child(div().flex_1().text_size(px(16.)).font_weight(FontWeight(600.)).child(title))
        .child(press(div().id("ai-close").size(px(28.)).flex().items_center().justify_center().rounded(px(6.))
            .cursor(CursorStyle::PointingHand).hover(move |st| st.bg(theme.hover))
            .tooltip(|w, cx| gpui_component::tooltip::Tooltip::new("关闭 AI 编辑").build(w, cx))
            .child(glyph(Icon::Close, 16., theme.text_secondary)), &editor, |this, w, cx| this.ai_close(w, cx)));

    let preview_box = |id: &'static str| div().id(id).max_h(px(240.)).overflow_y_scroll().p(px(12.)).rounded(px(10.))
        .bg(theme.paper).border_1().border_color(theme.hairline).text_size(px(15.)).line_height(px(24.)).flex().flex_col().gap(px(8.));

    let body: Option<AnyElement> = match &s.status {
        AiStatus::Input if !writing => Some(div().text_size(px(13.)).text_color(theme.text_secondary).line_clamp(2)
            .child(SharedString::from(format!("所选内容：{}", s.original.replace('\n', " ")))).into_any_element()),
        AiStatus::Thinking { partial, .. } if !partial.is_empty() => Some(preview_box("ai-stream")
            .child(div().text_color(theme.text).whitespace_normal().child(SharedString::from(partial.clone()))).into_any_element()),
        AiStatus::Review { draft } => {
            let deleted = s.original.chars().count();
            let added = draft.chars().count();
            Some(div().flex().flex_col().gap(px(8.))
                .child(div().flex().gap(px(10.)).text_size(px(13.))
                    .when(!writing, |d| d.child(div().text_color(rgb(0xb42318)).child(SharedString::from(format!("删除 {deleted} 字")))))
                    .child(div().text_color(rgb(0x086b42)).child(SharedString::from(format!("新增 {added} 字")))))
                .child(preview_box("ai-review")
                    .when(!writing, |d| d.child(div().text_color(theme.text_secondary).line_through().bg(hsla(0., 0.7, 0.5, 0.06)).rounded(px(4.)).px(px(4.))
                        .child(SharedString::from(s.original.clone()))))
                    .child(div().text_color(theme.text).bg(hsla(150. / 360., 0.6, 0.4, 0.08)).rounded(px(4.)).px(px(4.))
                        .child(SharedString::from(draft.clone()))))
                .into_any_element())
        }
        AiStatus::Error { message, settings } => Some(div().flex().flex_col().gap(px(6.)).text_size(px(13.))
            .child(div().text_color(rgb(0xb42318)).font_weight(FontWeight(500.)).child("生成中断，未对正文进行修改"))
            .child(div().text_color(theme.text_secondary).child(SharedString::from(message.clone())))
            .when(*settings, |d| d.child(div().text_color(theme.text_secondary).child(SETTINGS_HINT)))
            .into_any_element()),
        _ => None,
    };

    // Prompt row: the IME-capable input, or the generating ticker while busy.
    let field = div().h(px(40.)).px(px(12.)).flex().items_center().gap(px(8.)).rounded(px(10.))
        .border_1().border_color(if busy { theme.hairline } else { theme.btn_border }).bg(theme.paper);
    let prompt_row = if let AiStatus::Thinking { tick, reasoning, .. } = &s.status {
        let dots = ".".repeat(tick / 8 % 3 + 1);
        let label = if *reasoning { "正在思考并生成" } else { "正在生成" };
        field.child(div().size(px(10.)).rounded_full().bg(theme.accent).opacity(0.35 + 0.65 * ((*tick % 16) as f32 / 15.)))
            .child(div().flex_1().text_size(px(15.)).text_color(theme.text_secondary).child(SharedString::from(format!("{label}{dots}"))))
            .child(press(div().id("ai-stop").h(px(28.)).px(px(10.)).flex().items_center().gap(px(4.)).rounded(px(6.))
                .text_size(px(13.)).cursor(CursorStyle::PointingHand).hover(move |st| st.bg(theme.hover))
                .child(glyph(Icon::Stop, 14., theme.text)).child("停止"), &editor, |this, w, cx| this.ai_stop(w, cx)))
    } else {
        let empty = false;
        field.child(div().flex_1().min_w_0().h(px(28.)).text_size(px(15.)).child(Input::new(&s.input).appearance(false).bordered(false).focus_bordered(false).h(px(28.))))
            .child(press(div().id("ai-send").size(px(28.)).flex_none().flex().items_center().justify_center().rounded(px(8.))
                .bg(if empty { theme.hover } else { theme.primary }).cursor(CursorStyle::PointingHand)
                .tooltip(|w, cx| gpui_component::tooltip::Tooltip::new("发送").build(w, cx))
                .child(glyph(Icon::ArrowUp, 16., if empty { theme.text_secondary } else { gpui::white() })), &editor, |this, w, cx| this.ai_submit_input(w, cx)))
    };

    let mut footer = div().flex().flex_wrap().items_center().gap(px(6.));
    let mut submenu: Option<gpui::Div> = None;
    match &s.status {
        AiStatus::Input if !writing => {
            footer = footer.child(preset_tag(Preset::Polish, &editor)).child(preset_tag(Preset::Simplify, &editor))
                .child(menu_tag("ai-translate", "翻译", AiMenu::Translate, s.menu, &editor))
                .child(menu_tag("ai-more", "更多", AiMenu::More, s.menu, &editor));
            let items: &[Preset] = match s.menu {
                AiMenu::Translate => &[Preset::English, Preset::Chinese],
                AiMenu::More => &[Preset::Jargon, Preset::Colloquial, Preset::List, Preset::Tasks],
                AiMenu::None => &[],
            };
            if !items.is_empty() {
                submenu = Some(items.iter().fold(div().flex().flex_wrap().gap(px(6.)).p(px(6.)).rounded(px(10.)).bg(theme.hover),
                    |d, p| d.child(preset_tag(*p, &editor))));
            }
        }
        AiStatus::Input => {}
        AiStatus::Thinking { .. } => {}
        AiStatus::Review { .. } => {
            footer = footer.child(tag("ai-accept", "应用修改", true, false, &editor, |this, w, cx| this.ai_accept(w, cx)))
                .child(tag("ai-discard", "放弃", false, false, &editor, |this, w, cx| this.ai_close(w, cx)))
                .child(tag("ai-retry", "重新生成", false, false, &editor, |this, w, cx| this.ai_retry(w, cx)));
        }
        AiStatus::Error { settings, .. } => {
            footer = footer.child(tag("ai-retry", "重试", true, false, &editor, |this, w, cx| this.ai_retry(w, cx)))
                .when(*settings, |f| f.child(tag("ai-settings", "打开设置", false, false, &editor, open_settings as Run)))
                .child(tag("ai-discard", "放弃", false, false, &editor, |this, w, cx| this.ai_close(w, cx)));
        }
    }
    let show_footer = !matches!(s.status, AiStatus::Thinking { .. } | AiStatus::Input if writing)
        && !matches!(s.status, AiStatus::Thinking { .. });

    let key_editor = editor.clone();
    gpui::deferred(gpui::anchored().position(s.anchor).snap_to_window_with_margin(px(12.)).child(
        div().id("inline-ai").w(px(460.)).p(px(18.)).flex().flex_col().gap(px(14.)).rounded(px(16.))
            .border_1().border_color(theme.menu_border).bg(theme.menu).text_color(theme.text).text_size(px(16.))
            .shadow(vec![BoxShadow { color: hsla(0., 0., 0., 0.12), offset: point(px(0.), px(12.)), blur_radius: px(32.), spread_radius: px(0.) }])
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
