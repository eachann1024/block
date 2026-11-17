//! Native Block paper preview for inline AI:
//! - `Input` stage: 1px dashed Ocean accent ring around target block(s).
//! - `Thinking` & `Review` stages: unified dashed Ocean Paper container rendering real parsed `Block`
//!   components (headings, todos, lists, callouts, quotes, code cards, tables, inline code pills)
//!   with block-rise entrance animation and flowing ink caret.
use std::{cell::RefCell, collections::HashMap, time::Duration};
use gpui::{
    Animation, AnimationExt, AnyElement, Bounds, EntityId, FontStyle, FontWeight, HighlightStyle,
    Hsla, IntoElement, Pixels, StrikethroughStyle, StyledText, TextRun, UnderlineStyle, Window,
    div, hsla, point, prelude::*, px, rgb, size, transparent_black,
};
use super::session::{AiStatus, clean_stream};
use crate::{
    editor::{
        chrome::{HANDLE_WIDTH, INDENT_PER_CHAR, RIGHT_INSET, ROW_GAP, block_gutter, collapsed_gap},
        markdown,
        model::{Block, Kind, MarkKind, TableRow},
        text_runs::{attribute_color, color, css},
        view::{Editor, layout::TextLayout},
    },
    icons::{Icon, IconColors, icon},
    theme::{Theme, is_dark},
};

thread_local! {
    static LIVE_PREVIEW_BOUNDS: RefCell<HashMap<EntityId, Bounds<Pixels>>> = Default::default();
}

pub(in crate::editor) fn live_preview_bounds(editor: EntityId) -> Option<Bounds<Pixels>> {
    LIVE_PREVIEW_BOUNDS.with(|m| m.borrow().get(&editor).copied())
}

pub(in crate::editor) fn clear_preview_bounds(editor: EntityId) {
    LIVE_PREVIEW_BOUNDS.with(|m| {
        m.borrow_mut().remove(&editor);
    });
}

pub(in crate::editor) fn ai_preview_key(editor: &Editor, block: u64) -> u64 {
    let Some(s) = editor.ai_session() else { return 0 };
    if !s.parts.iter().any(|(id, _)| *id == block) {
        return 0;
    }
    1
}

impl Editor {
    pub(in crate::editor) fn ai_preview_key(&self, block: u64) -> u64 {
        ai_preview_key(self, block)
    }

    pub(in crate::editor) fn ai_preview(&self, _block: u64, _runs: &[TextRun]) -> Option<(String, Vec<TextRun>)> {
        None
    }
}

/// Dashed ring around target block(s) while in `AiStatus::Input`.
pub(in crate::editor) fn paint_outline(
    editor_id: EntityId,
    editor: &Editor,
    block: u64,
    layout: &TextLayout,
    window: &mut Window,
) {
    let Some(s) = editor.ai_session() else { return };
    if !matches!(s.status, AiStatus::Input) {
        return;
    }
    if !s.parts.iter().any(|(id, _)| *id == block) {
        return;
    }
    let zoom = crate::editor::zoom::zoom_factor(editor.minimal);
    let b = layout.bounds;
    let ring = Bounds::new(
        b.origin - point(px(8. * zoom), px(5. * zoom)),
        b.size + size(px(16. * zoom), px(10. * zoom)),
    );
    if s.parts.first().is_some_and(|(id, _)| *id == block) {
        LIVE_PREVIEW_BOUNDS.with(|m| {
            m.borrow_mut().insert(editor_id, ring);
        });
    } else if s.parts.last().is_some_and(|(id, _)| *id == block) {
        LIVE_PREVIEW_BOUNDS.with(|m| {
            let mut map = m.borrow_mut();
            let combined = map.get(&editor_id).map_or(ring, |prev| prev.union(&ring));
            map.insert(editor_id, combined);
        });
    }
    window.paint_quad(gpui::quad(
        ring,
        px(10. * zoom),
        transparent_black(),
        px(1.),
        Theme::ocean().accent.opacity(0.85),
        gpui::BorderStyle::Dashed,
    ));
}

fn pulse_alpha(tick: usize) -> f32 {
    let phase = (tick % 18) as f32 / 18.0;
    let wave = (phase * std::f32::consts::TAU).cos() * 0.5 + 0.5;
    0.22 + 0.78 * wave
}

fn block_styled_text(block: &Block, caret: Option<Hsla>) -> StyledText {
    let dark = is_dark();
    let code_fg: Hsla = if dark { rgb(0xf28b82).into() } else { rgb(0xb42318).into() };
    let code_bg: Hsla = if dark { rgb(0x2c2c2e).into() } else { rgb(0xf3f1ec).into() };

    let mut points = vec![0, block.text.len()];
    for span in &block.spans {
        points.extend([span.start, span.end]);
    }
    for tag in &block.meta.inline {
        points.extend([tag.start, tag.end]);
    }
    points.retain(|at| *at <= block.text.len() && block.text.is_char_boundary(*at));
    points.sort_unstable();
    points.dedup();

    let mut highlights = Vec::new();
    for range in points.windows(2).map(|pair| pair[0]..pair[1]).filter(|r| !r.is_empty()) {
        let (mut style, mut changed) = (HighlightStyle::default(), false);
        for span in block.spans.iter().filter(|s| s.start <= range.start && range.end <= s.end) {
            if span.marks.contains(&MarkKind::Bold) {
                style.font_weight = Some(FontWeight(650.));
                changed = true;
            }
            if span.marks.contains(&MarkKind::Italic) {
                style.font_style = Some(FontStyle::Italic);
                changed = true;
            }
            if span.marks.contains(&MarkKind::Strike) {
                style.strikethrough = Some(StrikethroughStyle { color: None, thickness: px(1.) });
                changed = true;
            }
            if span.marks.contains(&MarkKind::Code) {
                style.background_color = Some(code_bg);
                style.color = Some(code_fg);
                style.font_weight = Some(FontWeight(500.));
                changed = true;
            }
        }
        for tag in block.meta.inline.iter().filter(|t| t.start <= range.start && range.end <= t.end) {
            if tag.name == "a" {
                style.color = Some(rgb(0x3d63b0).into());
                changed = true;
            }
            if matches!(tag.name.as_str(), "u" | "ins")
                || css(&tag.attrs, "text-decoration").is_some_and(|v| v.contains("underline"))
            {
                style.underline = Some(UnderlineStyle { color: None, thickness: px(1.), wavy: false });
                changed = true;
            }
            if css(&tag.attrs, "text-decoration").is_some_and(|v| v.contains("line-through")) {
                style.strikethrough = Some(StrikethroughStyle { color: style.color, thickness: px(1.) });
                changed = true;
            }
            if tag.name == "mark" && style.background_color.is_none() {
                style.background_color = color("yellow", true);
                changed = true;
            }
            if let Some(fg) = attribute_color(&tag.attrs, false) {
                style.color = Some(fg);
                changed = true;
            }
            if let Some(bg) = attribute_color(&tag.attrs, true) {
                style.background_color = Some(bg);
                changed = true;
            }
        }
        if changed {
            highlights.push((range, style));
        }
    }

    let mut full = block.text.clone();
    if let Some(caret_color) = caret {
        let start = full.len();
        full.push_str("\u{200a}\u{258e}");
        highlights.push((
            start..full.len(),
            HighlightStyle {
                color: Some(caret_color),
                font_weight: Some(FontWeight(600.)),
                ..Default::default()
            },
        ));
    }
    StyledText::new(full).with_highlights(highlights)
}

fn struck_original_block(block: &Block, range: &std::ops::Range<usize>, minimal: bool, zoom: f32) -> AnyElement {
    let theme = Theme::ocean();
    let dark = is_dark();
    let del_fg: Hsla = if dark { rgb(0xf28b82).into() } else { rgb(0xb42318).into() };
    let del_bg: Hsla = if dark { hsla(4. / 360., 0.55, 0.25, 0.45) } else { rgb(0xfbe9e7).into() };
    let (size_px, factor) = crate::ui::editor_content::heading_metrics(
        block.kind,
        block.meta.heading_level,
        block.meta.code_language.is_some(),
        minimal,
        f32::from(theme.editor_font),
        zoom,
    );
    let line_px = size_px * factor;
    let text = &block.text;
    let start = range.start.min(text.len());
    let end = range.end.min(text.len()).max(start);
    let whole = start == 0 && end == text.len();

    if whole {
        return div()
            .mx(px(-6. * zoom))
            .px(px(6. * zoom))
            .py(px(2. * zoom))
            .rounded(px(6. * zoom))
            .bg(del_bg)
            .text_color(del_fg)
            .text_size(px(size_px))
            .line_height(px(line_px))
            .line_through()
            .child(text.clone())
            .into_any_element();
    }

    let mut highlights = Vec::new();
    if start < end {
        highlights.push((
            start..end,
            HighlightStyle {
                color: Some(del_fg),
                background_color: Some(del_bg),
                strikethrough: Some(StrikethroughStyle {
                    color: Some(del_fg),
                    thickness: px(1.),
                }),
                ..Default::default()
            },
        ));
    }
    div()
        .py(px(1. * zoom))
        .text_size(px(size_px))
        .line_height(px(line_px))
        .text_color(theme.text)
        .child(StyledText::new(text.clone()).with_highlights(highlights))
        .into_any_element()
}

fn table_row_view(row: &TableRow, bottom_border: bool, zoom: f32) -> impl IntoElement {
    let theme = Theme::ocean();
    let dark = is_dark();
    let is_header = row.cells.iter().any(|c| c.header);
    let header_bg: Hsla = if dark { gpui::rgba(0xffffff08).into() } else { rgb(0xf5f3ed).into() };
    let count = row.cells.len();
    div()
        .flex()
        .w_full()
        .min_w_0()
        .when(bottom_border, |el| el.border_b_1().border_color(theme.hairline))
        .when(is_header, |el| el.bg(header_bg))
        .children(row.cells.iter().enumerate().map(|(idx, cell)| {
            div()
                .flex_1()
                .min_w_0()
                .px(px(8. * zoom))
                .py(px(6. * zoom))
                .text_size(px(14. * zoom))
                .line_height(px(21. * zoom))
                .when(idx + 1 < count, |el| el.border_r_1().border_color(theme.hairline))
                .when(cell.header, |el| el.font_weight(FontWeight(600.)).text_color(theme.text_secondary))
                .child(block_styled_text(&cell.content, None))
        }))
}

fn render_native_block(
    block: &Block,
    top_gap: f32,
    list_number: Option<usize>,
    caret: Option<Hsla>,
    minimal: bool,
    zoom: f32,
    anim_key: Option<(u64, usize)>,
) -> AnyElement {
    let theme = Theme::ocean();
    let dark = is_dark();
    let (size_px, factor) = crate::ui::editor_content::heading_metrics(
        block.kind,
        block.meta.heading_level,
        block.meta.code_language.is_some(),
        minimal,
        f32::from(theme.editor_font),
        zoom,
    );
    let line_px = size_px * factor;
    let indent_px = block.meta.indent.min(12) as f32 * INDENT_PER_CHAR * zoom;
    let marker_width = block_gutter(block, zoom);

    let inner: gpui::Div = if crate::editor::block_menu::is_divider(block) {
        div()
            .w_full()
            .h(px(16. * zoom))
            .flex()
            .items_center()
            .child(div().h(px(1. * zoom)).w_full().bg(if dark { theme.hairline } else { gpui::rgba(0x0b0b0b33).into() }))
    } else if let Some(table) = &block.meta.table {
        let rows_len = table.rows.len();
        div()
            .w_full()
            .min_w_0()
            .rounded(px(8. * zoom))
            .border_1()
            .border_color(theme.hairline)
            .bg(theme.paper)
            .overflow_hidden()
            .children(table.rows.iter().enumerate().map(|(i, r)| table_row_view(r, i + 1 < rows_len, zoom)))
    } else if block.meta.code_language.is_some() {
        div()
            .w_full()
            .min_w_0()
            .px(px(16. * zoom))
            .py(px(12. * zoom))
            .rounded(px(8. * zoom))
            .bg(theme.code_bg)
            .border_1()
            .border_color(theme.hairline)
            .font_family(crate::theme::mono_font_family())
            .text_size(px(size_px))
            .line_height(px(line_px))
            .text_color(theme.text)
            .child(block_styled_text(block, caret))
    } else if let Some(callout_kind) = block.meta.callout {
        let st = crate::ui::callout_controls::style(callout_kind);
        crate::ui::callout_controls::style_card(
            div()
                .relative()
                .w_full()
                .min_w_0()
                .text_size(px(size_px))
                .line_height(px(line_px))
                .text_color(theme.text),
            callout_kind,
            zoom,
        )
        .child(
            div()
                .absolute()
                .top(px(7. * zoom))
                .left(px(10. * zoom))
                .h(px(26. * zoom))
                .flex()
                .items_center()
                .gap(px(4. * zoom))
                .child(
                    div()
                        .size(px(26. * zoom))
                        .flex()
                        .items_center()
                        .justify_center()
                        .child(icon(
                            Icon::Callout,
                            px(crate::ui::icon_size::ROW * zoom),
                            IconColors {
                                line: st.accent,
                                fill: st.icon_fill,
                                accent: st.accent,
                            },
                        )),
                )
                .child(
                    div()
                        .h(px(24. * zoom))
                        .px(px(4. * zoom))
                        .flex()
                        .items_center()
                        .text_size(px(14. * zoom))
                        .line_height(px(20. * zoom))
                        .font_weight(crate::ui::type_scale::TITLE)
                        .text_color(st.text)
                        .child(crate::ui::callout_controls::label(callout_kind)),
                ),
        )
        .child(block_styled_text(block, caret))
    } else if marker_width > 0. {
        let checked = block.checked;
        let check_border: Hsla = if checked || caret.is_some() {
            rgb(0x4774cf).into()
        } else if dark {
            rgb(0x858585).into()
        } else {
            gpui::rgba(0x0b0b0b80).into()
        };
        let marker = match block.kind {
            Kind::Bullet => "•".to_owned(),
            Kind::Numbered => format!("{}.", list_number.unwrap_or(1)),
            _ => String::new(),
        };
        div()
            .w_full()
            .min_w_0()
            .flex()
            .items_start()
            .child(
                div()
                    .w(px(marker_width))
                    .pl(px(indent_px))
                    .flex_none()
                    .text_color(theme.text)
                    .text_size(px(size_px))
                    .line_height(px(line_px))
                    .when(block.kind == Kind::Todo, |col| {
                        col.child(
                            div()
                                .mt(px((line_px - 16. * zoom).max(0.) * 0.5))
                                .ml(px(4.8 * zoom))
                                .size(px(16. * zoom))
                                .flex_none()
                                .rounded(px(4. * zoom))
                                .when(!checked, |b| b.border_1().border_color(check_border).bg(theme.paper))
                                .when(checked, |b| {
                                    b.bg(rgb(0x4774cf)).flex().items_center().justify_center().child(icon(
                                        Icon::Check,
                                        px(12. * zoom),
                                        IconColors {
                                            line: rgb(0xffffff).into(),
                                            fill: transparent_black(),
                                            accent: rgb(0xffffff).into(),
                                        },
                                    ))
                                }),
                        )
                    })
                    .when(!marker.is_empty(), |col| col.flex().justify_end().pr(px(8. * zoom)).child(marker)),
            )
            .child(
                div()
                    .flex_1()
                    .min_w_0()
                    .text_size(px(size_px))
                    .line_height(px(line_px))
                    .text_color(theme.text)
                    .child(block_styled_text(block, caret)),
            )
    } else if block.kind == Kind::Quote {
        div()
            .relative()
            .w_full()
            .min_w_0()
            .pl(px(16. * zoom))
            .py(px(1.6 * zoom))
            .text_size(px(size_px))
            .line_height(px(line_px))
            .text_color(theme.text)
            .child(div().absolute().left_0().top_0().bottom_0().w(px(3. * zoom)).bg(theme.quote_bar))
            .child(block_styled_text(block, caret))
    } else {
        let is_heading = matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3);
        let is_h6 = block.kind == Kind::H3 && block.meta.heading_level == Some(6);
        div()
            .w_full()
            .min_w_0()
            .text_size(px(size_px))
            .line_height(px(line_px))
            .text_color(if is_h6 { theme.text_secondary } else { theme.text })
            .when(is_heading, |d| d.font_weight(FontWeight(if is_h6 { 500. } else { 600. })))
            .child(block_styled_text(block, caret))
    };

    let node = div().w_full().min_w_0().pt(px(top_gap)).child(inner);
    if let Some((serial, idx)) = anim_key {
        node.with_animation(
            gpui::ElementId::from(gpui::SharedString::from(format!("ai-blk-{serial}-{idx}"))),
            Animation::new(Duration::from_millis(180)).with_easing(gpui::ease_out_quint()),
            |el, delta| el.opacity(delta).mt(px((1.0 - delta) * 4.0)),
        )
        .into_any_element()
    } else {
        node.into_any_element()
    }
}

/// Renders the live native Block preview inside the editor's virtual list during `Thinking` and `Review`.
pub(in crate::editor) fn render_stream_row(
    editor: &Editor,
    index: usize,
    cx: &mut gpui::Context<Editor>,
) -> Option<AnyElement> {
    let s = editor.ai_session()?;
    let (source, streaming, tick) = match &s.status {
        AiStatus::Thinking { tick, partial, .. } => (clean_stream(partial), true, *tick),
        AiStatus::Review { draft } => (draft.clone(), false, 0),
        AiStatus::Input => return None,
    };

    let first_id = s.parts.first()?.0;
    let last_id = s.parts.last()?.0;
    let first_idx = editor.doc.index_of(first_id)?;
    let last_idx = editor.doc.index_of(last_id)?;
    let (lo, hi) = (first_idx.min(last_idx), first_idx.max(last_idx));
    if index < lo || index > hi {
        return None;
    }
    if index > lo {
        return Some(div().id(("ai-merged-row", index)).into_any_element());
    }

    let theme = Theme::ocean();
    let dark = is_dark();
    let zoom = crate::editor::zoom::zoom_factor(editor.minimal);
    let (pt, pb) = crate::editor::chrome::row_pads(editor, index);
    let serial = s.serial;
    let empty_ctx = s.is_empty_ctx();

    let parsed = if source.trim().is_empty() {
        Vec::new()
    } else {
        markdown::parse(&source).blocks.iter().cloned().collect::<Vec<_>>()
    };

    let caret_color = streaming.then(|| theme.accent.opacity(pulse_alpha(tick)));

    let mut num_run = 0usize;
    let total_blocks = parsed.len();
    let rendered_blocks: Vec<AnyElement> = parsed
        .iter()
        .enumerate()
        .map(|(idx, blk)| {
            let num = if blk.kind == Kind::Numbered {
                num_run = if let Some(start) = blk.meta.list_start {
                    start as usize
                } else {
                    num_run + 1
                };
                Some(num_run)
            } else {
                num_run = 0;
                None
            };
            let top_gap = if idx == 0 {
                0.
            } else {
                collapsed_gap(&parsed[idx - 1], blk) * zoom
            };
            let is_last = idx + 1 == total_blocks;
            render_native_block(
                blk,
                top_gap,
                num,
                if is_last { caret_color } else { None },
                editor.minimal,
                zoom,
                streaming.then_some((serial, idx)),
            )
        })
        .collect();

    let struck_originals: Vec<AnyElement> = if !empty_ctx {
        s.parts
            .iter()
            .filter_map(|(bid, r)| {
                let blk = editor.doc.get(*bid)?;
                if blk.text.is_empty() {
                    None
                } else {
                    Some(struck_original_block(blk, r, editor.minimal, zoom))
                }
            })
            .collect()
    } else {
        Vec::new()
    };

    let tint_bg: Hsla = if dark {
        hsla(218. / 360., 0.36, 0.32, 0.18)
    } else {
        hsla(220. / 360., 0.78, 0.96, 0.65)
    };
    let accent = theme.accent;
    let editor_entity = cx.entity();
    let editor_id = cx.entity_id();

    let outline_canvas = gpui::canvas(
        |_, _, _| (),
        move |bounds, (), window, cx| {
            window.paint_quad(gpui::quad(
                bounds,
                px(10. * zoom),
                tint_bg,
                px(1.),
                accent,
                gpui::BorderStyle::Dashed,
            ));
            LIVE_PREVIEW_BOUNDS.with(|m| {
                m.borrow_mut().insert(editor_id, bounds);
            });
            editor_entity.update(cx, |this, _| {
                this.painted_rows.insert(first_id, bounds);
            });
        },
    )
    .absolute()
    .inset_0();

    let left_pad = (HANDLE_WIDTH + ROW_GAP - 8. * zoom).max(0.);
    let right_pad = if editor.minimal { 0. } else { (RIGHT_INSET - 8. * zoom).max(0.) };
    let preview_input = s.input.clone();

    Some(
        div()
            .id(("ai-stream-row", first_id as usize))
            .relative()
            .w_full()
            .min_w_0()
            .pt(px(pt))
            .pb(px(pb))
            .pl(px(left_pad))
            .pr(px(right_pad))
            .child(
                div()
                    .relative()
                    .w_full()
                    .max_w(theme.content_width + px(16. * zoom))
                    .px(px(8. * zoom))
                    .py(px(8. * zoom))
                    .on_mouse_down(gpui::MouseButton::Left, move |_, window, cx| {
                        preview_input.update(cx, |i, cx| i.focus(window, cx));
                        cx.stop_propagation();
                    })
                    .child(outline_canvas)
                    .child(
                        div()
                            .relative()
                            .w_full()
                            .min_w_0()
                            .font_family(crate::theme::editor_font().0)
                            .flex()
                            .flex_col()
                            .when(!struck_originals.is_empty(), |d| {
                                d.child(
                                    div()
                                        .w_full()
                                        .min_w_0()
                                        .pb(px(8. * zoom))
                                        .flex()
                                        .flex_col()
                                        .gap(px(4. * zoom))
                                        .children(struck_originals),
                                )
                            })
                            .when(rendered_blocks.is_empty() && streaming, |d| {
                                d.child(
                                    div()
                                        .h(px(24. * zoom))
                                        .flex()
                                        .items_center()
                                        .child(
                                            div()
                                                .w(px(2.))
                                                .h(px(18. * zoom))
                                                .rounded(px(1.))
                                                .bg(accent.opacity(pulse_alpha(tick))),
                                        ),
                                )
                            })
                            .children(rendered_blocks),
                    ),
            )
            .into_any_element(),
    )
}
