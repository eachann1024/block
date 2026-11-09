//! Linear progress in the Ocean Paper palette.
//! The track and fill stay the existing colors; `gpui_base::Progress` owns the value semantics.
use gpui::{ElementId, IntoElement, SharedString, prelude::*, px, relative};
use gpui_base::{Progress, ProgressIndicator, ProgressTrack};
use crate::theme::Theme;
use super::radius;

pub fn linear(id: impl Into<ElementId>, pct: f32, label: impl Into<SharedString>) -> impl IntoElement {
    let theme = Theme::ocean();
    let pct = pct.clamp(0., 100.);
    Progress::new(id)
        .value(pct)
        .accessibility_label(label)
        .w_full()
        .relative()
        .h(px(6.))
        .rounded(radius::PILL)
        .overflow_hidden()
        .child(
            ProgressTrack::new()
                .absolute()
                .size_full()
                .rounded(radius::PILL)
                .bg(theme.sidebar_border),
        )
        .child(
            ProgressIndicator::new()
                .h_full()
                .rounded(radius::PILL)
                .bg(theme.accent)
                .w(relative(pct / 100.)),
        )
}
