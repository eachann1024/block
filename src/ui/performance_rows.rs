use std::{sync::OnceLock, time::Duration};
use gpui::{
    Animation, AnimationExt, AnyElement, App, CursorStyle, Div, Font, FontFeatures, FontStyle,
    FontWeight, IntoElement, MouseButton, MouseDownEvent, ParentElement, Pixels, SharedString,
    Stateful, Styled, TextRun, Window, div, linear_color_stop, linear_gradient, prelude::*, px,
    relative,
};
use crate::{
    theme::{Theme, is_dark, is_en, tr},
    ui::{annotation_card::spinner, fade, radius, smoke_overlay},
};

/// OpenType `tnum`: every digit has the same advance, so 142.3 and 188.8 occupy the same width.
pub fn tabular_nums() -> FontFeatures {
    static FEATURES: OnceLock<FontFeatures> = OnceLock::new();
    FEATURES.get_or_init(|| FontFeatures(std::sync::Arc::new(vec![("tnum".into(), 1)]))).clone()
}

/// Widest live readouts. Slots stay this wide so a longer sample cannot push its neighbors.
#[derive(Clone, Copy)]
pub struct ReadoutWidth {
    pub bytes: Pixels,
    pub cpu: Pixels,
    pub threads: Pixels,
    pub time: Pixels,
    pub percent: Pixels,
}

pub fn readout_widths(window: &Window) -> ReadoutWidth {
    let strong = FontWeight(560.);
    let time = if is_en() { "2026-11-08 20:42:15" } else { "2026年11月08日 20:42:15" };
    ReadoutWidth {
        bytes: readout_width(window, &["999.9 KiB", "999.9 MiB", "999.9 GiB", "999.9 TiB", "999 B"], px(13.), strong),
        cpu: readout_width(window, &["999.9%"], px(13.), strong),
        threads: readout_width(window, &["9999"], px(13.), strong),
        time: readout_width(window, &[time], px(13.), strong),
        percent: readout_width(window, &[">99.99%", "100.00%", "<0.01%"], px(12.), FontWeight::NORMAL),
    }
}

fn readout_width(window: &Window, samples: &[&str], size: Pixels, weight: FontWeight) -> Pixels {
    let font = Font {
        family: crate::theme::ui_font_family(),
        features: tabular_nums(),
        fallbacks: None,
        weight,
        style: FontStyle::Normal,
    };
    let measured = samples.iter().fold(px(0.), |width, sample| {
        let run = TextRun { len: sample.len(), font: font.clone(), ..TextRun::default() };
        width.max(window.text_system().shape_line(SharedString::from(*sample), size, &[run], None).width())
    });
    // Whole pixels plus slack, so a subpixel shaping difference cannot resize the slot.
    measured.ceil() + px(2.)
}

/// Right-aligned tabular number in a fixed slot. Shorter values leave space on the left; the right edge stays put.
pub fn numeric_slot(value: impl Into<SharedString>, slot: Pixels, size: Pixels, weight: FontWeight) -> Div {
    div()
        .w(slot)
        .min_w(slot)
        .max_w(slot)
        .flex_none()
        .text_right()
        .whitespace_nowrap()
        .overflow_hidden()
        .text_size(size)
        .font_weight(weight)
        .font_features(tabular_nums())
        .child(value.into())
}

pub fn metric(label: impl Into<SharedString>, value: impl Into<SharedString>, slot: Pixels) -> AnyElement {
    let theme = Theme::ocean();
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(5.))
        .text_size(px(12.))
        .child(div().flex_none().whitespace_nowrap().text_color(theme.text_secondary).child(label.into()))
        .child(numeric_slot(value, slot, px(13.), FontWeight(560.)).text_color(theme.text))
        .into_any_element()
}

pub fn refresh_button(
    loading: bool,
    on_down: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Stateful<Div> {
    let theme = Theme::ocean();
    let label = if loading { tr("统计中…", "Scanning…") } else { tr("重新统计", "Rescan") };
    div()
        .id("performance-refresh")
        .relative()
        .children(smoke_overlay::probe("performance-refresh"))
        .h(px(28.))
        .px(px(12.))
        .rounded(radius::CONTROL)
        .bg(theme.paper)
        .border_1()
        .border_color(theme.btn_border)
        .flex()
        .items_center()
        .justify_center()
        .gap(px(6.))
        .text_size(px(13.))
        .font_weight(FontWeight(460.))
        .text_color(if loading { theme.text_secondary } else { theme.text })
        .cursor(if loading { CursorStyle::Arrow } else { CursorStyle::PointingHand })
        .when(!loading, |btn| btn.hover(|s| s.bg(theme.hover).border_color(theme.accent)))
        .on_mouse_down(MouseButton::Left, move |event, window, cx| {
            cx.stop_propagation();
            on_down(event, window, cx);
        })
        .when(loading, |btn| btn.child(spinner("performance-refresh-spin", theme.accent)))
        .child(label)
}

pub fn loading_screen() -> AnyElement {
    let theme = Theme::ocean();
    div()
        .id("performance-loading")
        .w_full()
        .px(px(20.))
        .py(px(14.))
        .flex()
        .flex_col()
        .gap(px(10.))
        .child(
            div()
                .h(px(36.))
                .w_full()
                .rounded(radius::CONTROL)
                .bg(theme.sidebar)
                .border_1()
                .border_color(theme.hairline)
                .px(px(14.))
                .flex()
                .items_center()
                .gap(px(8.))
                .child(spinner("performance-loading-spin", theme.accent))
                .child(
                    div()
                        .text_size(px(13.))
                        .font_weight(FontWeight(500.))
                        .text_color(theme.text)
                        .child(tr("正在统计磁盘与进程占用…", "Scanning storage and process usage…")),
                ),
        )
        .child(skeleton_row("perf-skel-r1", &[0.34, 0.33, 0.33]))
        .child(skeleton_row("perf-skel-r2", &[0.5, 0.5]))
        .child(skeleton_row("perf-skel-r3", &[0.5, 0.5]))
        .child(skeleton_row("perf-skel-proc", &[1.0]))
        .into_any_element()
}

fn skeleton_row(prefix: &'static str, shares: &[f32]) -> AnyElement {
    let theme = Theme::ocean();
    let (base, peak) = if is_dark() { (0x242424, 0x2f3542) } else { (0xf2f4f7, 0xe5ebf5) };
    div()
        .w_full()
        .flex()
        .gap(px(6.))
        .children(shares.iter().copied().enumerate().map(move |(ix, share)| {
            let anim_id = SharedString::from(format!("{prefix}-{ix}"));
            div()
                .w(relative(share))
                .flex_1()
                .h(px(52.))
                .rounded(radius::CONTROL)
                .border_1()
                .border_color(theme.hairline)
                .with_animation(
                    anim_id,
                    Animation::new(Duration::from_millis(1200)).repeat(),
                    move |el, delta| {
                        let phase = delta * std::f32::consts::TAU;
                        let c1 = fade::mix(base, peak, 0.5 - 0.5 * phase.cos());
                        let c2 = fade::mix(base, peak, 0.5 - 0.5 * (phase - 1.3).cos());
                        el.bg(linear_gradient(90., linear_color_stop(c1, 0.), linear_color_stop(c2, 1.)))
                    },
                )
        }))
        .into_any_element()
}
