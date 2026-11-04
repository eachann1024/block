//! Fenced code block card: padding via layout margin, painted background, border, language picker and copy button.
use std::{cell::RefCell, collections::{HashMap, HashSet}, time::Duration};
use gpui::{
    App, BorderStyle, Bounds, ClipboardItem, Corners, Context, CursorStyle, Edges, Entity,
    Focusable, FontWeight, IntoElement, Length, MouseButton, Pixels, Point, Subscription, Window,
    anchored, deferred, div, point, prelude::*, px, quad, rgb, size, transparent_black,
};
use gpui_component::{Sizable, input::{Input, InputEvent, InputState}};
use crate::{icons::{Icon, IconColors, icon}, theme::{Theme, is_dark}};
use super::{
    block_menu::{Cmd, LANGUAGES, Mode, language_label},
    menu_style,
    view::Editor,
};

const HEADER_H: f32 = 30.;
const PAD_TOP: f32 = 12.;
const PAD_X: f32 = 14.;
const PAD_BOTTOM: f32 = 14.;

struct LangPicker {
    block_id: u64,
    anchor: Point<Pixels>,
    input: Entity<InputState>,
    _sub: Subscription,
}

thread_local! {
    static COPIED_BLOCKS: RefCell<HashSet<u64>> = Default::default();
    static LANG_BOUNDS: RefCell<HashMap<u64, Bounds<Pixels>>> = Default::default();
    static LANG_PICKER: RefCell<Option<LangPicker>> = const { RefCell::new(None) };
}

pub(super) fn is_mermaid(language: &str) -> bool {
    language.trim().eq_ignore_ascii_case("mermaid")
}

pub(super) fn is_math(language: &str) -> bool {
    matches!(language.trim().to_ascii_lowercase().as_str(), "math" | "latex" | "tex" | "katex")
}

pub(super) fn is_preview_lang(language: &str) -> bool {
    is_mermaid(language) || is_math(language)
}

/// Space around the text that the card occupies (30px header + 12px/14px/14px code area padding).
pub(super) fn margin(_language: &str) -> Edges<Length> {
    let top = HEADER_H + PAD_TOP;
    Edges { top: px(top).into(), right: px(PAD_X).into(), bottom: px(PAD_BOTTOM).into(), left: px(PAD_X).into() }
}

/// Paint the card behind the text `bounds` (which exclude the margin).
pub(super) fn paint_card(bounds: Bounds<Pixels>, language: &str, window: &mut Window, _cx: &mut App) {
    let dark = is_dark();
    let bg = if dark { rgb(0x16191e) } else { rgb(0xf5f6f8) };
    let border = if dark { rgb(0x282c34) } else { rgb(0xeceef1) };
    let top = HEADER_H + PAD_TOP;
    let card = Bounds::new(
        bounds.origin - point(px(PAD_X), px(top)),
        size(bounds.size.width + px(PAD_X * 2.), bounds.size.height + px(top + PAD_BOTTOM)),
    );
    let radii = if is_preview_lang(language) {
        Corners { top_left: px(8.), top_right: px(8.), bottom_right: px(0.), bottom_left: px(0.) }
    } else {
        Corners::all(px(8.))
    };
    window.paint_quad(quad(card, radii, bg, px(1.), border, BorderStyle::Solid));
    window.paint_quad(gpui::fill(
        Bounds::new(card.origin + point(px(1.), px(HEADER_H)), size(card.size.width - px(2.), px(1.))),
        border,
    ));
}

/// Rendered preview section for ```mermaid and ```math blocks (standalone card or edit-mode footer).
pub(super) fn preview_card(
    editor: Entity<Editor>,
    block_id: u64,
    language: &str,
    source: &str,
    in_edit_mode: bool,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let dark = is_dark();
    let bg = if dark { rgb(0x16191e) } else { rgb(0xf5f6f8) };
    let border = if dark { rgb(0x282c34) } else { rgb(0xeceef1) };
    let content = if is_mermaid(language) {
        super::diagram_preview::view(editor, block_id, source, cx)
    } else {
        super::math_preview::view(editor, block_id, source, cx)
    };
    if in_edit_mode {
        div().id(("code-preview", block_id as usize)).w_full().min_w_0().px(px(PAD_X)).py(px(14.))
            .bg(bg).border_l_1().border_r_1().border_b_1().border_color(border).rounded_b(px(8.))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                window.focus(&this.focus);
                cx.stop_propagation();
            }))
            .child(content)
            .into_any_element()
    } else {
        div().id(("code-preview", block_id as usize)).w_full().min_w_0().pt(px(HEADER_H))
            .bg(bg).border_1().border_color(border).rounded(px(8.)).cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                this.block_sel = None;
                this.focus_block(block_id, window, cx);
                cx.stop_propagation();
            }))
            .child(div().w_full().min_w_0().px(px(PAD_X)).py(px(16.)).border_t_1().border_color(border).child(content))
            .into_any_element()
    }
}

fn close_lang_picker() {
    LANG_PICKER.with(|slot| { *slot.borrow_mut() = None; });
}

fn filtered_languages(query: &str) -> Vec<(&'static str, &'static str)> {
    let q = query.trim().to_lowercase();
    LANGUAGES.iter().copied()
        .filter(|(tag, label)| q.is_empty() || tag.contains(&q) || label.to_lowercase().contains(&q))
        .collect()
}

/// Header controls (clickable language picker + "复制代码" button) overlaid on a fenced code block.
pub(super) fn copy_button(editor: Entity<Editor>, block_id: u64, raw_lang: String, cx: &mut Context<Editor>) -> impl IntoElement {
    let dark = is_dark();
    let base_fg: gpui::Hsla = if dark { rgb(0x9aa3b2).into() } else { rgb(0x526071).into() };
    let hover_fg: gpui::Hsla = if dark { rgb(0xe6e9ef).into() } else { rgb(0x16181c).into() };
    let hover_bg = if dark { rgb(0x252a34) } else { rgb(0xe8eaee) };
    let copied = COPIED_BLOCKS.with(|set| set.borrow().contains(&block_id));
    let glyph = if copied { Icon::Check } else { Icon::Copy };
    let tint = |line| IconColors { line, fill: transparent_black(), accent: transparent_black() };
    let mapped = language_label(&raw_lang);
    let display_lang = if mapped == "其他" && !raw_lang.is_empty() { raw_lang.clone() } else { mapped.to_owned() };
    let picker_state = LANG_PICKER.with(|slot| {
        slot.borrow().as_ref().filter(|p| p.block_id == block_id).map(|p| (p.anchor, p.input.clone()))
    });

    let lang_btn = div()
        .id(("code-lang", block_id as usize))
        .group("code-lang")
        .absolute()
        .top(px(3.))
        .left(px(8.))
        .h(px(24.))
        .px(px(6.))
        .rounded(px(4.))
        .flex()
        .items_center()
        .gap(px(4.))
        .cursor(CursorStyle::PointingHand)
        .text_size(px(12.))
        .text_color(base_fg)
        .hover(move |s| s.bg(hover_bg).text_color(hover_fg))
        .when(picker_state.is_some(), move |s| s.bg(hover_bg).text_color(hover_fg))
        .child(display_lang)
        .child(icon(Icon::TbChevron, px(10.), tint(base_fg)))
        .child(gpui::canvas(
            move |bounds, _, _| { LANG_BOUNDS.with(|b| { b.borrow_mut().insert(block_id, bounds); }); },
            |_, _, _, _| {},
        ).absolute().top_0().left_0().size_full())
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, event: &gpui::MouseDownEvent, window, cx| {
            cx.stop_propagation();
            let already_open = LANG_PICKER.with(|slot| slot.borrow().as_ref().is_some_and(|p| p.block_id == block_id));
            if already_open {
                close_lang_picker();
                window.focus(&this.focus);
                cx.notify();
                return;
            }
            let anchor = LANG_BOUNDS.with(|b| b.borrow().get(&block_id).copied())
                .map(|b| point(b.left(), b.bottom() + px(4.)))
                .unwrap_or_else(|| event.position + point(px(0.), px(16.)));
            let input = cx.new(|cx| InputState::new(window, cx).placeholder("搜索语言…"));
            let input_ref = input.clone();
            let sub = cx.subscribe_in(&input, window, move |this, _, ev: &InputEvent, window, cx| {
                match ev {
                    InputEvent::Change => cx.notify(),
                    InputEvent::PressEnter { .. } => {
                        let q = input_ref.read(cx).value().to_string();
                        if let Some((tag, label)) = filtered_languages(&q).first().copied() {
                            let cur = this.doc.get(block_id)
                                .and_then(|b| b.meta.code_language.as_deref())
                                .map(language_label).unwrap_or("");
                            close_lang_picker();
                            this.run_block_cmd(block_id, Mode::Handle, Cmd::SetLanguage(tag, label == cur), window, cx);
                        }
                    }
                    _ => {}
                }
            });
            let focus = input.read(cx).focus_handle(cx);
            window.focus(&focus);
            LANG_PICKER.with(|slot| {
                *slot.borrow_mut() = Some(LangPicker { block_id, anchor, input, _sub: sub });
            });
            cx.notify();
        }));

    let copy_editor = editor.clone();
    let copy_btn = div()
        .id(("code-copy", block_id as usize))
        .group("code-copy")
        .absolute()
        .top(px(3.))
        .right(px(8.))
        .size(px(24.))
        .rounded(px(4.))
        .flex()
        .items_center()
        .justify_center()
        .cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(hover_bg))
        .child(
            div().relative().flex_none().size(px(13.))
                .child(div().group_hover("code-copy", |s| s.invisible()).child(icon(glyph, px(13.), tint(base_fg))))
                .child(div().absolute().top_0().left_0().invisible().group_hover("code-copy", |s| s.visible())
                    .child(icon(glyph, px(13.), tint(hover_fg)))),
        )
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, _, cx| {
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

    div().absolute().top_0().left_0().right_0().h(px(HEADER_H))
        .child(lang_btn)
        .child(copy_btn)
        .when_some(picker_state, |el, (anchor, input)| {
            el.child(lang_dropdown(editor.clone(), block_id, mapped, anchor, input, cx))
        })
}

fn lang_dropdown(
    editor: Entity<Editor>,
    block_id: u64,
    current_label: &'static str,
    anchor: Point<Pixels>,
    input: Entity<InputState>,
    cx: &mut Context<Editor>,
) -> impl IntoElement {
    let theme = Theme::ocean();
    let query = input.read(cx).value().to_string();
    let items = filtered_languages(&query);
    let mut list = div().id(("code-lang-list", block_id as usize))
        .max_h(px(220.)).overflow_y_scroll().flex().flex_col().gap(px(2.));
    for (idx, (tag, label)) in items.into_iter().enumerate() {
        let checked = label == current_label;
        let pick_editor = editor.clone();
        list = list.child(
            div().id(("code-lang-item", idx)).w_full().min_h(theme.menu_item_height).flex_none()
                .px(px(8.)).rounded(px(4.)).flex().items_center().justify_between()
                .text_size(px(13.)).text_color(theme.text).cursor(CursorStyle::PointingHand)
                .hover({ let h = theme.hover; move |s| s.bg(h) })
                .when(checked, |s| s.font_weight(FontWeight(600.)))
                .child(label)
                .when(checked, |s| s.child(icon(Icon::Check, px(14.), theme.icon_colors(theme.accent))))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    cx.stop_propagation();
                    close_lang_picker();
                    pick_editor.update(cx, |this, cx| {
                        this.run_block_cmd(block_id, Mode::Handle, Cmd::SetLanguage(tag, checked), window, cx);
                    });
                }),
        );
    }
    let card = menu_style::shell(("code-lang-menu", block_id as usize), &theme)
        .w(px(184.)).gap(px(4.))
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .child(div().px(px(2.)).pt(px(2.)).child(Input::new(&input).xsmall().cleanable(true)))
        .child(menu_style::separator(&theme))
        .child(list);
    let backdrop = deferred(anchored().position(point(px(0.), px(0.))).child(
        div().id("code-lang-backdrop").w(px(10000.)).h(px(10000.)).bg(transparent_black())
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
                close_lang_picker(); window.focus(&this.focus); cx.notify();
            }))
            .on_mouse_down(MouseButton::Right, cx.listener(|this, _, window, cx| {
                close_lang_picker(); window.focus(&this.focus); cx.notify();
            })),
    )).with_priority(2);
    let popup = deferred(anchored().position(anchor).snap_to_window_with_margin(px(8.)).child(card)).with_priority(3);
    div().child(backdrop).child(popup)
}
