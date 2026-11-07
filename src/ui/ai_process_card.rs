use std::{cell::{Cell, RefCell}, collections::{HashMap, HashSet}, rc::Rc, time::Duration};
use gpui::{
    Animation, AnimationExt, AnyElement, App, BoxShadow, CursorStyle, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, ScrollHandle, SharedString, Window, canvas, div, point,
    prelude::*, px, rgba, size,
};
use crate::{icons::{Icon, icon}, theme::{is_en, tr}, ui::smoke_overlay};
pub use super::ai_process_status::{
    DetailTone, LoaderPattern, ProcessDetailLine, ProcessDiffChip, ProcessElapsed,
    ProcessStepIcon, ProcessStepMeta, TaskStatus,
};
use super::ai_process_status;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ProcessStage { Skill, Scan, Read, Write, Synth, Done }

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ProcessStep {
    pub label: String,
    pub detail: String,
    pub merged_count: usize,
    pub running: bool,
}

pub struct ProcessCardSpec {
    pub key: String,
    pub stage: ProcessStage,
    pub headline: String,
    pub steps: Vec<ProcessStep>,
    pub notes: Vec<String>,
    pub steps_expanded: bool,
    pub steps_toggle_seq: usize,
    pub notes_expanded: bool,
    pub steps_scroll: ScrollHandle,
}

#[derive(Clone, Default)]
pub struct ProcessCardOptions {
    pub status: Option<TaskStatus>,
    pub elapsed: Option<ProcessElapsed>,
    pub loader: LoaderPattern,
    pub step_statuses: Vec<TaskStatus>,
    pub step_details: Vec<String>,
    pub step_metas: Vec<ProcessStepMeta>,
    pub diff_chips: Vec<ProcessDiffChip>,
    pub on_details_changed: Option<Rc<dyn Fn(&mut App)>>,
}

thread_local! {
    static EXPANDED_DETAILS: RefCell<HashSet<(String, usize)>> = RefCell::new(HashSet::new());
    static DETAIL_HEIGHTS: RefCell<HashMap<String, gpui::Pixels>> = RefCell::new(HashMap::new());
    static HOVERED_CHIP: RefCell<Option<(String, usize)>> = const { RefCell::new(None) };
    static SMOKE_SCROLL: RefCell<Option<ScrollHandle>> = const { RefCell::new(None) };
}

pub(crate) fn smoke_expanded_details() -> Vec<(String, usize)> {
    EXPANDED_DETAILS.with(|open| open.borrow().iter().cloned().collect())
}

pub(crate) fn smoke_scroll_offset() -> Option<gpui::Point<gpui::Pixels>> {
    SMOKE_SCROLL.with(|scroll| scroll.borrow().as_ref().map(ScrollHandle::offset))
}

fn fallback_lines(raw: Option<&String>) -> Vec<ProcessDetailLine> {
    raw.into_iter().flat_map(|text| text.lines()).map(str::trim).filter(|line| !line.is_empty()).map(|line| {
        let tone = if line.starts_with("+ ") || line.starts_with('+') { DetailTone::Add }
            else if line.starts_with("- ") || line.starts_with("− ") { DetailTone::Del }
            else { DetailTone::Neutral };
        ProcessDetailLine { text: line.to_owned(), tone }
    }).collect()
}

fn fallback_chips(notes: &[String]) -> Vec<ProcessDiffChip> {
    notes.iter().map(|note| {
        let file = if note.contains('.') { note.clone() } else { format!("{note}.md") };
        let add = note.chars().count().max(3) * 2;
        ProcessDiffChip {
            file: file.clone(), add, del: 0,
            lines: vec![
                ProcessDetailLine { text: format!("# {note}"), tone: DetailTone::Neutral },
                ProcessDetailLine { text: format!("{} {file}", tr("已读取笔记", "Read note")), tone: DetailTone::Add },
            ],
        }
    }).collect()
}

pub fn process_card(
    spec: ProcessCardSpec,
    on_toggle_steps: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_toggle_notes: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> AnyElement {
    process_card_with_options(spec, ProcessCardOptions::default(), on_toggle_steps, on_toggle_notes)
}

pub fn process_card_with_options(
    spec: ProcessCardSpec,
    options: ProcessCardOptions,
    on_toggle_steps: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_toggle_notes: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> AnyElement {
    if crate::smoke::is_requested() { SMOKE_SCROLL.with(|scroll| *scroll.borrow_mut() = Some(spec.steps_scroll.clone())); }
    let theme = super::ai_theme::theme();
    let key = &spec.key;
    let status = options.status.unwrap_or(if spec.stage == ProcessStage::Done { TaskStatus::Completed } else { TaskStatus::Running });
    let done = status != TaskStatus::Running;
    let status_probe = match status {
        TaskStatus::Running => "ai-process-status-running", TaskStatus::Completed => "ai-process-status-completed",
        TaskStatus::Failed => "ai-process-status-failed", TaskStatus::Stopped => "ai-process-status-stopped",
        TaskStatus::Unknown => "ai-process-status-unknown",
    };
    let header_symbol = match status {
        TaskStatus::Running => ai_process_status::status_icon_with_pattern(key, status, options.loader),
        TaskStatus::Failed => icon(Icon::Close, px(12.), theme.mono_icon_colors(ai_process_status::diff_red())).into_any_element(),
        TaskStatus::Stopped => icon(Icon::Stop, px(12.), theme.mono_icon_colors(ai_process_status::ink_3())).into_any_element(),
        TaskStatus::Completed | TaskStatus::Unknown => icon(
            if spec.steps_expanded { Icon::TbChevron } else { Icon::TbChevronRight },
            px(12.), theme.mono_icon_colors(ai_process_status::ink_3()),
        ).into_any_element(),
    };
    let badge = div().relative().children(smoke_overlay::probe("ai-process-stage-badge"))
        .children(smoke_overlay::probe(status_probe)).size(px(14.)).flex_none().flex().items_center().justify_center().child(header_symbol);
    let headline = if done {
        div().text_size(px(12.5)).text_color(theme.text_secondary).child(spec.headline.clone()).into_any_element()
    } else {
        let chars: Vec<char> = spec.headline.chars().collect();
        let total = chars.len().max(1);
        let mut text = div().flex().items_center().text_size(px(12.5)).font_weight(FontWeight(500.));
        for (idx, ch) in chars.into_iter().enumerate() {
            let (base, peak) = (theme.text_secondary, theme.text);
            text = text.child(div().child(ch.to_string()).with_animation(
                SharedString::from(format!("{key}-shimmer-{idx}")),
                Animation::new(Duration::from_millis(1400)).repeat(), move |el, delta| {
                    let glow = (1.0 - ((idx as f32 / total as f32) - (delta * 1.4 - 0.2)).abs() * 4.5).clamp(0.0, 1.0);
                    el.text_color(gpui::Hsla { h: base.h, s: base.s * (1.0 - glow), l: base.l + (peak.l - base.l) * glow + 0.18 * glow, a: 0.72 + 0.28 * glow })
                }));
        }
        text.into_any_element()
    };
    let header = div().id(SharedString::from(format!("ai-process-toggle-{key}"))).relative()
        .children(smoke_overlay::probe("ai-process-toggle"))
        .mx(px(-6.)).px(px(6.)).py(px(4.)).rounded(crate::ui::radius::CONTROL)
        .flex().items_center().gap(px(6.)).cursor(CursorStyle::PointingHand).hover(|s| s.bg(theme.hover))
        .on_mouse_down(MouseButton::Left, move |ev, window, cx| { cx.stop_propagation(); on_toggle_steps(ev, window, cx); })
        .child(badge).child(headline)
        .when_some(options.elapsed, |header, elapsed| header.child(div().relative().children(smoke_overlay::probe("ai-process-elapsed")).child(ai_process_status::elapsed(key, elapsed))));
    let mut card = div().id(SharedString::from(format!("ai-process-card-{key}"))).relative()
        .children(smoke_overlay::probe("ai-process-card")).w_full().flex().flex_col().pb(px(2.)).child(div().flex().items_center().child(header));
    if spec.steps_expanded || spec.steps_toggle_seq > 0 {
        let expanded_details = EXPANDED_DETAILS.with(|open| open.borrow().clone());
        let hovered_chip = HOVERED_CHIP.with(|state| state.borrow().clone());
        let mut details = div().relative().mx(px(-4.)).px(px(6.)).pb(px(4.)).flex().flex_col();
        if !spec.steps.is_empty() {
            let mut list = div().id(SharedString::from(format!("ai-process-steps-{key}"))).relative()
                .mt(px(6.)).flex().flex_col().gap(px(4.)).max_h(px(180.)).overflow_y_scroll().track_scroll(&spec.steps_scroll);
            for (index, step) in spec.steps.iter().enumerate() {
                let meta = options.step_metas.get(index);
                let (icon_kind, mono, detail_mono) = (meta.map_or(ProcessStepIcon::Read, |m| m.icon), meta.map_or(true, |m| m.mono), meta.map_or(false, |m| m.detail_mono));
                let lines = meta.filter(|m| !m.lines.is_empty()).map(|m| m.lines.clone()).unwrap_or_else(|| fallback_lines(options.step_details.get(index)));
                let has_detail = !lines.is_empty();
                let is_open = has_detail && expanded_details.contains(&(key.clone(), index));
                let label = if step.merged_count > 1 { format!("{} ×{}", step.label, step.merged_count) } else { step.label.clone() };
                let chip_label = if step.detail.is_empty() { tr("详情", "Details") } else { &step.detail };
                let row_group = SharedString::from(format!("ai-process-row-group-{key}-{index}"));
                let step_status = options.step_statuses.get(index).copied().unwrap_or_else(|| {
                    if step.running { TaskStatus::Running } else if step.detail == tr("失败", "Failed") { TaskStatus::Failed } else { TaskStatus::Completed }
                });
                let mut chip = div().id(SharedString::from(format!("ai-process-tool-{key}-{index}"))).relative()
                    .children(smoke_overlay::probe("ai-process-tool-chip")).children(smoke_overlay::probe(format!("ai-process-tool-chip-{index}")))
                    .flex_1().min_w_0().child(ai_process_status::inline_tool_chip(chip_label, mono));
                if has_detail {
                    let (row_key, on_details_changed) = (key.clone(), options.on_details_changed.clone());
                    chip = chip.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        cx.stop_propagation();
                        EXPANDED_DETAILS.with(|open| { let mut open = open.borrow_mut(); let entry = (row_key.clone(), index); if !open.remove(&entry) { open.insert(entry); } });
                        if let Some(notify) = &on_details_changed { notify(cx); }
                        window.refresh();
                    });
                }
                let mut row = div().id(SharedString::from(format!("ai-process-row-{key}-{index}"))).group(row_group.clone())
                    .mx(px(-3.)).px(px(3.)).h(px(28.)).flex_none().flex().items_center().gap(px(8.)).rounded(crate::ui::radius::CONTROL).hover(|s| s.bg(theme.hover))
                    .child(ai_process_status::row_icon_slot(&format!("{key}-step-{index}"), &row_group, icon_kind, step_status, is_open, has_detail))
                    .child(div().flex_none().text_size(px(12.5)).font_weight(FontWeight(500.)).text_color(theme.text).child(label))
                    .child(chip);
                if has_detail {
                    let (row_key, on_details_changed) = (key.clone(), options.on_details_changed.clone());
                    row = row.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |_, window, cx| {
                        cx.stop_propagation();
                        EXPANDED_DETAILS.with(|open| { let mut open = open.borrow_mut(); let entry = (row_key.clone(), index); if !open.remove(&entry) { open.insert(entry); } });
                        if let Some(notify) = &on_details_changed { notify(cx); }
                        window.refresh();
                    });
                }
                let mut item = div().flex_none().flex().flex_col().child(row);
                if is_open {
                    item = item.child(div().relative().children(smoke_overlay::probe(format!("ai-process-tool-detail-{index}")))
                        .mt(px(2.)).mb(px(4.)).ml(px(8.)).pl(px(14.)).py(px(2.)).border_l_1().border_color(theme.hairline).flex().flex_col().gap(px(2.))
                        .children(lines.into_iter().map(|line| {
                            let color = match line.tone {
                                DetailTone::Add => ai_process_status::diff_green(),
                                DetailTone::Del => ai_process_status::diff_red(),
                                DetailTone::Neutral => theme.text_secondary,
                            };
                            div().w_full().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                                .when(detail_mono, |el| el.font_family(crate::theme::mono_font_family()))
                                .text_size(px(11.5)).line_height(px(18.4)).text_color(color).child(line.text)
                        }))
                        .with_animation(SharedString::from(format!("{key}-detail-open-{index}")),
                            Animation::new(Duration::from_millis(220)).with_easing(crate::ui::motion::ease_out_expo()),
                            |el, delta| el.opacity(delta).relative().top(px(-3.0 * (1.0 - delta)))));
                }
                list = list.child(item.with_animation(SharedString::from(format!("{key}-row-enter-{index}")),
                    Animation::new(Duration::from_millis(260 + (index as u64).min(8) * 35)).with_easing(crate::ui::motion::ease_out_expo()),
                    |el, delta| el.opacity(delta).relative().top(px(4.0 * (1.0 - delta)))));
            }
            details = details.child(div().relative().when(spec.steps_expanded, |d| d.children(smoke_overlay::probe("ai-process-steps"))).child(list));
        }
        let chips = if !options.diff_chips.is_empty() { options.diff_chips } else { fallback_chips(&spec.notes) };
        if !chips.is_empty() {
            let visible_len = if spec.notes_expanded || chips.len() <= 3 { chips.len() } else { 3 };
            let mut chip_row = div().mt(px(10.)).pt(px(10.)).border_t_1().border_color(theme.hairline).w_full().flex().flex_wrap().items_center().gap(px(6.));
            for (chip_idx, chip) in chips[..visible_len].iter().enumerate() {
                let bounds = Rc::new(Cell::new(None));
                let is_hovered = hovered_chip.as_ref() == Some(&(key.clone(), chip_idx));
                let (hover_key, on_hover_notify) = (key.clone(), options.on_details_changed.clone());
                let btn = div().id(SharedString::from(format!("ai-process-diff-chip-{key}-{chip_idx}"))).relative()
                    .child(crate::ui::placement::track_bounds(bounds.clone(), false))
                    .h(px(28.)).max_w_full().px(px(8.)).rounded(px(6.)).border_1().border_color(theme.btn_border).bg(theme.paper)
                    .shadow(vec![BoxShadow { color: rgba(0x0000000d).into(), offset: point(px(0.), px(1.)), blur_radius: px(2.), spread_radius: px(0.), inset: false }])
                    .hover(|s| s.bg(theme.hover)).cursor(CursorStyle::PointingHand).flex().items_center().gap(px(6.))
                    .font_family(crate::theme::mono_font_family()).text_size(px(11.5)).text_color(theme.text)
                    .on_hover(move |hovered, window, cx| {
                        let changed = HOVERED_CHIP.with(|state| {
                            let mut state = state.borrow_mut();
                            if *hovered { let next = Some((hover_key.clone(), chip_idx)); if *state != next { *state = next; true } else { false } }
                            else if *state == Some((hover_key.clone(), chip_idx)) { *state = None; true } else { false }
                        });
                        if changed { if let Some(notify) = &on_hover_notify { notify(cx); } window.refresh(); }
                    })
                    .child(div().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(chip.file.clone()))
                    .when(chip.add > 0, |el| el.child(div().flex_none().text_color(ai_process_status::diff_green()).child(format!("+{}", chip.add))))
                    .when(chip.del > 0, |el| el.child(div().flex_none().text_color(ai_process_status::diff_red()).child(format!("−{}", chip.del))))
                    .when(is_hovered && !chip.lines.is_empty(), |el| {
                        let (preview, bounds) = (chip.clone(), bounds.clone());
                        el.child(crate::ui::placement::smart_popover_with(
                            move |window, _| {
                                let rect = bounds.get().unwrap_or_else(|| gpui::Bounds::new(window.mouse_position(), size(px(0.), px(28.))));
                                crate::ui::placement::PlacementSpec::below(rect).gap(px(6.)).margin(px(12.)).align_x(crate::ui::placement::AlignX::Left)
                            },
                            ai_process_status::diff_preview_popover(&preview), 60,
                        ))
                    })
                    .with_animation(SharedString::from(format!("{key}-diffchip-{chip_idx}-{}", spec.notes_expanded)),
                        Animation::new(Duration::from_millis(240 + (chip_idx as u64).min(6) * 50)).with_easing(crate::ui::motion::ease_out_expo()),
                        |el, delta| el.opacity(delta).relative().top(px(3.0 * (1.0 - delta))));
                chip_row = chip_row.child(btn);
            }
            if chips.len() > 3 {
                let expanded = spec.notes_expanded;
                let more_label = if expanded { tr("收起", "Show less").to_owned() }
                    else if is_en() { format!("+{} more", chips.len() - 3) }
                    else { format!("+{} 更多", chips.len() - 3) };
                chip_row = chip_row.child(div()
                    .id(SharedString::from(format!("ai-process-notes-{}-{key}", if expanded { "less" } else { "more" })))
                    .relative().when(spec.steps_expanded, |d| d.children(smoke_overlay::probe(if expanded { "ai-process-notes-less" } else { "ai-process-notes-more" })))
                    .h(px(28.)).px(px(6.)).rounded(px(6.)).flex().items_center()
                    .font_family(crate::theme::mono_font_family()).text_size(px(11.5)).text_color(ai_process_status::ink_3())
                    .hover(|s| s.text_color(theme.text_secondary).underline()).cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, move |ev, window, cx| { cx.stop_propagation(); on_toggle_notes(ev, window, cx); })
                    .child(more_label));
            }
            details = details.child(chip_row);
        }
        let expanded = spec.steps_expanded;
        let measured = DETAIL_HEIGHTS.with(|heights| heights.borrow().get(key).copied().unwrap_or(px(0.)));
        if expanded {
            let measure_key = key.clone();
            details = details.child(canvas(|_, _, _| (), move |bounds, _, _, _| {
                DETAIL_HEIGHTS.with(|heights| { heights.borrow_mut().insert(measure_key.clone(), bounds.size.height); });
            }).absolute().size_full());
        }
        card = card.child(details.overflow_hidden().with_animation(
            SharedString::from(format!("{key}-steps-{}", spec.steps_toggle_seq)),
            Animation::new(Duration::from_millis(320)).with_easing(crate::ui::motion::ease_out_expo()),
            move |el, delta| if expanded { el.opacity(delta).relative().top(px(-4.0 * (1.0 - delta))) }
                else { el.h(measured * (1.0 - delta)).opacity(1.0 - delta).relative().top(px(-4.0 * delta)) },
        ));
    }
    card.into_any_element()
}
