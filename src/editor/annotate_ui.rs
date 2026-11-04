//! Right-rail comment cards, `@Claude` composer card, and reaction picker.
use std::{cell::RefCell, collections::HashMap};
use gpui::{
    AnyElement, Context, CursorStyle, FontWeight, IntoElement, MouseButton, ParentElement,
    Pixels, SharedString, Styled, Window, div, prelude::*, px, rgb, transparent_black,
};
use gpui_component::input::{Escape, Input};
use crate::{icons::{Icon, IconColors, icon}, theme::tr};
use super::{
    annotate::{Comment, HL_AI_BG, HL_AI_BORDER, ReactionPicker, resolve_comment_span},
    view::{Editor, selection::Position},
};

const REACTIONS: [&str; 8] = ["👍", "❤️", "🎉", "🤔", "👀", "🔥", "✅", "💡"];

fn line_icon(glyph: Icon, size: f32, color: u32) -> impl IntoElement {
    let c = rgb(color).into();
    icon(glyph, px(size), IconColors { line: c, fill: transparent_black(), accent: c })
}

pub(super) fn overlay(editor: &mut Editor, _window: &mut Window, cx: &mut Context<Editor>) -> Option<AnyElement> {
    if editor.minimal || editor.path.is_none() { return None; }
    let has_rail = editor.annotate.has_visible_rail();
    let has_reactions = editor.annotate.reactions.is_some();
    if !has_rail && !has_reactions { return None; }

    let rail = has_rail.then(|| {
        let mut cards: Vec<(Pixels, u64, AnyElement)> = editor.annotate.comments.iter().cloned().map(|comment| {
            let target = resolve_comment_span(&editor.doc, &comment).map(|(id, range, _)| (id, range.start));
            let y = target.map(|(id, offset)| card_target(editor, id, offset)).unwrap_or(px(24.));
            let id = comment.id;
            (y, id, comment_card(comment, editor.annotate.active_comment, cx).into_any_element())
        }).collect();
        if let Some(composer) = &editor.annotate.composer {
            let input = composer.input.clone();
            let has_text = !input.read(cx).value().trim().is_empty();
            cards.push((card_target(editor, composer.block_id, composer.range.start), 0,
                composer_card(input, composer.send_to_claude, has_text, cx).into_any_element()));
        }
        cards.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
        let mut bottom = px(12.);
        let mut rail = div().id("annotate-rail").absolute().top_0().right(px(24.)).w(px(280.)).h_full();
        for (target, id, card) in cards {
            let top = target.max(bottom + px(12.));
            let height = CARD_HEIGHTS.with(|heights| heights.borrow().get(&id).copied()).unwrap_or(px(100.));
            bottom = top + height;
            let entity = cx.entity().downgrade();
            let measure = gpui::canvas(move |bounds, _, _| {
                let changed = CARD_HEIGHTS.with(|heights| heights.borrow_mut().insert(id, bounds.size.height) != Some(bounds.size.height));
                changed
            }, move |_, changed, _, cx| {
                if changed { let entity = entity.clone(); cx.defer(move |cx| { entity.update(cx, |_, cx| cx.notify()).ok(); }); }
            }).absolute().size_full();
            rail = rail.child(div().absolute().top(top).w_full().occlude().child(card).child(measure));
        }
        rail
    });

    let reactions = editor.annotate.reactions.clone().map(|picker| reaction_popover(picker, cx));
    Some(div().absolute().top_0().left_0().size_full().children(rail).children(reactions).into_any_element())
}

thread_local! { static CARD_HEIGHTS: RefCell<HashMap<u64, Pixels>> = RefCell::new(HashMap::new()); }

fn card_target(editor: &Editor, block_id: u64, offset: usize) -> Pixels {
    editor.layouts.get(&block_id).map(|layout| layout.point(offset).y - editor.annotate.editor_top)
        .or_else(|| editor.doc.index_of(block_id).and_then(|ix| editor.list.bounds_for_item(ix)).map(|b| b.top() - editor.annotate.editor_top))
        .unwrap_or(px(24.)).max(px(24.))
}

fn comment_card(comment: Comment, active_id: Option<u64>, cx: &mut Context<Editor>) -> impl IntoElement {
    let cid = comment.id;
    let is_active = active_id == Some(cid);
    let border_col = if is_active { 0xd5d4ce } else { 0xececea };
    let head = div().flex().items_center().gap(px(8.))
        .child(div().size(px(24.)).rounded(px(12.)).bg(rgb(0x2f9e6b)).text_color(rgb(0xffffff))
            .text_size(px(12.)).font_weight(FontWeight(600.)).flex().items_center().justify_center()
            .child(comment.avatar.clone()))
        .child(div().text_size(px(13.)).font_weight(FontWeight(600.)).text_color(rgb(0x1f1e1d))
            .child(comment.author.clone()))
        .child(div().text_size(px(12.)).text_color(rgb(0x85847e)).child(comment.time.clone()))
        .child(div().id(SharedString::from(format!("comment-del-{cid}"))).ml_auto().size(px(22.)).rounded(px(6.))
            .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
            .hover(|s| s.bg(rgb(0xf3f2ee)))
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                cx.stop_propagation();
                this.delete_comment(cid, cx);
            }))
            .child(line_icon(Icon::Close, 12., 0x85847e)));

    let body = (!comment.body.is_empty()).then(|| {
        div().mt(px(6.)).ml(px(32.)).text_size(px(14.)).line_height(px(21.)).text_color(rgb(0x1f1e1d))
            .child(comment.body.clone())
    });

    let status = if comment.streaming {
        Some(div().mt(px(6.)).ml(px(32.)).text_size(px(12.)).text_color(rgb(0x2f6fe4)).child(tr("Claude 正在改写选中内容…", "Claude is rewriting the selection…")))
    } else if let Some(err) = &comment.ai_error {
        Some(div().mt(px(6.)).ml(px(32.)).text_size(px(12.)).text_color(rgb(0xb42318)).child(err.clone()))
    } else if comment.ai_rewritten.is_some() {
        Some(div().mt(px(6.)).ml(px(32.)).flex().items_center().gap(px(6.)).text_size(px(12.)).text_color(rgb(0x787774))
            .child(div().size(px(8.)).rounded(px(2.)).bg(rgb(HL_AI_BG)).border_1().border_color(rgb(HL_AI_BORDER)))
            .child(tr("Claude 已更新原文", "Claude updated the text")))
    } else {
        None
    };

    let reactions = (!comment.reactions.is_empty()).then(|| {
        comment.reactions.iter().cloned().enumerate().fold(
            div().mt(px(8.)).ml(px(32.)).flex().flex_wrap().gap(px(6.)),
            |row, (idx, emoji)| {
                let emoji_click = emoji.clone();
                row.child(div().id(SharedString::from(format!("react-{cid}-{idx}"))).px(px(8.)).py(px(2.))
                    .rounded(px(12.)).bg(rgb(0xf6f5f2)).border_1().border_color(rgb(0xececea))
                    .text_size(px(13.)).cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                        cx.stop_propagation();
                        if let Some(c) = this.annotate.comments.iter_mut().find(|c| c.id == cid) {
                            c.reactions.retain(|r| r != &emoji_click);
                            let empty = c.body.is_empty() && c.reactions.is_empty();
                            if empty { this.annotate.comments.retain(|x| x.id != cid); }
                            this.persist_annotations();
                            cx.notify();
                        }
                    }))
                    .child(format!("{emoji} 1")))
            },
        )
    });

    div().id(SharedString::from(format!("comment-card-{cid}"))).w_full().px(px(14.)).py(px(12.))
        .rounded(px(16.)).bg(rgb(0xffffff)).border_1().border_color(rgb(border_col)).shadow_md()
        .cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            this.annotate.active_comment = Some(cid);
            if let Some(c) = this.annotate.comments.iter().find(|c| c.id == cid).cloned() {
                if let Some((block_id, range, _)) = resolve_comment_span(&this.doc, &c) {
                    this.ensure_block_visible(block_id);
                    let pos = Position { block: block_id, offset: range.end };
                    this.caret.anchor = Position { block: block_id, offset: range.start };
                    this.caret.head = pos;
                    this.reveal_caret = true;
                    window.focus(&this.focus);
                }
            }
            cx.notify();
        }))
        .child(head)
        .children(body)
        .children(status)
        .children(reactions)
}

fn composer_card(
    input: gpui::Entity<gpui_component::input::InputState>,
    send_to_claude: bool,
    has_text: bool,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let top = div().min_h(px(28.)).flex().items_center().gap(px(6.))
        .when(send_to_claude, |d| d.child(
            div().h(px(22.)).px(px(6.)).rounded(px(6.)).bg(rgb(0xe7effd)).text_color(rgb(0x2f6fe4))
                .text_size(px(14.)).font_weight(FontWeight(500.)).flex_none().flex().items_center()
                .child("@Claude"),
        ))
        .child(div().flex_1().min_w_0().h(px(28.)).flex().items_center().text_size(px(14.)).text_color(rgb(0x1f1e1d))
            .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).px(px(0.))));

    let checkbox = div().size(px(16.)).rounded(px(4.)).flex_none().flex().items_center().justify_center()
        .when(send_to_claude, |d| d.bg(rgb(0x2f6fe4)).border_1().border_color(rgb(0x2f6fe4))
            .child(line_icon(Icon::Check, 11., 0xffffff)))
        .when(!send_to_claude, |d| d.bg(rgb(0xffffff)).border_1().border_color(rgb(0xc8c7c1)));

    let foot = div().mt(px(10.)).flex().items_center().justify_between()
        .child(div().id("annotate-send-claude").flex().items_center().gap(px(8.)).cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                if let Some(c) = &mut this.annotate.composer {
                    c.send_to_claude = !c.send_to_claude;
                    let focus_in = c.input.clone();
                    focus_in.update(cx, |i, cx| i.focus(window, cx));
                }
                cx.notify();
            }))
            .child(checkbox)
            .child(div().text_size(px(13.)).text_color(rgb(0x37352f)).child(tr("发送给 Claude", "Send to Claude"))))
        .child(div().id("annotate-submit").size(px(28.)).rounded(px(14.)).flex_none().flex().items_center().justify_center()
            .bg(rgb(if has_text { 0x2f6fe4 } else { 0xe6e6e3 }))
            .when(has_text, |d| d.cursor(CursorStyle::PointingHand))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                this.submit_annotation(window, cx);
            }))
            .child(line_icon(Icon::ArrowUp, 14., 0xffffff)));

    div().id("annotate-composer").key_context("AnnotateComposer").w_full()
        .pt(px(12.)).px(px(14.)).pb(px(10.)).rounded(px(16.))
        .bg(rgb(0xffffff)).border_1().border_color(rgb(0xececea)).shadow_md()
        .on_action(cx.listener(|this, _: &Escape, window, cx| {
            this.close_annotation_composer(window, cx);
            cx.stop_propagation();
        }))
        .child(top)
        .child(foot)
}

fn reaction_popover(picker: ReactionPicker, cx: &mut Context<Editor>) -> impl IntoElement {
    let x = (picker.anchor.x - px(120.)).max(px(24.));
    let y = (picker.anchor.y + px(8.)).max(px(48.));
    let bar = REACTIONS.iter().enumerate().fold(
        div().id("annotate-reactions").absolute().left(x).top(y).px(px(8.)).py(px(6.))
            .rounded(px(12.)).bg(rgb(0xffffff)).border_1().border_color(rgb(0xececea)).shadow_md()
            .flex().items_center().gap(px(4.)).occlude(),
        |row, (idx, &emoji)| {
            row.child(div().id(SharedString::from(format!("pick-emoji-{idx}"))).size(px(28.)).rounded(px(6.))
                .flex().items_center().justify_center().text_size(px(16.)).cursor(CursorStyle::PointingHand)
                .hover(|s| s.bg(rgb(0xf3f2ee)))
                .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
                    cx.stop_propagation();
                    this.pick_reaction(emoji, cx);
                }))
                .child(emoji))
        },
    );
    div().id("annotate-reactions-backdrop").absolute().top_0().left_0().size_full()
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| {
            this.annotate.reactions = None;
            cx.notify();
        }))
        .child(bar)
}
