use std::time::{Duration, Instant};
use gpui::{
    Animation, AnimationExt, AnyElement, FontWeight, Hsla, IntoElement, SharedString, div,
    prelude::*, px, rgb, rgba,
};
use crate::{icons::{Icon, icon}, theme::is_dark};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TaskStatus { Running, Completed, Failed, Stopped, Unknown }

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum LoaderPattern { Drive, #[default] Orbit }

#[derive(Clone, Copy, Debug)]
pub enum ProcessElapsed { Running(Instant), Finished(Duration) }

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum ProcessStepIcon { #[default] Think, Write, Run, Read }

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum ThinkingVariant { #[default] Steps, Reasoning, Search, Coding }

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum DetailTone { #[default] Neutral, Add, Del }

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ProcessDetailLine { pub text: String, pub tone: DetailTone }

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ProcessStepMeta {
    pub icon: ProcessStepIcon,
    pub variant: ThinkingVariant,
    pub mono: bool,
    pub detail_mono: bool,
    pub add: usize,
    pub del: usize,
    pub lines: Vec<ProcessDetailLine>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ProcessDiffChip {
    pub file: String,
    pub add: usize,
    pub del: usize,
    pub lines: Vec<ProcessDetailLine>,
}

impl ProcessElapsed {
    fn duration(self) -> Duration {
        match self { Self::Running(start) => start.elapsed(), Self::Finished(duration) => duration }
    }
}

pub fn ink_3() -> Hsla {
    let mut color = super::ai_theme::theme().text_secondary;
    color.a = 0.78;
    color
}
pub fn diff_green() -> Hsla { if is_dark() { rgb(0x4ade80).into() } else { rgb(0x16a34a).into() } }
pub fn diff_red() -> Hsla { if is_dark() { rgb(0xf87171).into() } else { rgb(0xdc2626).into() } }
pub fn green_tint() -> Hsla { if is_dark() { rgba(0x4ade8022).into() } else { rgba(0x16a34a16).into() } }
pub fn red_tint() -> Hsla { if is_dark() { rgba(0xf8717122).into() } else { rgba(0xdc262616).into() } }
pub fn field_bg() -> Hsla { if is_dark() { rgba(0xffffff09).into() } else { rgba(0x00000007).into() } }
pub fn field_border() -> Hsla { if is_dark() { rgba(0xffffff12).into() } else { rgba(0x0000000c).into() } }
pub fn row_hover_bg() -> Hsla { if is_dark() { rgba(0xffffff0d).into() } else { rgba(0x0000000a).into() } }

fn elapsed_label(duration: Duration) -> String {
    let tenths = duration.as_millis() / 100;
    if tenths < 600 { format!("{}.{:01}s", tenths / 10, tenths % 10) }
    else { format!("{}m {}.{:01}s", tenths / 600, tenths % 600 / 10, tenths % 10) }
}

pub fn elapsed(key: &str, value: ProcessElapsed) -> AnyElement {
    let text = div().font_family(crate::theme::mono_font_family()).text_size(px(12.)).text_color(ink_3());
    match value {
        ProcessElapsed::Finished(_) => text.child(elapsed_label(value.duration())).into_any_element(),
        ProcessElapsed::Running(_) => text.with_animation(
            SharedString::from(format!("{key}-elapsed")), Animation::new(Duration::from_millis(100)).repeat(),
            move |element, _| element.child(elapsed_label(value.duration())),
        ).into_any_element(),
    }
}

pub fn status_icon_with_pattern(key: &str, status: TaskStatus, pattern: LoaderPattern) -> AnyElement {
    let theme = super::ai_theme::theme();
    if status == TaskStatus::Unknown {
        return div().size(px(14.)).flex().items_center().justify_center()
            .child(div().size(px(4.)).rounded_full().bg(theme.text_secondary)).into_any_element();
    }
    if status != TaskStatus::Running {
        let color = match status { TaskStatus::Failed => diff_red(), _ => ink_3() };
        let glyph = match status {
            TaskStatus::Completed => Icon::TbChevron,
            TaskStatus::Failed => Icon::Close,
            TaskStatus::Stopped => Icon::Stop,
            _ => unreachable!(),
        };
        return icon(glyph, px(12.), theme.mono_icon_colors(color)).into_any_element();
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

fn step_glyph(kind: ProcessStepIcon, color: Hsla) -> AnyElement {
    let theme = super::ai_theme::theme();
    let glyph = match kind {
        ProcessStepIcon::Think => Icon::ToolThink,
        ProcessStepIcon::Write => Icon::ToolWrite,
        ProcessStepIcon::Run => Icon::ToolRun,
        ProcessStepIcon::Read => Icon::ToolRead,
    };
    icon(glyph, px(13.), theme.mono_icon_colors(color)).into_any_element()
}

pub fn row_icon_slot(
    row_group: &SharedString, kind: ProcessStepIcon,
    is_open: bool, expandable: bool,
) -> AnyElement {
    let theme = super::ai_theme::theme();
    let color = ink_3();
    if is_open {
        return div().size(px(16.)).flex_none().flex().items_center().justify_center()
            .child(icon(Icon::TbChevron, px(12.), theme.mono_icon_colors(color))).into_any_element();
    }
    let base = div().size(px(16.)).flex().items_center().justify_center()
        .when(expandable, |el| el.group_hover(row_group.clone(), |s| s.invisible()))
        .child(step_glyph(kind, color));
    if !expandable { return base.flex_none().into_any_element(); }
    div().relative().size(px(16.)).flex_none().flex().items_center().justify_center()
        .child(base)
        .child(div().absolute().top_0().left_0().size(px(16.)).flex().items_center().justify_center()
            .invisible().group_hover(row_group.clone(), |s| s.visible())
            .child(icon(Icon::TbChevronRight, px(12.), theme.mono_icon_colors(color))))
        .into_any_element()
}

pub fn inline_tool_chip(detail: &str, mono: bool) -> AnyElement {
    let theme = super::ai_theme::theme();
    div().w_full().min_w_0().px(px(6.)).h(px(22.)).rounded(px(6.))
        .border_1().border_color(field_border()).bg(field_bg()).hover(|s| s.bg(row_hover_bg()))
        .flex().items_center().overflow_hidden()
        .when(mono, |el| el.font_family(crate::theme::mono_font_family()))
        .text_size(px(11.5)).font_weight(FontWeight(400.)).text_color(theme.text_secondary)
        .child(div().w_full().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().child(detail.to_owned()))
        .into_any_element()
}

pub fn diff_preview_popover(chip: &ProcessDiffChip) -> AnyElement {
    let (theme, green, red) = (super::ai_theme::theme(), diff_green(), diff_red());
    div().w(px(288.)).overflow_hidden().rounded(px(10.)).border_1().border_color(theme.btn_border).bg(theme.paper).shadow_lg().flex().flex_col()
        .child(div().flex().items_center().justify_between().gap(px(8.)).border_b_1().border_color(theme.hairline).px(px(10.)).py(px(6.))
            .font_family(crate::theme::mono_font_family()).text_size(px(11.))
            .child(div().min_w_0().flex_1().overflow_hidden().whitespace_nowrap().text_ellipsis().text_color(theme.text_secondary).child(chip.file.clone()))
            .child(div().flex_none().flex().items_center().gap(px(4.))
                .when(chip.add > 0, |el| el.child(div().text_color(green).child(format!("+{}", chip.add))))
                .when(chip.del > 0, |el| el.child(div().text_color(red).child(format!("−{}", chip.del))))))
        .child(div().py(px(4.)).font_family(crate::theme::mono_font_family()).text_size(px(11.)).line_height(px(19.8)).flex().flex_col()
            .children(chip.lines.iter().map(|line| {
                let (bg, fg, sign) = match line.tone {
                    DetailTone::Add => (green_tint(), green, "+"),
                    DetailTone::Del => (red_tint(), red, "−"),
                    DetailTone::Neutral => (gpui::transparent_black(), theme.text_secondary, " "),
                };
                div().flex().items_center().gap(px(8.)).px(px(10.)).bg(bg).text_color(fg)
                    .child(div().w(px(12.)).flex_none().child(sign))
                    .child(div().min_w_0().flex_1().overflow_hidden().whitespace_nowrap().text_ellipsis().child(line.text.clone()))
            })))
        .into_any_element()
}
