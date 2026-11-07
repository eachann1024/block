use std::time::{Duration, Instant};

use gpui::{Animation, AnimationExt, AnyElement, FontWeight, IntoElement, SharedString, div, prelude::*, px};

use crate::icons::{Icon, icon};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TaskStatus { Running, Completed, Failed, Stopped, Unknown }

#[derive(Clone, Copy, Debug, Default)]
pub enum LoaderPattern { #[default] Drive, Orbit }

#[derive(Clone, Copy, Debug)]
pub enum ProcessElapsed { Running(Instant), Finished(Duration) }

impl ProcessElapsed {
    fn duration(self) -> Duration {
        match self { Self::Running(start) => start.elapsed(), Self::Finished(duration) => duration }
    }
}

fn elapsed_label(duration: Duration) -> String {
    let tenths = duration.as_millis() / 100;
    if tenths < 600 { format!("{}.{:01}s", tenths / 10, tenths % 10) }
    else { format!("{}m {}.{:01}s", tenths / 600, tenths % 600 / 10, tenths % 10) }
}

pub fn elapsed(key: &str, value: ProcessElapsed) -> AnyElement {
    let theme = super::ai_theme::theme();
    let text = div().font_family(crate::theme::mono_font_family()).text_size(px(12.)).text_color(theme.text_secondary);
    match value {
        ProcessElapsed::Finished(_) => text.child(elapsed_label(value.duration())).into_any_element(),
        ProcessElapsed::Running(_) => text.with_animation(
            SharedString::from(format!("{key}-elapsed")), Animation::new(Duration::from_millis(100)).repeat(),
            move |element, _| element.child(elapsed_label(value.duration())),
        ).into_any_element(),
    }
}

pub fn status_icon(key: &str, status: TaskStatus) -> AnyElement {
    status_icon_with_pattern(key, status, LoaderPattern::Drive)
}

pub fn status_icon_with_pattern(key: &str, status: TaskStatus, pattern: LoaderPattern) -> AnyElement {
    let theme = super::ai_theme::theme();
    if status == TaskStatus::Unknown {
        return div().size(px(14.)).flex().items_center().justify_center()
            .child(div().size(px(4.)).rounded_full().bg(theme.text_secondary)).into_any_element();
    }
    if status != TaskStatus::Running {
        let glyph = match status { TaskStatus::Completed => Icon::Sparkles, TaskStatus::Failed => Icon::Close,
            TaskStatus::Stopped => Icon::Stop, TaskStatus::Running | TaskStatus::Unknown => unreachable!() };
        return icon(glyph, px(14.), theme.mono_icon_colors(theme.text_secondary)).into_any_element();
    }
    let mut grid = div().w(px(15.)).h(px(15.)).flex().flex_col().gap(px(1.5));
    for row in 0..3 {
        let mut line = div().flex().gap(px(1.5));
        for col in 0..3 {
            let index = row * 3 + col;
            let orbit_order = [0, 1, 2, 5, 8, 7, 6, 3];
            let delay = match pattern {
                LoaderPattern::Drive => Some((col + (row as i32 - 1).unsigned_abs() as usize) as f32 * 90. / 650.),
                LoaderPattern::Orbit => orbit_order.iter().position(|cell| *cell == index).map(|position| position as f32 * 110. / 950.),
            };
            let Some(delay) = delay else {
                line = line.child(div().size(px(4.)).rounded(px(1.)).bg(theme.text).opacity(0.07));
                continue;
            };
            let duration = match pattern { LoaderPattern::Drive => 650, LoaderPattern::Orbit => 950 };
            line = line.child(div().size(px(4.)).rounded(px(1.)).bg(theme.text).with_animation(
                SharedString::from(format!("{key}-pixel-{row}-{col}")),
                Animation::new(Duration::from_millis(duration)).repeat(), move |cell, delta| {
                    let phase = (delta - delay).rem_euclid(1.);
                    let light = if phase < 0.18 { phase / 0.18 } else if phase < 0.42 { 1. }
                        else if phase < 0.62 { (0.62 - phase) / 0.20 } else { 0. };
                    let eased = light * light * (3. - 2. * light);
                    cell.opacity(0.15 + 0.85 * eased)
                }));
        }
        grid = grid.child(line);
    }
    grid.into_any_element()
}

pub fn tool_chip(detail: &str) -> AnyElement {
    let theme = super::ai_theme::theme();
    div().min_w_0().px(px(6.)).h(px(22.)).rounded(px(5.))
        .border_1().border_color(theme.hairline).bg(theme.hover).flex().items_center()
        .text_size(px(11.5)).font_weight(FontWeight(400.)).text_color(theme.text_secondary)
        .child(detail.to_owned()).into_any_element()
}
