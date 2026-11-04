//! Fenced code block card: layout margin, painted card, top-right controls, and language dropdown.
use std::{cell::RefCell, collections::{HashMap, HashSet}, time::Duration};
use gpui::{
    App, BorderStyle, Bounds, ClipboardItem, Context, Corners, CursorStyle, Edges, Entity,
    FontWeight, IntoElement, Length, MouseButton, Pixels, Point, Size, Window, anchored, deferred,
    div, point, prelude::*, px, quad, rgb, rgba, size, transparent_black,
};
use crate::{
    dropdown::{self, RowSpec},
    theme::is_dark,
    ui::icon_button::copy_button as ui_copy_button,
};
use super::{
    block_menu::{Cmd, LANGUAGES, Mode, canonical_lang_tag, language_icon, language_label},
    model::Doc,
    view::Editor,
};

const PAD_TOP: f32 = 34.;
const PAD_X: f32 = 11.7;
const PAD_BOTTOM: f32 = 9.75;
const SOURCE_PAD_Y: f32 = 12.;
const SOURCE_PAD_X: f32 = 16.;
const LANG_MENU_W: f32 = 200.;

#[derive(Clone, Copy)]
struct LangPicker {
    block_id: u64,
    fallback: Point<Pixels>,
    index: usize,
}

thread_local! {
    static COPIED_BLOCKS: RefCell<HashSet<u64>> = Default::default();
    static LANG_BOUNDS: RefCell<HashMap<u64, (Bounds<Pixels>, Size<Pixels>)>> = Default::default();
    static LANG_PICKER: RefCell<Option<LangPicker>> = const { RefCell::new(None) };
}

pub(super) fn is_mermaid(language: &str) -> bool {
    canonical_lang_tag(language) == "mermaid"
}

pub(super) fn is_math(language: &str) -> bool {
    canonical_lang_tag(language) == "latex"
}

pub(super) fn is_preview_lang(language: &str) -> bool {
    is_mermaid(language) || is_math(language)
}

pub(super) fn is_menu_open_for(block_id: u64) -> bool {
    LANG_PICKER.with(|slot| slot.borrow().is_some_and(|p| p.block_id == block_id))
}

pub(super) fn close_menus() -> bool {
    LANG_PICKER.with(|slot| slot.borrow_mut().take().is_some())
}

pub(super) fn close_lang_picker_if_open(cx: &mut Context<Editor>) -> bool {
    if close_menus() { cx.notify(); true } else { false }
}

pub(super) fn step_lang_picker(down: bool, cx: &mut Context<Editor>) -> bool {
    let stepped = LANG_PICKER.with(|slot| {
        let mut borrow = slot.borrow_mut();
        let Some(picker) = borrow.as_mut() else { return false };
        let count = LANGUAGES.len();
        let cur = picker.index.min(count.saturating_sub(1));
        picker.index = if down { (cur + 1) % count } else { (cur + count - 1) % count };
        true
    });
    if stepped { cx.notify(); }
    stepped
}

pub(super) fn take_picked_lang(doc: &Doc) -> Option<(u64, &'static str, bool)> {
    let picker = LANG_PICKER.with(|slot| slot.borrow_mut().take())?;
    let (tag, _) = LANGUAGES.get(picker.index.min(LANGUAGES.len() - 1)).copied()?;
    let cur_canon = doc.get(picker.block_id)
        .and_then(|b| b.meta.code_language.as_deref())
        .map(canonical_lang_tag)
        .unwrap_or("");
    Some((picker.block_id, tag, cur_canon == tag))
}

/// Space around the text that the card or source editor occupies.
pub(super) fn margin(language: &str) -> Edges<Length> {
    if is_preview_lang(language) {
        Edges { top: px(SOURCE_PAD_Y).into(), right: px(SOURCE_PAD_X).into(), bottom: px(SOURCE_PAD_Y).into(), left: px(SOURCE_PAD_X).into() }
    } else {
        Edges { top: px(PAD_TOP).into(), right: px(PAD_X).into(), bottom: px(PAD_BOTTOM).into(), left: px(PAD_X).into() }
    }
}

/// Paint the borderless `#f8f8f6` card (`radius 6`) behind normal code blocks.
pub(super) fn paint_card(bounds: Bounds<Pixels>, language: &str, window: &mut Window, _cx: &mut App) {
    if is_preview_lang(language) { return; }
    let bg = if is_dark() { rgb(0x16191e) } else { rgb(0xf8f8f6) };
    let card = Bounds::new(
        bounds.origin - point(px(PAD_X), px(PAD_TOP)),
        size(bounds.size.width + px(PAD_X * 2.), bounds.size.height + px(PAD_TOP + PAD_BOTTOM)),
    );
    window.paint_quad(quad(card, Corners::all(px(6.)), bg, px(0.), transparent_black(), BorderStyle::Solid));
}

fn pill_text(raw_lang: &str) -> String {
    let trimmed = raw_lang.trim();
    let canon = canonical_lang_tag(trimmed);
    if canon == "plaintext" { "Plain Text".to_owned() }
    else if !canon.is_empty() { canon.to_owned() }
    else { trimmed.to_owned() }
}

/// Top-right controls (`Copy` ghost button + language pill) for normal code blocks.
pub(super) fn copy_button(
    editor: Entity<Editor>,
    block_id: u64,
    raw_lang: String,
    visible: bool,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    top_controls(editor, block_id, raw_lang, visible, true, false, cx)
}

/// Top-right controls shared by normal code blocks and renderable blocks.
pub(super) fn top_controls(
    editor: Entity<Editor>,
    block_id: u64,
    raw_lang: String,
    visible: bool,
    show_copy: bool,
    in_preview: bool,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    dropdown::register_closer(close_menus);
    let dark = is_dark();
    let icon_fg: gpui::Hsla = if dark { rgb(0x9aa3b2).into() } else { rgb(0x898781).into() };
    let hover_fg: gpui::Hsla = if dark { rgb(0xf0efed).into() } else { rgb(0x1f1e1d).into() };
    let pill_bg: gpui::Hsla = if dark { rgb(0x1f2228).into() } else { rgb(0xfcfcfb).into() };
    let pill_hover_bg: gpui::Hsla = if dark { rgb(0x2a2e36).into() } else { rgb(0xf4f3f0).into() };
    let pill_border: gpui::Hsla = if dark { rgba(0xffffff1a).into() } else { rgba(0x0b0b0b1a).into() };

    let copied = COPIED_BLOCKS.with(|set| set.borrow().contains(&block_id));
    let cur_canon = canonical_lang_tag(&raw_lang);
    let default_idx = LANGUAGES.iter().position(|(t, _)| *t == "plaintext").unwrap_or(0);
    let display_lang = pill_text(&raw_lang);
    let picker = LANG_PICKER.with(|slot| slot.borrow().filter(|p| p.block_id == block_id));
    let show_bar = visible || picker.is_some();

    let copy_editor = editor.clone();
    let copy_btn = ui_copy_button(("code-copy", block_id as usize), copied, cx.listener(move |this, _, _, cx| {
        cx.stop_propagation();
        let text = this.doc.get(block_id).map(|b| b.text.clone()).unwrap_or_default();
        cx.write_to_clipboard(ClipboardItem::new_string(text));
        COPIED_BLOCKS.with(|set| { set.borrow_mut().insert(block_id); });
        cx.notify();
        let entity = copy_editor.downgrade();
        cx.spawn(async move |_, cx| {
            cx.background_executor().timer(Duration::from_millis(1500)).await;
            let _ = entity.update(cx, |_, cx| {
                COPIED_BLOCKS.with(|set| { set.borrow_mut().remove(&block_id); });
                cx.notify();
            });
        }).detach();
    }));

    let lang_btn = div().id(("code-lang", block_id as usize))
        .h(px(22.)).px(px(8.)).rounded(px(8.)).border_1().border_color(pill_border).bg(pill_bg)
        .flex().items_center().justify_center().cursor(CursorStyle::PointingHand)
        .text_size(px(12.)).line_height(px(18.)).font_weight(FontWeight(400.)).text_color(icon_fg)
        .hover(move |s| s.bg(pill_hover_bg).text_color(hover_fg))
        .when(picker.is_some(), move |s| s.bg(pill_hover_bg).text_color(hover_fg))
        .child(display_lang)
        .child(gpui::canvas(
            move |bounds, window, _| {
                let vp = window.viewport_size();
                LANG_BOUNDS.with(|b| { b.borrow_mut().insert(block_id, (bounds, vp)); });
            },
            |_, _, _, _| {},
        ).absolute().top_0().left_0().size_full())
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseDownEvent, window, cx| {
            cx.stop_propagation();
            if is_menu_open_for(block_id) {
                close_menus();
                window.focus(&this.focus, cx);
                cx.notify();
                return;
            }
            let checked_idx = LANGUAGES.iter().position(|(t, _)| *t == cur_canon).unwrap_or(default_idx);
            let fallback = point(event.position.x - px(LANG_MENU_W), event.position.y + px(12.));
            window.focus(&this.focus, cx);
            LANG_PICKER.with(|slot| {
                *slot.borrow_mut() = Some(LangPicker { block_id, fallback, index: checked_idx });
            });
            cx.notify();
        }));

    let inset = if in_preview { px(4.) } else { px(6.) };
    div().absolute().top(inset).right(inset).h(px(22.)).flex().items_center().gap(px(4.))
        .when(!show_bar, |d| d.invisible())
        .when(show_copy, |d| d.child(copy_btn))
        .child(lang_btn)
        .when_some(picker, |el, p| el.child(lang_dropdown(editor, block_id, cur_canon, p, cx)))
}

fn lang_dropdown(
    editor: Entity<Editor>,
    block_id: u64,
    cur_canon: &'static str,
    picker: LangPicker,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let measured = LANG_BOUNDS.with(|b| b.borrow().get(&block_id).copied());
    let max_h = measured
        .map(|(_, vp)| (f32::from(vp.height) - 16.).clamp(80., dropdown::MAX_HEIGHT))
        .unwrap_or(dropdown::MAX_HEIGHT);
    let mut menu = dropdown::shell(("code-lang-menu", block_id as usize))
        .w(px(LANG_MENU_W))
        .max_h(px(max_h));
    for (idx, (tag, _)) in LANGUAGES.iter().copied().enumerate() {
        let label = language_label(tag);
        let checked = cur_canon == tag;
        let highlighted = idx == picker.index;
        let pick_editor = editor.clone();
        menu = menu.child(
            dropdown::row_with(
                ("code-lang-item", idx),
                RowSpec { icon: language_icon(tag), label: label.into(), checked, highlighted, ..Default::default() },
            )
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                cx.stop_propagation();
                close_menus();
                pick_editor.update(cx, |this, cx| {
                    this.run_block_cmd(block_id, Mode::Handle, Cmd::SetLanguage(tag, checked), window, cx);
                });
            }),
        );
    }
    let anchor = measured
        .map(|(b, vp)| dropdown::anchor_below_right(b, LANG_MENU_W, max_h, Some(vp)))
        .unwrap_or(picker.fallback);
    let backdrop = deferred(anchored().position(point(px(0.), px(0.))).child(
        div().id("code-lang-backdrop").w(px(10000.)).h(px(10000.)).bg(transparent_black())
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                close_menus(); window.focus(&this.focus, cx); cx.notify();
            }))
            .on_mouse_down(MouseButton::Right, cx.listener(|this, _, window, cx| {
                close_menus(); window.focus(&this.focus, cx); cx.notify();
            })),
    )).with_priority(2);
    let popup = dropdown::deferred_popover(anchor, menu, 3);
    div().child(backdrop).child(popup)
}
