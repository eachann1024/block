use std::{cell::RefCell, collections::{HashMap, HashSet}, rc::Rc, time::Duration};
use gpui::{Animation, AnimationExt, AnyElement, App, CursorStyle, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, ScrollHandle, SharedString, Window, canvas, div, prelude::*, px};
use crate::{theme::tr, ui::smoke_overlay};
use super::ai_process_status::{self, LoaderPattern, ProcessElapsed, TaskStatus};

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
    pub on_details_changed: Option<Rc<dyn Fn(&mut App)>>,
}

thread_local! {
    static EXPANDED_DETAILS: RefCell<HashSet<(String, usize)>> = RefCell::new(HashSet::new());
    static DETAIL_HEIGHTS: RefCell<HashMap<String, gpui::Pixels>> = RefCell::new(HashMap::new());
    static SMOKE_SCROLL: RefCell<Option<ScrollHandle>> = const { RefCell::new(None) };
}

pub(crate) fn smoke_expanded_details() -> Vec<(String, usize)> {
    EXPANDED_DETAILS.with(|open| open.borrow().iter().cloned().collect())
}

pub(crate) fn smoke_scroll_offset() -> Option<gpui::Point<gpui::Pixels>> {
    SMOKE_SCROLL.with(|scroll| scroll.borrow().as_ref().map(ScrollHandle::offset))
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
    let symbol = ai_process_status::status_icon_with_pattern(key, status, options.loader);
    let status_probe = match status { TaskStatus::Running => "ai-process-status-running", TaskStatus::Completed => "ai-process-status-completed", TaskStatus::Failed => "ai-process-status-failed", TaskStatus::Stopped => "ai-process-status-stopped", TaskStatus::Unknown => "ai-process-status-unknown" };
    let badge = div().relative().children(smoke_overlay::probe("ai-process-stage-badge"))
        .children(smoke_overlay::probe(status_probe))
        .w(px(16.)).flex_none().flex().items_center().justify_center().child(symbol);
    let headline = if done {
        div().text_size(px(13.)).text_color(theme.text_secondary).child(spec.headline.clone()).into_any_element()
    } else {
        let chars: Vec<char> = spec.headline.chars().collect();
        let total = chars.len().max(1);
        let mut text = div().flex().items_center().text_size(px(13.)).font_weight(FontWeight(500.));
        for (idx, ch) in chars.into_iter().enumerate() {
            let base = theme.text_secondary;
            let peak = theme.text;
            text = text.child(div().child(ch.to_string()).with_animation(
                SharedString::from(format!("{key}-shimmer-{idx}")),
                Animation::new(Duration::from_millis(1400)).repeat(), move |el, delta| {
                    let pos = idx as f32 / total as f32;
                    let center = delta * 1.4 - 0.2;
                    let glow = (1.0 - (pos - center).abs() * 4.5).clamp(0.0, 1.0);
                    el.text_color(gpui::Hsla { h: base.h, s: base.s * (1.0 - glow),
                        l: base.l + (peak.l - base.l) * glow + 0.18 * glow, a: 0.72 + 0.28 * glow })
                }));
        }
        text.into_any_element()
    };
    let header = div().id(SharedString::from(format!("ai-process-toggle-{key}"))).relative()
        .children(smoke_overlay::probe("ai-process-toggle")).flex().items_center().gap(px(6.))
        .cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, move |ev, window, cx| {
            cx.stop_propagation(); on_toggle_steps(ev, window, cx);
        }).child(badge).child(headline)
        .when_some(options.elapsed, |header, elapsed| header.child(div().relative()
            .children(smoke_overlay::probe("ai-process-elapsed")).child(ai_process_status::elapsed(key, elapsed))))
        .child(crate::icons::icon(if spec.steps_expanded { crate::icons::Icon::ChevronUp } else { crate::icons::Icon::ChevronDown }, px(13.), theme.mono_icon_colors(theme.text_secondary)));
    let mut card = div().id(SharedString::from(format!("ai-process-card-{key}"))).relative()
        .children(smoke_overlay::probe("ai-process-card")).w_full().flex().flex_col().gap(px(4.)).child(header);
    if spec.steps_expanded || spec.steps_toggle_seq > 0 {
        let expanded_details = EXPANDED_DETAILS.with(|open| open.borrow().clone());
        let mut details = div().relative().ml(px(7.)).pl(px(14.)).border_l_1().border_color(theme.hairline).flex().flex_col().gap(px(4.));
        if !spec.steps.is_empty() {
            let mut list = div().id(SharedString::from(format!("ai-process-steps-{key}"))).relative()
                .flex().flex_col().max_h(px(112.)).overflow_y_scroll().track_scroll(&spec.steps_scroll);
            for (index, step) in spec.steps.iter().enumerate() {
                let label = if step.merged_count > 1 {
                    format!("{} (×{})", step.label, step.merged_count)
                } else { step.label.clone() };
                let detail = options.step_details.get(index).filter(|detail| !detail.trim().is_empty());
                let is_open = detail.is_some() && expanded_details.contains(&(key.clone(), index));
                let chip_label = if step.detail.is_empty() { tr("详情", "Details") } else { &step.detail };
                let mut chip = div().id(SharedString::from(format!("ai-process-tool-{key}-{index}"))).relative().children(smoke_overlay::probe("ai-process-tool-chip"))
                    .children(smoke_overlay::probe(format!("ai-process-tool-chip-{index}")))
                    .child(ai_process_status::tool_chip(chip_label));
                if detail.is_some() {
                    let row_key = key.clone();
                    let on_details_changed = options.on_details_changed.clone();
                    chip = chip.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |_, window, cx| {
                            cx.stop_propagation();
                            EXPANDED_DETAILS.with(|open| {
                                let mut open = open.borrow_mut();
                                let entry = (row_key.clone(), index);
                                if !open.remove(&entry) { open.insert(entry); }
                            });
                            if let Some(notify) = &on_details_changed { notify(cx); }
                            window.refresh();
                        });
                }
                let row = div().h(px(28.)).flex_none().flex().items_center().gap(px(8.))
                    .child(ai_process_status::status_icon(&format!("{key}-step-{index}"), options.step_statuses.get(index).copied().unwrap_or_else(|| if step.running { TaskStatus::Running } else if step.detail == tr("失败", "Failed") { TaskStatus::Failed } else { TaskStatus::Completed })))
                    .child(div().min_w_0().text_size(px(12.5)).text_color(theme.text).child(label))
                    .when(!step.detail.is_empty() || detail.is_some(), |row| row.child(chip));
                let mut item = div().flex_none().flex().flex_col().child(row);
                if let Some(detail) = detail.filter(|_| is_open) {
                    item = item.child(div().relative().children(smoke_overlay::probe(format!("ai-process-tool-detail-{index}")))
                        .min_h(px(28.)).ml(px(7.)).pl(px(14.)).border_l_1().border_color(theme.hairline)
                        .text_size(px(11.5))
                        .text_color(theme.text_secondary).child(detail.clone()));
                }
                list = list.child(item);
            }
            details = details.child(div().relative().when(spec.steps_expanded, |d| d.children(smoke_overlay::probe("ai-process-steps"))).child(list));
        }
        if !spec.notes.is_empty() {
            let text = if !spec.notes_expanded && spec.notes.len() > 3 {
                format!("{}  {} / ...", tr("笔记", "Notes"), spec.notes[..3].join(" / "))
            } else { format!("{}  {}", tr("笔记", "Notes"), spec.notes.join(" / ")) };
            let notes = div().mt(px(4.)).text_size(px(14.)).text_color(theme.text_secondary).child(text);
            let notes = if spec.notes.len() > 3 {
                let expanded = spec.notes_expanded;
                notes.id(SharedString::from(format!("ai-process-notes-{}-{key}", if expanded { "less" } else { "more" })))
                    .relative().when(spec.steps_expanded, |d| d.children(smoke_overlay::probe(if expanded { "ai-process-notes-less" } else { "ai-process-notes-more" })))
                    .cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |ev, window, cx| {
                        cx.stop_propagation(); on_toggle_notes(ev, window, cx);
                    }).into_any_element()
            } else { notes.into_any_element() };
            details = details.child(div().child(notes).with_animation(
                SharedString::from(format!("{key}-notes-{}", spec.notes_expanded)),
                Animation::new(Duration::from_millis(150)).with_easing(crate::ui::motion::ease_out_expo()),
                |el, delta| el.opacity(delta).relative().top(px(-3.0 * (1.0 - delta)))));
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
            Animation::new(Duration::from_millis(400)).with_easing(crate::ui::motion::ease_out_expo()),
            move |el, delta| if expanded {
                el.opacity(delta).relative().top(px(-6.0 * (1.0 - delta)))
            } else {
                el.h(measured * (1.0 - delta)).opacity(1.0 - delta).relative().top(px(-6.0 * delta))
            }));
    }
    card.into_any_element()
}
