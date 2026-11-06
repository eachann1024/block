use std::{rc::Rc, time::Duration};
use gpui::{Animation, AnimationExt, AnyElement, App, CursorStyle, FontWeight, IntoElement,
    MouseButton, MouseDownEvent, ScrollHandle, SharedString, Window, div, prelude::*, px};
use crate::{icons::{Icon, icon}, theme::{Theme, is_en, tr}, ui::{motion, radius, smoke_overlay}};

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
    pub notes_expanded: bool,
    pub steps_scroll: ScrollHandle,
}

fn badge(stage: ProcessStage, key: &str) -> AnyElement {
    let theme = Theme::ocean();
    let content = if stage == ProcessStage::Done {
        icon(Icon::Check, px(15.), theme.mono_icon_colors(theme.accent)).into_any_element()
    } else {
        let duration = match stage {
            ProcessStage::Skill => 1200,
            ProcessStage::Scan => 950,
            ProcessStage::Read | ProcessStage::Write => 1050,
            ProcessStage::Synth => 850,
            ProcessStage::Done => unreachable!(),
        };
        div().relative().size(px(24.)).flex().items_center().justify_center()
            .child(div().absolute().rounded(px(7.)).border_1().border_color(theme.accent)
                .with_animation(SharedString::from(format!("{key}-ring-{stage:?}")),
                    Animation::new(Duration::from_millis(duration)).repeat().with_easing(gpui::pulsating_between(0., 1.)),
                    |el, delta| el.size(px(16. + delta * 6.)).opacity(0.35 + (1. - delta) * 0.65)))
            .child(div().text_size(px(15.)).line_height(px(15.)).text_color(theme.accent).child("✦")
                .with_animation(SharedString::from(format!("{key}-star-{stage:?}")),
                    Animation::new(Duration::from_millis(duration)).repeat().with_easing(gpui::pulsating_between(0., 1.)),
                    |el, delta| el.opacity(0.65 + delta * 0.35)))
            .into_any_element()
    };
    div().relative().children(smoke_overlay::probe("ai-process-stage-badge"))
        .size(px(32.)).flex_none().rounded(radius::CONTROL).bg(theme.selected).border_1().border_color(theme.accent)
        .flex().items_center().justify_center().child(content).into_any_element()
}

pub fn process_card(
    spec: ProcessCardSpec,
    on_toggle_steps: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
    on_toggle_notes: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> AnyElement {
    let theme = Theme::ocean();
    let key = &spec.key;
    let mut activity = div().flex().flex_none().items_center().gap(px(3.)).px(px(5.)).py(px(4.)).rounded_full().bg(theme.hover);
    for index in 0..3 {
        activity = activity.child(div().size(px(4.)).rounded_full().bg(theme.accent)
            .with_animation(SharedString::from(format!("{key}-activity-{index}")), Animation::new(Duration::from_millis(1200)).repeat(), move |el, delta| {
                let phase = ((delta + index as f32 / 3.) * std::f32::consts::TAU).sin() * 0.5 + 0.5;
                el.opacity(0.25 + phase * 0.75)
            }));
    }
    let header = div().px(px(14.)).py(px(11.)).flex().items_center().justify_between().gap(px(10.))
        .child(div().flex().items_center().gap(px(10.)).min_w_0().flex_1()
            .child(badge(spec.stage, key))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(14.)).font_weight(FontWeight(600.)).text_color(theme.text).child(spec.headline))
            .when(spec.stage != ProcessStage::Done, |header| header.child(activity)))
        .when(!spec.steps.is_empty(), |header| header.child(div().id(SharedString::from(format!("ai-process-toggle-{key}")))
            .relative().children(smoke_overlay::probe("ai-process-toggle")).px(px(10.)).py(px(4.))
            .rounded(radius::CONTROL).bg(theme.hover).border_1().border_color(theme.btn_border)
            .text_size(px(14.)).text_color(theme.text).cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.selected))
            .on_mouse_down(MouseButton::Left, move |ev, window, cx| { cx.stop_propagation(); on_toggle_steps(ev, window, cx); })
            .child(if spec.steps_expanded { tr("收起过程", "Hide steps") } else { tr("展开过程", "Show steps") })));
    let mut card = div().id(SharedString::from(format!("ai-process-card-{key}"))).relative()
        .children(smoke_overlay::probe("ai-process-card")).w_full().flex().flex_col().rounded(radius::SURFACE)
        .border_1().border_color(theme.btn_border).bg(theme.shell).overflow_hidden().child(header);
    if !spec.notes.is_empty() {
        let mut notes = div().px(px(14.)).py(px(9.)).border_t_1().border_color(theme.hairline).flex().items_center().gap(px(6.)).flex_wrap()
            .child(div().text_size(px(14.)).text_color(theme.text_secondary).child(tr("用到笔记", "Notes")));
        let visible = if spec.notes_expanded { spec.notes.len() } else { spec.notes.len().min(3) };
        for note in spec.notes.iter().take(visible) {
            notes = notes.child(div().px(px(8.)).py(px(2.)).rounded(radius::MICRO_MD).bg(theme.paper).border_1()
                .border_color(theme.btn_border).text_size(px(14.)).text_color(theme.text).child(note.clone()));
        }
        if spec.notes.len() > 3 {
            let expanded = spec.notes_expanded;
            let callback = Rc::new(on_toggle_notes);
            notes = notes.child(div().id(SharedString::from(format!("ai-process-notes-{}-{key}", if expanded { "less" } else { "more" })))
                .relative().children(smoke_overlay::probe(if expanded { "ai-process-notes-less" } else { "ai-process-notes-more" }))
                .px(px(if expanded { 9. } else { 10. })).py(px(2.)).rounded(radius::MICRO_MD)
                .bg(if expanded { theme.hover } else { theme.selected }).border_1().border_color(if expanded { theme.btn_border } else { theme.accent })
                .text_size(px(14.)).font_weight(FontWeight(if expanded { 400. } else { 600. }))
                .text_color(if expanded { theme.text_secondary } else { theme.accent }).cursor(CursorStyle::PointingHand)
                .on_mouse_down(MouseButton::Left, move |ev, window, cx| { cx.stop_propagation(); callback(ev, window, cx); })
                .child(if expanded { tr("收起", "Less") } else { "..." }));
        }
        card = card.child(notes);
    }
    if spec.steps_expanded && !spec.steps.is_empty() {
        let mut list = div().id(SharedString::from(format!("ai-process-steps-{key}"))).relative()
            .children(smoke_overlay::probe("ai-process-steps")).px(px(12.)).py(px(10.)).border_t_1().border_color(theme.hairline)
            .rounded_b(radius::SURFACE).bg(theme.paper).flex().flex_col().gap(px(6.))
            .when(spec.steps.len() > 4, |d| d.h(px(166.)).overflow_y_scroll().track_scroll(&spec.steps_scroll));
        for (index, step) in spec.steps.iter().enumerate() {
            let dot = div().size(px(7.)).flex_none().rounded(px(99.)).bg(theme.accent);
            let dot = if step.running { dot.with_animation(SharedString::from(format!("{key}-step-{index}")),
                Animation::new(Duration::from_millis(900)).repeat().with_easing(gpui::pulsating_between(0.35, 1.)), |el, delta| el.opacity(delta)).into_any_element() } else { dot.into_any_element() };
            let left = div().flex().items_center().gap(px(8.)).min_w_0().flex_1().child(dot)
                .child(div().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_size(px(14.)).text_color(theme.text).child(step.label.clone()))
                .when(step.merged_count > 1, |d| d.child(div().flex_none().px(px(7.)).py(px(1.)).rounded(px(99.)).bg(theme.selected)
                    .text_size(px(14.)).text_color(theme.accent).child(if is_en() { format!("×{}", step.merged_count) } else { format!("合并 {} 次调用", step.merged_count) })));
            list = list.child(div().h(px(32.)).flex_none().px(px(10.)).rounded(radius::CONTROL).flex().items_center().justify_between().gap(px(8.))
                .when(step.running, |row| row.bg(theme.selected)).child(left)
                .when(!step.detail.is_empty(), |row| row.child(div().flex_none().text_size(px(14.))
                    .text_color(if step.running { theme.accent } else { theme.text_secondary }).child(step.detail.clone()))));
        }
        card = card.child(motion::reveal_fade(list, SharedString::from(format!("{key}-steps-reveal"))));
    }
    card.into_any_element()
}
