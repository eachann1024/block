//! Link field and colour palette popovers for the selection toolbar.
use gpui::{AnyElement, CursorStyle, Entity, MouseButton, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use super::toolbar::{ACCENT, BG, BORDER, HOVER, INK, MUTED, SEP};
use super::{format_cmds::FormatPop, view::{Cancel, Editor}};

/// thinkly's palette: (stored name, label, text hex, background hex). The hex
/// values are the light-theme ones from thinkly's `text-colors.json`.
const PALETTE: [(&str, &str, u32, u32); 9] = [
    ("gray", "灰色", 0x5e5d59, 0xebeced), ("brown", "褐色", 0x64473a, 0xe9e5e3), ("red", "红色", 0xb42318, 0xfbe4e4),
    ("orange", "橙色", 0x974e08, 0xf6e9d9), ("yellow", "黄色", 0x855600, 0xfbf3db), ("green", "绿色", 0x086b42, 0xddedea),
    ("blue", "蓝色", 0x245bb2, 0xddebf1), ("purple", "紫色", 0x6940a5, 0xeae4f2), ("pink", "粉色", 0xad1a72, 0xf4dfeb),
];

pub(super) fn popover(editor: Entity<Editor>, pop: &FormatPop) -> AnyElement {
    let (anchor, body, pad) = match pop {
        FormatPop::Ai(session) => return super::ai::panel::render(editor, session),
        FormatPop::Link { input, existing, anchor, .. } => (*anchor, link_field(editor, input, *existing), 4.),
        FormatPop::Palette { anchor } => (*anchor, palette(editor), 10.),
        FormatPop::Kinds { anchor, current } => (*anchor, super::toolbar::menu(editor, true, *current), 4.),
        FormatPop::Lists { anchor, current } => (*anchor, super::toolbar::menu(editor, false, *current), 4.),
    };
    gpui::deferred(gpui::anchored().position(anchor).snap_to_window().child(
        div().rounded(px(12.)).border_1().border_color(rgb(BORDER)).bg(rgb(BG)).shadow_lg().p(px(pad))
            .text_size(px(12.)).text_color(rgb(INK)).on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .child(body))).into_any_element()
}

fn action_button(label: &'static str, id: &'static str, run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static, editor: Entity<Editor>, accent: bool) -> impl IntoElement {
    div().id(id).h(px(28.)).px(px(10.)).rounded(px(7.)).flex().items_center().cursor(CursorStyle::PointingHand).text_size(px(12.))
        .font_weight(gpui::FontWeight(500.))
        .when(accent, |b| b.bg(rgb(ACCENT)).text_color(rgb(0xffffff)))
        .when(!accent, |b| b.hover(|s| s.bg(rgb(HOVER))).text_color(rgb(INK))).child(label)
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| run(this, window, cx)); cx.stop_propagation();
        })
}

fn link_field(editor: Entity<Editor>, input: &Entity<InputState>, _existing: bool) -> AnyElement {
    div().id("link-field").flex().items_center().gap(px(6.)).pl(px(6.))
        .on_action({ let e = editor.clone(); move |_: &Escape, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .on_action({ let e = editor.clone(); move |_: &Cancel, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .child(div().w(px(260.)).h(px(28.)).px(px(8.)).rounded(px(6.)).bg(rgb(0x33363c)).text_size(px(13.)).text_color(rgb(INK)).flex().items_center().overflow_hidden()
            .child(div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false).focus_bordered(false).text_size(px(13.)))))
        .child(action_button("确定", "link-save", |this, w, cx| this.link_save(w, cx), editor, true))
        .into_any_element()
}

fn swatch(editor: &Entity<Editor>, id: (&'static str, usize), prop: &'static str, name: Option<&'static str>, _label: &'static str, hex: Option<u32>, background: bool) -> impl IntoElement {
    let editor = editor.clone();
    let ink: gpui::Hsla = hex.filter(|_| !background).map_or(rgb(INK).into(), |h| rgb(h).into());
    div().id(id).size(px(22.)).rounded_md().border_1().border_color(rgb(SEP)).flex().items_center().justify_center()
        .cursor(CursorStyle::PointingHand).text_color(ink)
        .when_some(hex.filter(|_| background), |s, h| s.bg(rgb(h)))
        // Text colours sit on a light chip so dark inks stay readable on the dark popover.
        .when(!background && hex.is_some(), |s| s.bg(rgb(0xf3f4f6)))
        .hover(|s| s.border_color(rgb(ACCENT)))
        .child(if background && hex.is_none() { "∅" } else { "A" })
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| { this.apply_style(prop, name, cx); this.close_format(window, cx); });
            cx.stop_propagation();
        })
}

fn palette(editor: Entity<Editor>) -> AnyElement {
    let row = |title: &'static str, prop: &'static str, background: bool, base: usize| {
        let mut swatches = div().flex().gap(px(2.))
            .child(swatch(&editor, ("swatch", base), prop, None, if background { "无背景" } else { "默认" }, None, background));
        for (i, (name, label, text, bg)) in PALETTE.iter().enumerate() {
            swatches = swatches.child(swatch(&editor, ("swatch", base + i + 1), prop, Some(name), label, Some(if background { *bg } else { *text }), background));
        }
        div().flex().flex_col().gap_1().child(div().text_color(rgb(MUTED)).child(title)).child(swatches)
    };
    div().flex().flex_col().gap_2()
        .child(row("文字颜色", "color", false, 0))
        .child(row("背景颜色", "background-color", true, 20)).into_any_element()
}
