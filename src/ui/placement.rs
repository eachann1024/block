//! Intelligent viewport-aware placement for floating menus, popovers, and toolbars.
//!
//! Ensures floating surfaces stay attached to their trigger and inside the safe area:
//! - Places below (or above when `prefer_above` is set) when that side can hold the surface.
//! - Flips to the opposite vertical side, then to the opposite horizontal trigger edge, when that edge fits.
//! - When neither trigger edge fits but the surface itself fits in the safe area, slides it so every
//!   edge stays inside. A model menu at the right of the window moves left instead of being clipped.
//! - When the surface is larger than the safe area, pins the anchored edge (top and left, or the right
//!   edge for a right-aligned menu) so the head stays visible.
//! - Point anchors and cascading submenus clamp fully inside.
//! - Caps scrollable menu height (`max_height_for_spec`) to the available side in short windows
//!   so those menus scroll internally instead of covering the input line.
//! - Resolves geometry during `prepaint` after normal elements have published current-frame bounds.
use gpui::{
    AnyElement, App, Bounds, Deferred, Display, Element, GlobalElementId, InspectorElementId,
    IntoElement, LayoutId, ParentElement, Pixels, Point, Position, Size, Style, Styled, Window, deferred,
    div, point, px, size,
};

pub const DEFAULT_MARGIN: f32 = 8.;
pub const DEFAULT_GAP: f32 = 4.;
pub const MIN_SCROLL_HEIGHT: f32 = 80.;

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum AlignX {
    /// Prefers the trigger's left edge, flipping to its right edge when that edge fits.
    /// Slides inside the safe area when neither edge fits.
    Left,
    /// Left edge of the trigger. Never flips. Slides left when the surface would leave the safe area.
    PinLeft,
    /// Prefers the trigger's right edge, flipping to its left edge when that edge fits.
    /// Slides inside the safe area when neither edge fits.
    Right,
    /// Centers the popup horizontally over `trigger.center().x`.
    Center,
    /// Cascading submenu: opens to the right of `trigger.right() + gap`, flipping left if needed.
    CascadeRight { gap: Pixels },
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct PlacementSpec {
    /// Bounding box of the trigger or input line that must not be occluded.
    pub trigger: Bounds<Pixels>,
    /// Optional container viewport (e.g. editor scroll viewport); intersected with window bounds.
    pub viewport: Option<Bounds<Pixels>>,
    /// Vertical clearance between `trigger` and the popup surface.
    pub gap: Pixels,
    /// Minimum margin from viewport/window edges.
    pub margin: Pixels,
    /// Horizontal alignment mode relative to `trigger`.
    pub align_x: AlignX,
    /// Optional capacity height (e.g. unfiltered menu max height) used to keep a stable side
    /// while the user types to filter a menu.
    pub reserve_height: Option<Pixels>,
    /// When true, prefers placing above `trigger.top() - gap` before falling back below.
    pub prefer_above: bool,
}

impl PlacementSpec {
    pub fn below(trigger: Bounds<Pixels>) -> Self {
        Self {
            trigger,
            viewport: None,
            gap: px(DEFAULT_GAP),
            margin: px(DEFAULT_MARGIN),
            align_x: AlignX::Left,
            reserve_height: None,
            prefer_above: false,
        }
    }

    pub fn above(trigger: Bounds<Pixels>) -> Self {
        Self { prefer_above: true, ..Self::below(trigger) }
    }

    pub fn point(at: Point<Pixels>) -> Self {
        Self::below(Bounds::new(at, size(px(0.), px(0.)))).gap(px(2.))
    }

    pub fn viewport(mut self, viewport: Bounds<Pixels>) -> Self { self.viewport = Some(viewport); self }
    pub fn maybe_viewport(mut self, viewport: Option<Bounds<Pixels>>) -> Self { self.viewport = viewport; self }
    pub fn gap(mut self, gap: Pixels) -> Self { self.gap = gap; self }
    pub fn margin(mut self, margin: Pixels) -> Self { self.margin = margin; self }
    pub fn align_x(mut self, align_x: AlignX) -> Self { self.align_x = align_x; self }
    pub fn reserve_height(mut self, reserve_height: Pixels) -> Self { self.reserve_height = Some(reserve_height); self }
}

fn safe_limits(spec: &PlacementSpec, window_size: Size<Pixels>) -> (Pixels, Pixels, Pixels, Pixels) {
    let win = Bounds::new(Point::default(), window_size);
    let area = match spec.viewport {
        Some(vp) if vp.size.width > px(0.) && vp.size.height > px(0.) => vp.intersect(&win),
        _ => win,
    };
    let min_x = area.left() + spec.margin;
    let max_x = (area.right() - spec.margin).max(min_x);
    let min_y = area.top() + spec.margin;
    let max_y = (area.bottom() - spec.margin).max(min_y);
    (min_x, max_x, min_y, max_y)
}

/// Computes the maximum height a scrollable menu may take on its chosen side of `spec.trigger`
/// so that even in a short window it scrolls internally instead of overlapping `spec.trigger`.
pub fn max_height_for_spec(spec: &PlacementSpec, window_size: Size<Pixels>, cap: Pixels) -> Pixels {
    available_height_for_spec(spec, window_size, cap).max(px(MIN_SCROLL_HEIGHT))
}

pub fn available_height_for_spec(spec: &PlacementSpec, window_size: Size<Pixels>, cap: Pixels) -> Pixels {
    let (_, _, min_y, max_y) = safe_limits(spec, window_size);
    let (space_above, space_below) = vertical_space(spec, min_y, max_y);
    let room = (max_y - min_y).max(px(0.));
    let need_h = spec.reserve_height.unwrap_or(cap).min(cap).min(room);
    let avail = if place_above(spec, space_above, space_below, need_h) { space_above } else { space_below };
    avail.max(px(0.)).min(cap)
}

fn vertical_space(spec: &PlacementSpec, min_y: Pixels, max_y: Pixels) -> (Pixels, Pixels) {
    let space_above = spec.trigger.top() - spec.gap - min_y;
    let space_below = max_y - (spec.trigger.bottom() + spec.gap);
    (space_above, space_below)
}

fn place_above(spec: &PlacementSpec, space_above: Pixels, space_below: Pixels, need_h: Pixels) -> bool {
    if spec.prefer_above {
        space_above >= need_h || space_above >= space_below
    } else {
        space_below < need_h && space_above > space_below
    }
}

/// Slides `raw` so a surface of `size` lies inside `[min, max]`.
/// A surface larger than the span pins to `min`, or to `max - size` when `pin_end` is set,
/// so the anchored edge stays visible and the far side is the one that clips.
fn fit_axis(raw: Pixels, size: Pixels, min: Pixels, max: Pixels, pin_end: bool) -> Pixels {
    let room = (max - min).max(px(0.));
    if size >= room {
        return if pin_end { max - size } else { min };
    }
    raw.min(max - size).max(min)
}

fn clamp_span(raw: Pixels, size: Pixels, min: Pixels, max: Pixels) -> Pixels {
    fit_axis(raw, size, min, max, false)
}

pub fn track_bounds(
    bounds: std::rc::Rc<std::cell::Cell<Option<Bounds<Pixels>>>>,
    refresh_on_change: bool,
) -> impl IntoElement {
    gpui::canvas(move |current, window, _| {
        if bounds.replace(Some(current)) != Some(current) && refresh_on_change { window.refresh(); }
    }, |_, _, _, _| {}).absolute().top_0().left_0().size_full()
}

/// Computes the top-left window coordinate for a surface of `menu_size` placed according to `spec`.
pub fn resolve_origin(spec: &PlacementSpec, menu_size: Size<Pixels>, window_size: Size<Pixels>) -> Point<Pixels> {
    let (min_x, max_x, min_y, max_y) = safe_limits(spec, window_size);
    let point_anchor = spec.trigger.size.width <= px(0.) && spec.trigger.size.height <= px(0.);
    let left = spec.trigger.left();
    let right = spec.trigger.right() - menu_size.width;
    let fits = |x: Pixels| x >= min_x && x + menu_size.width <= max_x;
    let raw_x = match spec.align_x {
        AlignX::Left if !point_anchor && !fits(left) && fits(right) => right,
        AlignX::Right if !point_anchor && !fits(right) && fits(left) => left,
        AlignX::Left | AlignX::PinLeft => left,
        AlignX::Right => right,
        AlignX::Center => spec.trigger.center().x - menu_size.width * 0.5,
        AlignX::CascadeRight { gap } => {
            let right_x = spec.trigger.right() + gap;
            if right_x + menu_size.width <= max_x {
                right_x
            } else {
                let left_x = spec.trigger.left() - gap - menu_size.width;
                if left_x >= min_x { left_x } else { right_x }
            }
        }
    };
    // Submenus and cursor points have no button edge to stay glued to, so they stay on screen.
    if matches!(spec.align_x, AlignX::CascadeRight { .. }) || point_anchor {
        let x = clamp_span(raw_x, menu_size.width, min_x, max_x);
        let y = if point_anchor {
            let ideal = spec.trigger.bottom() + spec.gap;
            clamp_span(ideal, menu_size.height, min_y, max_y)
        } else {
            clamp_span(spec.trigger.top(), menu_size.height, min_y, max_y)
        };
        return point(x.round(), y.round());
    }

    let end_anchor = matches!(spec.align_x, AlignX::Right) && raw_x == right;
    let x = fit_axis(raw_x, menu_size.width, min_x, max_x, end_anchor);

    let (space_above, space_below) = vertical_space(spec, min_y, max_y);
    let room_y = (max_y - min_y).max(px(0.));
    let need_h = spec.reserve_height.map_or(menu_size.height, |r| r.max(menu_size.height)).min(room_y);
    let above = place_above(spec, space_above, space_below, need_h);
    let ideal_y = if above {
        spec.trigger.top() - spec.gap - menu_size.height
    } else {
        spec.trigger.bottom() + spec.gap
    };
    // Too tall to fit: keep the top on screen. Otherwise slide so the bottom cannot leave the area.
    let y = fit_axis(ideal_y, menu_size.height, min_y, max_y, false);
    point(x.round(), y.round())
}

pub struct SmartAnchored {
    children: Vec<AnyElement>,
    resolver: Box<dyn Fn(&Window, &App) -> PlacementSpec>,
}

pub fn smart_anchored(spec: PlacementSpec) -> SmartAnchored {
    SmartAnchored { children: Vec::new(), resolver: Box::new(move |_, _| spec) }
}

pub fn smart_anchored_with(resolver: impl Fn(&Window, &App) -> PlacementSpec + 'static) -> SmartAnchored {
    SmartAnchored { children: Vec::new(), resolver: Box::new(resolver) }
}

pub fn smart_popover(spec: PlacementSpec, child: impl IntoElement, priority: usize) -> Deferred {
    deferred(smart_anchored(spec).child(crate::ui::motion::popup_enter(div().child(child), "dropdown-popup"))).with_priority(priority)
}

pub fn smart_popover_with(resolver: impl Fn(&Window, &App) -> PlacementSpec + 'static, child: impl IntoElement, priority: usize) -> Deferred {
    deferred(smart_anchored_with(resolver).child(crate::ui::motion::popup_enter(div().child(child), "dropdown-popup"))).with_priority(priority)
}

pub fn anchored_motion(
    id: impl Into<gpui::ElementId>,
    spec: PlacementSpec,
    child: impl IntoElement,
    phase: crate::ui::motion::PopupMotion,
    priority: usize,
) -> AnyElement {
    let animated = crate::ui::motion::animate_popup(div().child(child), phase, id);
    deferred(smart_anchored(spec).child(animated)).with_priority(priority).into_any_element()
}

pub fn anchored_motion_with(
    id: impl Into<gpui::ElementId>,
    resolver: impl Fn(&Window, &App) -> PlacementSpec + 'static,
    child: impl IntoElement,
    phase: crate::ui::motion::PopupMotion,
    priority: usize,
) -> AnyElement {
    let animated = crate::ui::motion::animate_popup(div().child(child), phase, id);
    deferred(smart_anchored_with(resolver).child(animated)).with_priority(priority).into_any_element()
}

impl ParentElement for SmartAnchored {
    fn extend(&mut self, elements: impl IntoIterator<Item = AnyElement>) { self.children.extend(elements); }
}

impl Element for SmartAnchored {
    type RequestLayoutState = Vec<LayoutId>;
    type PrepaintState = ();

    fn id(&self) -> Option<gpui::ElementId> { None }
    fn source_location(&self) -> Option<&'static core::panic::Location<'static>> { None }

    fn request_layout(&mut self, _: Option<&GlobalElementId>, _: Option<&InspectorElementId>, window: &mut Window, cx: &mut App) -> (LayoutId, Self::RequestLayoutState) {
        let child_layout_ids = self.children.iter_mut().map(|child| child.request_layout(window, cx)).collect::<Vec<_>>();
        let style = Style { position: Position::Absolute, display: Display::Flex, ..Style::default() };
        let layout_id = window.request_layout(style, child_layout_ids.iter().copied(), cx);
        (layout_id, child_layout_ids)
    }

    fn prepaint(&mut self, _: Option<&GlobalElementId>, _: Option<&InspectorElementId>, bounds: Bounds<Pixels>, child_layout_ids: &mut Self::RequestLayoutState, window: &mut Window, cx: &mut App) {
        if child_layout_ids.is_empty() { return; }
        let children_bounds = child_layout_ids.iter().map(|id| window.layout_bounds(*id)).reduce(|acc, b| acc.union(&b)).unwrap();
        let spec = (self.resolver)(window, cx);
        let desired_origin = resolve_origin(&spec, children_bounds.size, window.viewport_size());
        let offset = desired_origin - bounds.origin;
        window.with_element_offset(point(offset.x.round(), offset.y.round()), |window| {
            for child in &mut self.children { child.prepaint(window, cx); }
        });
    }

    fn paint(&mut self, _: Option<&GlobalElementId>, _: Option<&InspectorElementId>, _: Bounds<Pixels>, _: &mut Self::RequestLayoutState, _: &mut Self::PrepaintState, window: &mut Window, cx: &mut App) {
        for child in &mut self.children { child.paint(window, cx); }
    }
}

impl IntoElement for SmartAnchored {
    type Element = Self;
    fn into_element(self) -> Self::Element { self }
}

#[cfg(test)]
mod tests {
    use super::*;
    use gpui::Bounds;

    fn window() -> Size<Pixels> { size(px(800.), px(600.)) }

    fn trigger(x: f32, y: f32, w: f32, h: f32) -> Bounds<Pixels> {
        Bounds::new(point(px(x), px(y)), size(px(w), px(h)))
    }

    fn origin(spec: PlacementSpec, w: f32, h: f32) -> Point<Pixels> {
        resolve_origin(&spec, size(px(w), px(h)), window())
    }

    #[test]
    fn flips_to_the_trigger_edge_that_fits() {
        let viewport = Bounds::new(point(px(100.), px(40.)), size(px(600.), px(500.)));
        let below = PlacementSpec::below(trigger(620., 500., 32., 28.)).viewport(viewport);
        assert_eq!(origin(below, 206., 126.), point(px(446.), px(370.)));
        let right = PlacementSpec::below(trigger(120., 500., 32., 28.)).viewport(viewport).align_x(AlignX::Right);
        assert_eq!(origin(right, 206., 126.), point(px(120.), px(370.)));
        let fits = PlacementSpec::below(trigger(200., 500., 32., 28.)).viewport(viewport);
        assert_eq!(origin(fits, 206., 126.), point(px(200.), px(370.)));
    }

    #[test]
    fn slides_inside_when_neither_trigger_edge_fits() {
        let viewport = Bounds::new(point(px(100.), px(40.)), size(px(600.), px(500.)));
        let spec = PlacementSpec::below(trigger(380., 500., 32., 28.)).viewport(viewport);
        // 500px menu fits in the viewport but not on either trigger edge, so its right edge
        // sits on the safe right (692) instead of hanging out.
        assert_eq!(origin(spec, 500., 126.), point(px(192.), px(370.)));
    }

    #[test]
    fn model_menu_at_the_window_edge_stays_fully_inside() {
        // Narrow chat column on the right: the trigger's left edge plus the menu width
        // passes the window, which is what clipped Thinking Level and Full access.
        let spec = PlacementSpec::above(trigger(820., 640., 160., 30.))
            .align_x(AlignX::PinLeft)
            .gap(px(4.))
            .margin(px(8.));
        let at = resolve_origin(&spec, size(px(308.), px(280.)), size(px(1100.), px(700.)));
        assert_eq!(at.x, px(784.));
        assert_eq!(at.x + px(308.), px(1092.));
        assert!(at.y >= px(8.) && at.y + px(280.) <= px(640.) - px(4.));
    }

    #[test]
    fn menu_taller_than_the_trigger_gap_pins_to_the_safe_top() {
        let spec = PlacementSpec::above(trigger(80., 360., 40., 28.)).gap(px(4.)).margin(px(8.));
        // Window 600. Space above the trigger cannot hold a 500px menu. Top pins to the margin.
        assert_eq!(origin(spec, 200., 500.), point(px(80.), px(8.)));
    }

    #[test]
    fn right_anchor_slides_inside_instead_of_leaving_the_window() {
        let spec = PlacementSpec::below(trigger(40., 200., 36., 28.)).align_x(AlignX::Right).margin(px(8.));
        // Wider than either trigger edge, but still narrower than the window: slide fully inside.
        let at = origin(spec, 780., 80.);
        assert_eq!(at.x, px(8.));
        assert!(at.x + px(780.) <= px(792.));
        assert_eq!(at.y, px(232.));
    }

    #[test]
    fn anchor_outside_the_safe_area_is_pulled_back_in() {
        let spec = PlacementSpec::below(trigger(-80., 200., 40., 28.)).align_x(AlignX::PinLeft).margin(px(8.));
        assert_eq!(origin(spec.align_x(AlignX::PinLeft), 120., 40.).x, px(8.));
        let right = PlacementSpec::below(trigger(860., 200., 40., 28.)).align_x(AlignX::Right).margin(px(8.));
        let at = origin(right, 100., 40.);
        assert_eq!(at.x + px(100.), px(792.));
    }

    #[test]
    fn center_and_point_and_cascade_stay_inside() {
        let centered = PlacementSpec::below(trigger(760., 80., 24., 24.)).align_x(AlignX::Center).margin(px(8.));
        let at = origin(centered, 120., 40.);
        assert_eq!(at.x + px(120.), px(792.));
        assert!(at.y >= px(8.) && at.y + px(40.) <= px(592.));

        let wide = PlacementSpec::below(trigger(400., 80., 20., 20.)).align_x(AlignX::Center).margin(px(8.));
        assert_eq!(origin(wide, 900., 40.).x, px(8.));

        let cursor = PlacementSpec::point(point(px(780.), px(560.)));
        let at = origin(cursor, 80., 120.);
        assert_eq!(at, point(px(712.), px(472.)));

        let row = trigger(40., 520., 160., 28.);
        let cascade = PlacementSpec::below(row).align_x(AlignX::CascadeRight { gap: px(8.) }).margin(px(8.));
        let at = origin(cascade, 200., 180.);
        assert!(at.x >= px(8.) && at.x + px(200.) <= px(792.));
        assert!(at.y >= px(8.) && at.y + px(180.) <= px(592.));
    }

    #[test]
    fn flips_vertical_side_and_keeps_a_reserved_side() {
        let low = PlacementSpec::below(trigger(40., 520., 32., 28.)).margin(px(8.));
        assert_eq!(origin(low, 80., 160.).y + px(160.), px(520.) - px(4.));

        let high = PlacementSpec::above(trigger(40., 20., 32., 28.)).margin(px(8.));
        assert_eq!(origin(high, 80., 80.).y, px(52.));

        let filtering = PlacementSpec::above(trigger(40., 300., 32., 28.))
            .reserve_height(px(400.))
            .margin(px(8.));
        let open = origin(filtering, 80., 40.);
        assert_eq!(open.y + px(40.), px(300.) - px(4.));
    }

    #[test]
    fn tall_menu_slides_up_so_the_bottom_stays_in_the_window() {
        let spec = PlacementSpec::below(trigger(40., 280., 32., 28.)).margin(px(8.));
        // 500px fits in the window but not under the trigger. Bottom sits on the safe bottom.
        let at = origin(spec, 80., 500.);
        assert_eq!(at.y, px(92.));
        assert_eq!(at.y + px(500.), px(592.));
    }

    #[test]
    fn menu_taller_than_the_window_pins_to_the_top() {
        let spec = PlacementSpec::above(trigger(40., 360., 32., 28.)).margin(px(8.));
        let at = origin(spec, 80., 700.);
        assert_eq!(at, point(px(40.), px(8.)));
    }
}
