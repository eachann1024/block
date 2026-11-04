//! Link field and colour palette popovers for the selection toolbar.
use gpui::{AnyElement, CursorStyle, Entity, MouseButton, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use super::toolbar::{ToolbarColors, menu_shadows};
use super::{format_cmds::{FormatPop, palette::PalettePopover}, view::{Cancel, Editor}};

pub(super) fn popover(editor: Entity<Editor>, pop: &FormatPop) -> AnyElement {
    let tc = ToolbarColors::current();
    let (anchor, body, pad) = match pop {
        FormatPop::Ai(session) => return super::ai::panel::render(editor, session),
        FormatPop::Link { input, existing, anchor, .. } => (*anchor, link_field(editor, input, *existing), 4.),
        FormatPop::Palette { anchor } => (*anchor, PalettePopover { editor }.into_any_element(), 6.),
        FormatPop::Kinds { anchor, current } => (*anchor, super::toolbar::menu(editor, true, *current), 6.),
        FormatPop::Lists { anchor, current } => (*anchor, super::toolbar::menu(editor, false, *current), 6.),
    };
    gpui::deferred(gpui::anchored().position(anchor).snap_to_window().child(
        div().rounded(px(12.)).border_1().border_color(rgb(tc.border)).bg(rgb(tc.bg)).shadow(menu_shadows()).p(px(pad))
            .text_size(px(12.)).text_color(rgb(tc.ink)).on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
            .child(body))).into_any_element()
}

fn action_button(label: &'static str, id: &'static str, run: impl Fn(&mut Editor, &mut gpui::Window, &mut gpui::Context<Editor>) + 'static, editor: Entity<Editor>, accent: bool) -> impl IntoElement {
    let tc = ToolbarColors::current();
    div().id(id).h(px(28.)).px(px(10.)).rounded(px(7.)).flex().items_center().cursor(CursorStyle::PointingHand).text_size(px(12.))
        .font_weight(gpui::FontWeight(500.))
        .when(accent, |b| b.bg(rgb(tc.accent)).text_color(rgb(0xffffff)))
        .when(!accent, |b| b.hover(move |s| s.bg(rgb(tc.hover))).text_color(rgb(tc.ink))).child(label)
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            editor.update(cx, |this, cx| run(this, window, cx)); cx.stop_propagation();
        })
}

fn link_field(editor: Entity<Editor>, input: &Entity<InputState>, _existing: bool) -> AnyElement {
    let tc = ToolbarColors::current();
    div().id("link-field").flex().items_center().gap(px(6.)).pl(px(6.))
        .on_action({ let e = editor.clone(); move |_: &Escape, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .on_action({ let e = editor.clone(); move |_: &Cancel, w, cx| { e.update(cx, |this, cx| this.close_format(w, cx)); cx.stop_propagation(); } })
        .child(div().w(px(260.)).h(px(28.)).px(px(8.)).rounded(px(6.)).bg(rgb(tc.input_bg)).text_size(px(13.)).text_color(rgb(tc.ink)).flex().items_center().overflow_hidden()
            .child(div().flex_1().min_w_0().child(Input::new(input).appearance(false).bordered(false).focus_bordered(false).text_size(px(13.)))))
        .child(action_button(crate::theme::tr("确定", "OK"), "link-save", |this, w, cx| this.link_save(w, cx), editor, true))
        .into_any_element()
}
