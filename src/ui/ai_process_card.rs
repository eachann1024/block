use std::time::Duration;
use gpui::{Animation, AnimationExt, AnyElement, App, CursorStyle, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, ScrollHandle, SharedString, Window, div, prelude::*, px};
use crate::{theme::{Theme, tr}, ui::smoke_overlay};

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

pub fn process_card(
    spec: ProcessCardSpec,
    on_toggle_steps: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_toggle_notes: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> AnyElement {
    let theme = Theme::ocean();
    let key = &spec.key;
    let done = spec.stage == ProcessStage::Done;
    let symbol = div().text_size(px(14.)).text_color(theme.text_secondary)
        .child(if done { "✓" } else { "◌" });
    let symbol = if done { symbol.into_any_element() } else {
        symbol.with_animation(SharedString::from(format!("{key}-star")),
            Animation::new(Duration::from_millis(1400)).repeat().with_easing(gpui::pulsating_between(0.45, 1.0)),
            |el, delta| el.opacity(delta)).into_any_element()
    };
    let badge = div().relative().children(smoke_overlay::probe("ai-process-stage-badge"))
        .w(px(16.)).flex_none().flex().items_center().justify_center().child(symbol);
    let headline = if done {
        div().text_size(px(14.)).text_color(theme.text_secondary).child(spec.headline.clone()).into_any_element()
    } else {
        let chars: Vec<char> = spec.headline.chars().collect();
        let total = chars.len().max(1);
        let mut text = div().flex().items_center().text_size(px(14.)).font_weight(FontWeight(500.));
        for (idx, ch) in chars.into_iter().enumerate() {
            let base = theme.text_secondary;
            let peak = theme.text;
            text = text.child(div().child(ch.to_string()).with_animation(
                SharedString::from(format!("{key}-shimmer-{idx}")),
                Animation::new(Duration::from_millis(1800)).repeat(), move |el, delta| {
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
        }).child(badge).child(headline);
    let mut card = div().id(SharedString::from(format!("ai-process-card-{key}"))).relative()
        .children(smoke_overlay::probe("ai-process-card")).w_full().flex().flex_col().gap(px(4.)).child(header);
    if spec.steps_expanded || spec.steps_toggle_seq > 0 {
        let steps_h = (spec.steps.len().min(4) as f32) * 28.0;
        let notes_h = if spec.notes.is_empty() { 0.0 } else { 28.0 };
        let full_h = steps_h + notes_h;
        let mut details = div().ml(px(22.)).flex().flex_col().gap(px(4.));
        if !spec.steps.is_empty() {
            let mut list = div().id(SharedString::from(format!("ai-process-steps-{key}"))).relative()
                .when(spec.steps_expanded, |d| d.children(smoke_overlay::probe("ai-process-steps"))).flex().flex_col()
                .when(spec.steps.len() > 4, |d| d.h(px(112.)).overflow_y_scroll().track_scroll(&spec.steps_scroll));
            for step in &spec.steps {
                let label = if step.merged_count > 1 {
                    format!("{} (×{})", step.label, step.merged_count)
                } else { step.label.clone() };
                list = list.child(div().h(px(28.)).flex_none().flex().items_center().gap(px(10.))
                    .child(div().w(px(12.)).flex_none().text_size(px(14.)).text_color(theme.text_secondary)
                        .child(if step.running { "›" } else { "✓" }))
                    .child(div().min_w_0().text_size(px(14.)).text_color(theme.text_secondary).child(label)));
            }
            details = details.child(list);
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
        card = card.child(details.overflow_hidden().with_animation(
            SharedString::from(format!("{key}-steps-{}", spec.steps_toggle_seq)),
            Animation::new(Duration::from_millis(180)).with_easing(crate::ui::motion::ease_out_expo()),
            move |el, delta| if expanded {
                el.h(px(full_h * delta)).opacity(delta).relative().top(px(-6.0 * (1.0 - delta)))
            } else {
                el.h(px(full_h * (1.0 - delta))).opacity(1.0 - delta).relative().top(px(-6.0 * delta))
            }));
    }
    card.into_any_element()
}
