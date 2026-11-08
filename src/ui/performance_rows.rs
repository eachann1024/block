use std::{sync::OnceLock, time::Duration};
use gpui::{
    Animation, AnimationExt, AnyElement, App, CursorStyle, Div, FontFeatures, FontWeight,
    IntoElement, MouseButton, MouseDownEvent, ParentElement, SharedString, Stateful, Styled,
    Window, div, linear_color_stop, linear_gradient, prelude::*, px, relative,
};
use crate::{
    theme::{Theme, is_dark, tr},
    ui::{annotation_card::spinner, fade, radius, smoke_overlay},
};

/// OpenType `tnum`: every digit has the same advance, so 142.3 and 188.8 occupy the same width.
pub fn tabular_nums() -> FontFeatures {
    static FEATURES: OnceLock<FontFeatures> = OnceLock::new();
    FEATURES.get_or_init(|| FontFeatures(std::sync::Arc::new(vec![("tnum".into(), 1)]))).clone()
}

pub fn metric(label: impl Into<SharedString>, value: impl Into<SharedString>) -> AnyElement {
    let theme = Theme::ocean();
    div()
        .flex()
        .flex_none()
        .items_center()
        .gap(px(5.))
        .text_size(px(12.))
        .child(div().flex_none().whitespace_nowrap().text_color(theme.text_secondary).child(label.into()))
        .child(
            div()
                .flex_none()
                .whitespace_nowrap()
                .text_size(px(13.))
                .font_weight(FontWeight(560.))
                .font_features(tabular_nums())
                .text_color(theme.text)
                .child(value.into()),
        )
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
