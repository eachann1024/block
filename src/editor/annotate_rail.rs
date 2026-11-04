//! Wide-window right rail: single-line micro strips with an inline follow-up input.
use gpui::{Context, CursorStyle, Entity, IntoElement, MouseButton, ParentElement, SharedString, Styled, div, prelude::*, px, rgb};
use gpui_component::input::{Escape, Input, InputState};
use crate::{icons::{Icon, IconColors, icon}, theme::Theme, ui::annotation_note};
use super::{annotate::{Annotation, ANNOTATION_GENERATED, ANNOTATION_PENDING}, view::Editor};

pub(super) const RAIL_W: f32 = 224.;

fn followup_field(input: Entity<InputState>, cx: &mut Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean();
    div().mt(px(2.)).h(px(30.)).px(px(8.)).rounded(px(5.)).bg(theme.shell).border_1().border_color(theme.btn_border)
        .flex().items_center()
        .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| cx.stop_propagation()))
        .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.)))
}

pub(super) fn annotation_row(note: Annotation, active_id: Option<u64>, followup: Option<Entity<InputState>>, cx: &mut Context<Editor>) -> impl IntoElement {
    let id = note.id;
    let (is_applied, has_rewrite, is_active) = (note.is_applied(), note.has_rewrite(), active_id == Some(id));
    let toggle = move |this: &mut Editor, window: &mut gpui::Window, cx: &mut Context<Editor>| this.toggle_annotation_detail(id, window, cx);
    if note.body.is_empty() {
        let pills = note.reactions.iter().enumerate().map(|(idx, emoji)| {
            annotation_note::reaction_pill(SharedString::from(format!("reaction-pill-{id}-{idx}")), emoji, is_active)
        });
        return div().w_full().child(div().id(SharedString::from(format!("annotation-row-{id}"))).w_full().flex().flex_wrap().items_center().gap(px(6.))
            .children(pills).on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| toggle(this, window, cx))));
    }
    let status = if has_rewrite { ANNOTATION_GENERATED } else { ANNOTATION_PENDING };
    let apply = has_rewrite.then(|| {
        let button = annotation_note::glyph_action(SharedString::from(format!("annotation-apply-{id}")), "✓", true);
        if is_applied { button.cursor(CursorStyle::Arrow) } else {
            button.on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| { cx.stop_propagation(); this.apply_annotation(id, window, cx); }))
        }
    });
    let restore = has_rewrite.then(|| annotation_note::glyph_action(SharedString::from(format!("annotation-restore-{id}")), "↺", false)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| { cx.stop_propagation(); this.restore_annotation(id, window, cx); })));
    let strip = annotation_note::strip(SharedString::from(format!("annotation-row-{id}")), is_active)
        .child(annotation_note::status_dot(status)).child(annotation_note::strip_text(note.body.clone()))
        .children(apply).children(restore)
        .when(!is_applied, |row| row.cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| toggle(this, window, cx))));
    let input = (is_active && !is_applied).then_some(followup).flatten().map(|input| followup_field(input, cx));
    let error = is_active.then(|| note.ai_error.clone()).flatten().map(|message| {
        div().px(px(8.)).mt(px(2.)).text_size(px(14.)).line_height(px(20.)).text_color(rgb(0xb42318)).child(message)
    });
    div().w_full().child(strip).children(input).children(error)
}

pub(super) fn composer_row(input: Entity<InputState>, has_text: bool, cx: &mut Context<Editor>) -> impl IntoElement {
    let theme = Theme::ocean();
    let field = div().flex_1().min_w_0().h(px(28.)).flex().items_center().text_size(px(14.)).text_color(theme.text)
        .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.)));
    let color = rgb(0xffffff).into();
    let submit = div().id("annotate-submit").size(px(26.)).flex_none().rounded(px(13.)).flex().items_center().justify_center()
        .bg(if has_text { theme.accent } else { theme.btn_border })
        .when(has_text, |button| button.cursor(CursorStyle::PointingHand))
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.submit_annotation(window, cx)))
        .child(icon(Icon::ArrowUp, px(14.), IconColors { line: color, fill: gpui::transparent_black(), accent: color }));
    annotation_note::rail_note("annotate-composer", ANNOTATION_PENDING,
        div().w_full().h(px(30.)).flex().items_center().gap(px(6.)).child(field).child(submit))
        .key_context("AnnotateComposer")
        .on_action(cx.listener(|this, _: &Escape, window, cx| { this.close_annotation_composer(window, cx); cx.stop_propagation(); }))
}
