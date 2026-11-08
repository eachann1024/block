//! Intelligent viewport-aware placement for floating menus, popovers, and toolbars.
//!
//! Ensures floating surfaces never occlude their trigger / active input line:
//! - Places below (or above when `prefer_above` is set) when sufficient vertical room exists.
//! - Automatically flips to the opposite vertical side of the trigger bounds when overflowing.
//! - Tries the opposite horizontal trigger edge before clamping an overflowing aligned popup.
//! - Caps scrollable menu height (`max_height_for_spec`) to the available side in short windows
//!   so the surface scrolls internally instead of sliding over the input line.
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
    /// Prefers the trigger's left edge, flipping to its right edge before viewport clamping.
    Left,
    /// Left edge of the trigger, clamped into the viewport. Trigger width is ignored.
    PinLeft,
    /// Prefers the trigger's right edge, flipping to its left edge before viewport clamping.
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
    let below_top = spec.trigger.bottom().max(min_y) + spec.gap;
    let above_bottom = spec.trigger.top().min(max_y) - spec.gap;
    let space_below = (max_y - below_top).max(px(0.));
    let space_above = (above_bottom - min_y).max(px(0.));
    let need_h = spec.reserve_height.unwrap_or(cap).min(cap);
    let place_above = if spec.prefer_above {
        space_above >= need_h || space_above >= space_below
    } else {
        space_below < need_h && space_above > space_below
    };
    let avail = if place_above { space_above } else { space_below };
    avail.min(cap)
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
    let left = spec.trigger.left();
    let right = spec.trigger.right() - menu_size.width;
    let fits = |x: Pixels| x >= min_x && x + menu_size.width <= max_x;
    // Point anchors have no button edge; preserve their cursor-relative clamping.
    let raw_x = match spec.align_x {
        AlignX::Left if !fits(left) && fits(right) && spec.trigger.size.width > px(0.) => right,
        AlignX::Right if !fits(right) && fits(left) && spec.trigger.size.width > px(0.) => left,
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
    let x = raw_x.min((max_x - menu_size.width).max(min_x)).max(min_x);
    if matches!(spec.align_x, AlignX::CascadeRight { .. }) {
        let y = spec.trigger.top().min((max_y - menu_size.height).max(min_y)).max(min_y);
        return point(x.round(), y.round());
    }

    let below_top = spec.trigger.bottom().max(min_y) + spec.gap;
    let above_bottom = spec.trigger.top().min(max_y) - spec.gap;
    let space_below = (max_y - below_top).max(px(0.));
    let space_above = (above_bottom - min_y).max(px(0.));
    let need_h = spec
        .reserve_height
        .map_or(menu_size.height, |r| r.max(menu_size.height))
        .min((max_y - min_y).max(px(0.)));
    let place_above = if spec.prefer_above {
        space_above >= need_h || space_above >= space_below
    } else {
        space_below < need_h && space_above > space_below
    };

    let y = if place_above {
        let top = (above_bottom - menu_size.height).max(min_y);
        if spec.trigger.size.height > px(0.) {
            top.min((above_bottom - menu_size.height).max(px(4.)))
        } else {
            top
        }
    } else if spec.trigger.size.height > px(0.) {
        below_top.max(min_y)
    } else {
        below_top.min((max_y - menu_size.height).max(min_y)).max(min_y)
    };
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
