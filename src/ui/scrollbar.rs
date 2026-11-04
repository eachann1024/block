//! Shared scrollbar and scroll helpers.
//!
//! Matches the reference page: a 14px transparent track with 4px above and below, a capsule
//! thumb with 4px transparent insets (6px visible), `rgba(11,11,11,.2)` resting and `.35` on
//! hover or drag (white at the same alpha in dark mode). The bar overlays a gutter the content
//! reserves with [`GUTTER`], so showing it never moves the content sideways.
use gpui::{
    App, Div, ElementId, Hsla, ListState, Pixels, ScrollEvent, Window, div, point,
    prelude::*, px, rgb, size, transparent_black, white,
};
use gpui_component::scroll::{
    Scrollbar, ScrollbarHandle, ScrollbarMode, ScrollbarStyles, ScrollbarThumbStyle,
};
use crate::theme::is_dark;

/// Width of the track, and the space a scroll area reserves on its right (`scrollbar-gutter: stable`).
pub const GUTTER: f32 = 14.;
const END_INSET: f32 = 4.;
const THUMB_INSET: f32 = 4.;
const THUMB_MIN: f32 = 32.;

fn thumb_color(hover: bool) -> Hsla {
    let base: Hsla = if is_dark() { white() } else { rgb(0x0b0b0b).into() };
    base.opacity(if hover { 0.35 } else { 0.2 })
}

fn styles(styles: ScrollbarStyles) -> ScrollbarStyles {
    let thumb = |hover: bool| move |thumb: ScrollbarThumbStyle| thumb
        .bg(thumb_color(hover))
        .width(px(GUTTER - THUMB_INSET * 2.))
        .inset(px(THUMB_INSET))
        .radius(px(999.))
        .min_length(px(THUMB_MIN));
    styles
        .track(|track| track.width(px(GUTTER)).bg(transparent_black()))
        .thumb(thumb(false))
        .thumb_hover(thumb(true))
        .thumb_active(thumb(true))
}

/// Vertical scrollbar for a scroll area, hidden by default and shown while scrolling
/// when the content overflows.
///
/// Place it as a child of a `relative` container that has exactly the scroll area's box and
/// reserves [`GUTTER`] on its right. `content_padding` is padding the scroll area adds around
/// its content that the handle does not count (a `ListState` ignores its own padding), so the
/// thumb can reach both ends.
pub fn vertical<H: ScrollbarHandle + Clone>(
    id: impl Into<ElementId>,
    handle: &H,
    content_padding: Pixels,
) -> Div {
    let content = handle.content_size();
    let viewport = handle.viewport_bounds().size;
    let shrink = px(END_INSET * 2.);
    let total_height = if content.height > viewport.height {
        (content.height + content_padding - shrink).max(px(0.))
    } else {
        px(0.)
    };
    let scroll_size = size(content.width, total_height);
    div().absolute().top(px(END_INSET)).bottom(px(END_INSET)).right_0().w(px(GUTTER)).child(
        Scrollbar::vertical(handle).id(id).mode(ScrollbarMode::Scrolling).styles(styles)
            .viewport_from_layout().scroll_size(scroll_size),
    )
}

/// Scrolls `list` by `delta` (positive moves further down), staying inside its content.
pub fn scroll_clamped(list: &ListState, delta: Pixels) {
    if delta == px(0.) { return; }
    list.set_offset_from_scrollbar(point(px(0.), list.scroll_px_offset_for_scrollbar().y - delta));
}

/// Wheel handler for the empty strips beside a centred list. The list scrolls itself while the
/// pointer is over it, so only events outside its bounds are forwarded.
pub fn forward_wheel(list: &ListState) -> impl Fn(&ScrollEvent, &mut Window, &mut App) + 'static {
    let list = list.clone();
    move |event, window, _| {
        if list.viewport_bounds().contains(&event.position) { return; }
        scroll_clamped(&list, -event.delta.pixel_delta(px(20.)).y);
        window.refresh();
    }
}

/// Keeps a line at `y` (window coordinates, `height` tall) at least `margin` inside the list's
/// viewport. Does nothing while it is already inside that band.
pub fn reveal_line(list: &ListState, y: Pixels, height: Pixels, margin: Pixels) {
    let viewport = list.viewport_bounds();
    let margin = margin.min((viewport.size.height - height) / 4.).max(px(0.));
    let (top, bottom) = (viewport.top() + margin, viewport.bottom() - margin);
    if y < top {
        scroll_clamped(list, y - top);
    } else if y + height > bottom {
        scroll_clamped(list, y + height - bottom);
    }
}

/// Brings an item that is not laid out yet into view, then leaves `margin` beyond it.
pub fn reveal_item(list: &ListState, ix: usize, margin: Pixels) {
    let above = list.item_is_above_viewport(ix) == Some(true);
    let below = list.item_is_below_viewport(ix) == Some(true);
    list.scroll_to_reveal_item(ix);
    if above { scroll_clamped(list, -margin); } else if below { scroll_clamped(list, margin); }
}
