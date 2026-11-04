//! Persistence helpers for restoring and clamping main and quick-note window bounds across launches.
use gpui::{App, Bounds, DisplayId, Pixels, Size, Window, WindowBounds, point, px, size};
use crate::files::state::SavedWindowBounds;

pub fn capture_window_bounds(window: &Window, cx: &App) -> SavedWindowBounds {
    let wb = window.window_bounds();
    let b = wb.get_bounds();
    let display_id = window.display(cx).map(|d| u64::from(d.id()) as u32);
    let (fullscreen, maximized) = match wb {
        WindowBounds::Fullscreen(_) => (true, false),
        WindowBounds::Maximized(_) => (false, true),
        WindowBounds::Windowed(_) => (false, false),
    };
    SavedWindowBounds {
        x: f32::from(b.origin.x),
        y: f32::from(b.origin.y),
        width: f32::from(b.size.width),
        height: f32::from(b.size.height),
        display_id,
        fullscreen,
        maximized,
    }
}

pub fn resolve_window_bounds(
    saved: Option<SavedWindowBounds>,
    default_size: Size<Pixels>,
    min_size: Size<Pixels>,
    cx: &App,
) -> (WindowBounds, Option<DisplayId>) {
    if let Some(s) = saved
        && s.x.is_finite()
        && s.y.is_finite()
        && s.width.is_finite()
        && s.height.is_finite()
        && s.width > 0.
        && s.height > 0.
    {
        let display = s
            .display_id
            .and_then(|id| cx.find_display(DisplayId::from(id as u64)))
            .or_else(|| cx.primary_display());
        let min_w = f32::from(min_size.width);
        let min_h = f32::from(min_size.height);
        let mut width = s.width.max(min_w);
        let mut height = s.height.max(min_h);
        let mut x = s.x;
        let mut y = s.y;

        if let Some(d) = &display {
            let db = d.bounds();
            let dw = f32::from(db.size.width);
            let dh = f32::from(db.size.height);
            if dw >= min_w {
                width = width.min(dw);
            }
            if dh >= min_h {
                height = height.min(dh);
            }
            let dx = f32::from(db.origin.x);
            let dy = f32::from(db.origin.y);
            let min_x = dx - width + 80.;
            let max_x = (dx + dw - 80.).max(min_x);
            let min_y = dy;
            let max_y = (dy + dh - 60.).max(min_y);
            x = x.clamp(min_x, max_x);
            y = y.clamp(min_y, max_y);
        }

        let bounds = Bounds::new(point(px(x), px(y)), size(px(width), px(height)));
        let wb = if s.fullscreen {
            WindowBounds::Fullscreen(bounds)
        } else if s.maximized {
            WindowBounds::Maximized(bounds)
        } else {
            WindowBounds::Windowed(bounds)
        };
        return (wb, display.map(|d| d.id()));
    }

    (WindowBounds::Windowed(Bounds::centered(None, default_size, cx)), None)
}
