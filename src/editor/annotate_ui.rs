//! Cardless right-rail annotations, compact follow-up input, and reaction picker.
use std::{cell::RefCell, collections::HashMap};
use gpui::{
    AnyElement, BoxShadow, Context, CursorStyle, IntoElement, MouseButton, ParentElement, Pixels,
    SharedString, Styled, Window, div, point, prelude::*, px, rgb, rgba, transparent_black,
};
use gpui_component::input::{Escape, Input};
use crate::{icons::{Icon, IconColors, icon}, theme::Theme, ui::annotation_note};
use super::{
    annotate::{Annotation, ANNOTATION_GENERATED, ANNOTATION_PENDING, ReactionPicker, resolve_annotation_span},
    view::Editor,
};

const RAIL_W: f32 = 208.;
const REACTIONS: [&str; 8] = ["👍", "❤️", "🎉", "🤔", "👀", "🔥", "✅", "💡"];

fn popover_shadows() -> Vec<BoxShadow> {
    let shadow = |color, y, blur| BoxShadow { color, offset: point(px(0.), px(y)), blur_radius: px(blur), spread_radius: px(0.), inset: false };
    vec![shadow(rgba(0x0000001f).into(), 1., 2.), shadow(rgba(0x00000014).into(), 2., 8.)]
}

fn line_icon(glyph: Icon, size: f32, color: u32) -> impl IntoElement {
    let color = rgb(color).into();
    icon(glyph, px(size), IconColors { line: color, fill: transparent_black(), accent: color })
}

pub(super) fn overlay(editor: &mut Editor, _window: &mut Window, cx: &mut Context<Editor>) -> Option<AnyElement> {
    if editor.minimal || editor.path.is_none() { return None; }
    let has_rail = editor.annotate.has_visible_rail();
    let has_reactions = editor.annotate.reactions.is_some();
    if !has_rail && !has_reactions { return None; }
    let viewport = editor.list.viewport_bounds().size.width;
    let narrow = viewport > px(0.) && viewport < px(860.);

    let rail = has_rail.then(|| {
        if narrow {
            let mut layer = div().id("annotate-rail").absolute().top_0().left_0().size_full();
            let mut next_y = px(0.);
            let notes = editor.annotate.annotations.iter().filter(|note| !note.is_applied() || note.has_rewrite()).cloned().collect::<Vec<_>>();
            for (index, note) in notes.into_iter().enumerate() {
                let target = resolve_annotation_span(&editor.doc, &note).map(|(id, range, _)| (id, range.start));
                let y = target.map(|(id, offset)| row_target(editor, id, offset)).unwrap_or(px(24.)).max(next_y + px(28.));
                let id = note.id;
                let is_active = editor.annotate.active_annotation == Some(id);
                let status = if note.has_rewrite() { ANNOTATION_GENERATED } else { ANNOTATION_PENDING };
                if note.body.is_empty() && !note.reactions.is_empty() {
                    layer = layer.child(
                        annotation_note::reaction_pill(SharedString::from(format!("annotation-marker-{id}")), &note.reactions[0], is_active)
                            .absolute().top(y).right(px(12.)).occlude()
                            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                                cx.stop_propagation();
                                this.toggle_annotation_detail(id, window, cx);
                            })),
                    );
                } else {
                    layer = layer.child(div().id(SharedString::from(format!("annotation-marker-{id}")))
                        .absolute().top(y).right(px(12.)).min_w(px(24.)).h(px(24.)).px(px(6.))
                        .rounded(px(6.)).border_l_2().border_color(rgb(status)).bg(Theme::ocean().paper)
                        .flex().items_center().justify_center().text_size(px(14.)).text_color(Theme::ocean().text_secondary)
                        .cursor(CursorStyle::PointingHand).occlude()
                        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                            cx.stop_propagation();
                            this.toggle_annotation_detail(id, window, cx);
                        })).child(format!("{}", index + 1)));
                }
                next_y = y + if is_active && !note.body.is_empty() { px(92.) } else { px(28.) };
                if is_active && !note.body.is_empty() {
                    let followup = editor.annotate.followup.as_ref()
                        .filter(|field| field.annotation_id == id).map(|field| field.input.clone());
                    layer = layer.child(div().absolute().top(y + px(28.)).right(px(16.)).w(px(RAIL_W)).occlude()
                        .child(annotation_row(note, editor.annotate.active_annotation, followup, cx)));
                }
            }
            if let Some(composer) = &editor.annotate.composer {
                let input = composer.input.clone();
                let has_text = !input.read(cx).value().trim().is_empty();
                let y = row_target(editor, composer.block_id, composer.range.start);
                layer = layer.child(div().absolute().top(y).right(px(16.)).w(px(RAIL_W)).occlude()
                    .child(composer_row(input, has_text, cx)));
            }
            return layer;
        }
        let mut rows: Vec<(Pixels, u64, AnyElement)> = editor.annotate.annotations.iter()
            .filter(|note| !note.is_applied() || note.has_rewrite()).cloned().map(|note| {
                let target = resolve_annotation_span(&editor.doc, &note).map(|(id, range, _)| (id, range.start));
                let y = target.map(|(id, offset)| row_target(editor, id, offset)).unwrap_or(px(24.));
                let id = note.id;
                let followup = editor.annotate.followup.as_ref()
                    .filter(|field| field.annotation_id == id).map(|field| field.input.clone());
                (y, id, annotation_row(note, editor.annotate.active_annotation, followup, cx).into_any_element())
            }).collect();
        if let Some(composer) = &editor.annotate.composer {
            let input = composer.input.clone();
            let has_text = !input.read(cx).value().trim().is_empty();
            rows.push((row_target(editor, composer.block_id, composer.range.start), 0,
                composer_row(input, has_text, cx).into_any_element()));
        }
        rows.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
        let mut bottom = px(12.);
        let mut rail = div().id("annotate-rail").absolute().top_0().right(px(24.)).w(px(RAIL_W)).h_full();
        for (target, id, row) in rows {
            let top = target.max(bottom + px(12.));
            let height = ROW_HEIGHTS.with(|heights| heights.borrow().get(&id).copied()).unwrap_or(px(24.));
            bottom = top + height;
            let entity = cx.entity().downgrade();
            let measure = gpui::canvas(move |bounds, _, _| {
                ROW_HEIGHTS.with(|heights| heights.borrow_mut().insert(id, bounds.size.height) != Some(bounds.size.height))
            }, move |_, changed, _, cx| {
                if changed {
                    let entity = entity.clone();
                    cx.defer(move |cx| { entity.update(cx, |_, cx| cx.notify()).ok(); });
                }
            }).absolute().size_full();
            rail = rail.child(div().absolute().top(top).w_full().occlude().child(row).child(measure));
        }
        rail
    });

    let reactions = editor.annotate.reactions.clone().map(|picker| {
        let selected = picker.annotation_id
            .and_then(|id| editor.annotate.annotations.iter().find(|n| n.id == id))
            .map(|n| n.reactions.clone())
            .unwrap_or_default();
        reaction_popover(picker, selected, cx)
    });
    Some(div().absolute().top_0().left_0().size_full().children(rail).children(reactions).into_any_element())
}

thread_local! { static ROW_HEIGHTS: RefCell<HashMap<u64, Pixels>> = RefCell::new(HashMap::new()); }

fn row_target(editor: &Editor, block_id: u64, offset: usize) -> Pixels {
    editor.layouts.get(&block_id).map(|layout| layout.point(offset).y - editor.annotate.editor_top)
        .or_else(|| editor.doc.index_of(block_id).and_then(|index| editor.list.bounds_for_item(index)).map(|bounds| bounds.top() - editor.annotate.editor_top))
        .unwrap_or(px(24.)).max(px(24.))
}

fn annotation_row(
    note: Annotation,
    active_id: Option<u64>,
    followup: Option<gpui::Entity<gpui_component::input::InputState>>,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let id = note.id;
    let is_applied = note.is_applied();
    let has_rewrite = note.has_rewrite();
    let is_active = active_id == Some(id);
    if note.body.is_empty() {
        let pills = note.reactions.iter().enumerate().map(|(idx, emoji)| {
            annotation_note::reaction_pill(SharedString::from(format!("reaction-pill-{id}-{idx}")), emoji, is_active)
        });
        return div()
            .id(SharedString::from(format!("annotation-row-{id}")))
            .w_full()
            .flex()
            .flex_wrap()
            .items_center()
            .gap(px(6.))
            .children(pills)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                this.toggle_annotation_detail(id, window, cx);
            }));
    }
    let theme = Theme::ocean();
    let status = if has_rewrite { ANNOTATION_GENERATED } else { ANNOTATION_PENDING };
    let text = div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
        .text_size(px(14.)).line_height(px(20.)).text_color(theme.text_secondary).child(note.body.clone());
    let actions = if has_rewrite {
        let state = if is_applied {
            div().size(px(22.)).flex_none().flex().items_center().justify_center()
                .text_size(px(14.)).text_color(theme.accent).child("✓").into_any_element()
        } else {
            div().id(SharedString::from(format!("annotation-apply-{id}"))).size(px(22.)).flex_none()
                .rounded(px(4.)).flex().items_center().justify_center().text_size(px(14.)).text_color(theme.accent)
                .cursor(CursorStyle::PointingHand).hover(|style| style.bg(theme.hover))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                    cx.stop_propagation();
                    this.apply_annotation(id, window, cx);
                })).child("✓").into_any_element()
        };
        let restore = div().id(SharedString::from(format!("annotation-restore-{id}"))).size(px(22.)).flex_none()
            .rounded(px(4.)).flex().items_center().justify_center().text_size(px(14.)).text_color(theme.text_secondary)
            .cursor(CursorStyle::PointingHand).hover(|style| style.bg(theme.hover))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                cx.stop_propagation();
                this.restore_annotation(id, window, cx);
            })).child("↺");
        Some(div().flex_none().flex().items_center().gap(px(2.)).child(state).child(restore))
    } else { None };
    let head = div().w_full().h(px(22.)).flex().items_center().gap(px(4.)).child(text).children(actions);
    let candidate = note.ai_rewritten.clone().filter(|_| has_rewrite).map(|text| {
        div().mt(px(2.)).max_h(px(60.)).overflow_hidden().text_size(px(14.)).line_height(px(20.))
            .text_color(theme.text).child(text)
    });
    let pills = (!note.reactions.is_empty()).then(|| {
        div().mt(px(6.)).flex().flex_wrap().items_center().gap(px(6.)).children(
            note.reactions.iter().enumerate().map(|(idx, emoji)| {
                annotation_note::reaction_pill(SharedString::from(format!("reaction-pill-{id}-{idx}")), emoji, is_active)
            }),
        )
    });
    let error = is_active.then(|| note.ai_error.clone()).flatten().map(|message| {
        div().mt(px(4.)).text_size(px(14.)).line_height(px(20.)).text_color(rgb(0xb42318)).child(message)
    });
    let input = (is_active && !is_applied).then_some(followup).flatten().map(|input| {
        div().mt(px(6.)).h(px(30.)).px(px(8.)).rounded(px(5.)).bg(theme.shell)
            .border_1().border_color(theme.btn_border).flex().items_center()
            .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| cx.stop_propagation()))
            .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.)))
    });
    let body = div().w_full().min_w_0().child(head).children(candidate).children(pills).children(input).children(error);
    annotation_note::rail_note(SharedString::from(format!("annotation-row-{id}")), status, body)
        .when(!is_applied, |row| row.cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                this.toggle_annotation_detail(id, window, cx);
            })))
}

fn composer_row(
    input: gpui::Entity<gpui_component::input::InputState>,
    has_text: bool,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let field = div().flex_1().min_w_0().h(px(28.)).flex().items_center().text_size(px(14.)).text_color(theme.text)
        .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.)));
    let submit = div().id("annotate-submit").size(px(26.)).flex_none().rounded(px(13.))
        .flex().items_center().justify_center().bg(if has_text { theme.accent } else { theme.btn_border })
        .when(has_text, |button| button.cursor(CursorStyle::PointingHand))
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.submit_annotation(window, cx)))
        .child(line_icon(Icon::ArrowUp, 14., 0xffffff));
    annotation_note::rail_note("annotate-composer", ANNOTATION_PENDING,
        div().w_full().h(px(30.)).flex().items_center().gap(px(6.)).child(field).child(submit))
        .key_context("AnnotateComposer")
        .on_action(cx.listener(|this, _: &Escape, window, cx| {
            this.close_annotation_composer(window, cx);
            cx.stop_propagation();
        }))
}

fn reaction_popover(picker: ReactionPicker, selected_reactions: Vec<String>, cx: &mut Context<Editor>) -> impl IntoElement {
    let x = (picker.anchor.x - px(120.)).max(px(24.));
    let y = (picker.anchor.y + px(8.)).max(px(48.));
    let bar = REACTIONS.iter().enumerate().fold(
        div().id("annotate-reactions").absolute().left(x).top(y).px(px(8.)).py(px(6.))
            .rounded(px(10.)).bg(Theme::ocean().menu).shadow(popover_shadows())
            .flex().items_center().gap(px(4.)).occlude(),
        |row, (index, &emoji)| {
            let selected = selected_reactions.iter().any(|r| r == emoji);
            row.child(
                annotation_note::reaction_picker_item(SharedString::from(format!("pick-emoji-{index}")), emoji, selected)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        this.pick_reaction(emoji, cx);
                    })),
            )
        },
    );
    div().id("annotate-reactions-backdrop").absolute().top_0().left_0().size_full()
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
            this.annotate.reactions = None;
            cx.notify();
        }))
        .child(bar)
}
