//! How the note, the file tree, and the chat share a narrow window.
//!
//! The page keeps a readable floor: the note at 480, Settings at 680. A shortage
//! is absorbed by the chat first (down to its minimum, then by floating over the
//! page). An expanded sidebar stays on screen: it docks when the page floor fits
//! beside it, and otherwise draws over the page. It does not disappear when the
//! window has no column left. A saved chat width is restored when the window grows.

use std::cell::Cell;

use gpui::px;

/// Narrowest note column that still reads as a page.
pub const NOTE_FLOOR: f32 = 480.;
/// Settings page. The agent split needs the list beside a detail that can ellipsize.
pub const SETTINGS_FLOOR: f32 = 680.;
/// Chat sash limits. The panel never docks narrower than this.
pub const CHAT_MIN: f32 = 320.;
pub const CHAT_MAX: f32 = 720.;

/// Smallest window. Settings keeps its page; a narrower main window floats the chat.
pub fn window_min_width() -> f32 {
    SETTINGS_FLOOR
}

#[derive(Clone, Copy, Debug)]
pub struct Input {
    pub window_width: f32,
    pub sidebar_width: f32,
    pub sidebar_collapsed: bool,
    /// Recorded when the sidebar is opened with no column to dock it.
    /// Visibility follows [`Self::sidebar_collapsed`]; a narrow window still draws it.
    pub sidebar_pinned: bool,
    pub chat_open: bool,
    pub chat_fullscreen: bool,
    /// Width the user chose for the chat, before this frame's squeeze.
    pub chat_width: f32,
    pub chat_min: f32,
    pub chat_max: f32,
    /// Width the page beside the sidebar must keep. The note uses [`NOTE_FLOOR`];
    /// Settings uses [`SETTINGS_FLOOR`].
    pub content_floor: f32,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct ShellSplit {
    /// The file tree is out of the row. The collapse preference is unchanged.
    pub sidebar_yielded: bool,
    /// The file tree is drawn over the note because docking it would crush the page.
    pub sidebar_drawer: bool,
    /// The chat is drawn over the note instead of taking a column.
    pub chat_overlay: bool,
    /// Width the chat should use this frame.
    pub chat_width: f32,
}

/// Sidebar metrics published by the workspace for the chat's own layout pass.
#[derive(Clone, Copy, Debug)]
pub struct ShellFrame {
    pub sidebar_width: f32,
    pub sidebar_collapsed: bool,
    pub sidebar_pinned: bool,
}

thread_local! {
    static FRAME: Cell<Option<ShellFrame>> = const { Cell::new(None) };
    static SPLIT: Cell<Option<ShellSplit>> = const { Cell::new(None) };
}

pub fn publish(frame: ShellFrame) {
    FRAME.with(|slot| slot.set(Some(frame)));
}

/// The split the workspace just resolved. The chat paints from this instead of
/// resolving a second copy that can dock while the note is still inset for an overlay.
pub fn publish_split(split: ShellSplit) {
    SPLIT.with(|slot| slot.set(Some(split)));
}

pub fn current_split() -> Option<ShellSplit> {
    SPLIT.with(|slot| slot.get())
}

pub fn frame() -> ShellFrame {
    FRAME.with(|slot| slot.get()).unwrap_or(ShellFrame {
        sidebar_width: 240.,
        sidebar_collapsed: false,
        sidebar_pinned: false,
    })
}

/// The saved sidebar and chat widths fit beside the page floor.
pub fn fits_saved(input: &Input) -> bool {
    let sidebar = reserved_sidebar(input.sidebar_collapsed, input.sidebar_width);
    let chat = if input.chat_open && !input.chat_fullscreen { clamped_chat(input) } else { 0. };
    sidebar + input.content_floor + chat <= input.window_width + 0.5
}

/// Widest chat width a drag may commit without pushing the note under its floor.
/// A docked sidebar keeps its column. A drawer does not, so the chat may use that width.
pub fn chat_limit(window: f32, sidebar: f32, collapsed: bool, chat_min: f32, chat_max: f32) -> f32 {
    let docks = !collapsed && sidebar + NOTE_FLOOR <= window + 0.5;
    let room = if docks { window - sidebar - NOTE_FLOOR } else { window - NOTE_FLOOR };
    room.clamp(chat_min, chat_max)
}

/// Right inset that keeps the note's measure on the clear side of an overlay chat.
pub fn note_padding(split: ShellSplit, window_width: f32, sidebar_width: f32) -> f32 {
    if !split.chat_overlay {
        return 0.;
    }
    let sidebar = if split.sidebar_yielded { 0. } else { sidebar_width };
    let column = (window_width - sidebar).max(0.);
    let visible = (column - split.chat_width).max(0.);
    if visible >= NOTE_FLOOR { split.chat_width } else { (column - NOTE_FLOOR).max(0.) }
}

pub fn resolve(input: Input) -> ShellSplit {
    let desired = clamped_chat(&input);
    let floor = input.content_floor;
    let sidebar = reserved_sidebar(input.sidebar_collapsed, input.sidebar_width);
    let expanded = sidebar > 0.;
    let can_dock = expanded && sidebar + floor <= input.window_width + 0.5;
    // No column left: draw the expanded sidebar over the page instead of hiding it.
    let drawer = expanded && !can_dock;
    let docked = if drawer { 0. } else { sidebar };

    if !input.chat_open || input.chat_fullscreen {
        return ShellSplit {
            sidebar_yielded: drawer,
            sidebar_drawer: drawer,
            chat_overlay: false,
            chat_width: desired,
        };
    }

    if docked + floor + desired <= input.window_width + 0.5 {
        return done(drawer, false, desired);
    }
    let beside = input.window_width - docked - floor;
    if docked > 0. && beside + 0.5 >= input.chat_min {
        return done(drawer, false, beside.min(desired).max(input.chat_min));
    }
    // The chat cannot take a column beside the sidebar. Keep the sidebar and,
    // when it is only a drawer, dock the chat in the width it freed.
    if drawer {
        let free = input.window_width - floor;
        if free + 0.5 >= input.chat_min {
            return ShellSplit {
                sidebar_yielded: true,
                sidebar_drawer: true,
                chat_overlay: false,
                chat_width: free.min(desired).max(input.chat_min),
            };
        }
    }
    ShellSplit {
        sidebar_yielded: drawer,
        sidebar_drawer: drawer,
        chat_overlay: true,
        chat_width: input.chat_min.min(input.window_width.max(input.chat_min)),
    }
}

/// Pixel width for callers that still speak in [`gpui::Pixels`].
pub fn floor() -> gpui::Pixels {
    px(NOTE_FLOOR)
}

/// Pixel width of the Settings page floor.
pub fn settings_floor() -> gpui::Pixels {
    px(SETTINGS_FLOOR)
}

fn done(drawer: bool, overlay: bool, chat_width: f32) -> ShellSplit {
    ShellSplit { sidebar_yielded: drawer, sidebar_drawer: drawer, chat_overlay: overlay, chat_width }
}

fn reserved_sidebar(collapsed: bool, width: f32) -> f32 {
    if collapsed { 0. } else { width.max(0.) }
}

fn clamped_chat(input: &Input) -> f32 {
    input.chat_width.clamp(input.chat_min, input.chat_max)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn input(window: f32, sidebar: f32, chat: f32, open: bool) -> Input {
        Input {
            window_width: window,
            sidebar_width: sidebar,
            sidebar_collapsed: false,
            sidebar_pinned: false,
            chat_open: open,
            chat_fullscreen: false,
            chat_width: chat,
            chat_min: CHAT_MIN,
            chat_max: CHAT_MAX,
            content_floor: NOTE_FLOOR,
        }
    }

    fn settings(window: f32, sidebar: f32) -> Input {
        let mut sample = input(window, sidebar, 0., false);
        sample.content_floor = SETTINGS_FLOOR;
        sample
    }

    fn note(window: f32, sidebar: f32, split: ShellSplit) -> f32 {
        let side = if split.sidebar_yielded { 0. } else { sidebar };
        let chat = if split.chat_overlay { 0. } else { split.chat_width };
        window - side - chat
    }

    #[test]
    fn wide_window_docks_every_column() {
        let split = resolve(input(1400., 240., 396., true));
        assert!(!split.sidebar_yielded && !split.chat_overlay);
        assert_eq!(split.chat_width, 396.);
        assert!(note(1400., 240., split) >= NOTE_FLOOR);
    }

    #[test]
    fn chat_shrinks_before_the_file_tree_leaves() {
        let split = resolve(input(1100., 240., 396., true));
        assert!(!split.sidebar_yielded && !split.chat_overlay);
        assert_eq!(split.chat_width, 380.);
        assert_eq!(note(1100., 240., split), NOTE_FLOOR);
    }

    #[test]
    fn file_tree_stays_and_the_chat_floats_when_the_chat_cannot_shrink() {
        let split = resolve(input(1000., 240., 396., true));
        assert!(!split.sidebar_yielded && !split.sidebar_drawer && split.chat_overlay);
        assert_eq!(split.chat_width, CHAT_MIN);
        let text = (1000. - 240.) - note_padding(split, 1000., 240.);
        assert!(text >= NOTE_FLOOR - 0.5);
    }

    #[test]
    fn minimum_window_keeps_the_file_tree_and_floats_the_chat() {
        let split = resolve(input(800., 240., 396., true));
        assert!(!split.sidebar_yielded && !split.sidebar_drawer && split.chat_overlay);
        assert_eq!(split.chat_width, CHAT_MIN);
        let text = (800. - 240.) - note_padding(split, 800., 240.);
        assert!(text >= NOTE_FLOOR - 0.5);
    }

    #[test]
    fn sidebar_draws_over_the_page_when_it_cannot_dock() {
        let split = resolve(input(700., 240., 396., true));
        assert!(split.sidebar_yielded && split.sidebar_drawer && split.chat_overlay);
        assert_eq!(split.chat_width, CHAT_MIN);
        let text = 700. - note_padding(split, 700., 240.);
        assert!(text >= NOTE_FLOOR - 0.5);
    }

    #[test]
    fn collapsed_sidebar_stays_hidden() {
        let mut sample = input(800., 240., 396., true);
        sample.sidebar_collapsed = true;
        let split = resolve(sample);
        assert!(!split.sidebar_yielded && !split.sidebar_drawer && split.chat_overlay);
    }

    #[test]
    fn pinned_file_tree_stays_and_the_chat_floats() {
        let mut sample = input(1000., 240., 396., true);
        sample.sidebar_pinned = true;
        let split = resolve(sample);
        assert!(!split.sidebar_yielded && !split.sidebar_drawer && split.chat_overlay);
        assert_eq!(split.chat_width, CHAT_MIN);
        let pad = note_padding(split, 1000., 240.);
        let text = (1000. - 240.) - pad;
        assert!(text >= NOTE_FLOOR - 0.5);
    }

    #[test]
    fn pinned_file_tree_that_cannot_dock_becomes_a_drawer() {
        let mut sample = input(800., 420., 0., false);
        sample.sidebar_pinned = true;
        let split = resolve(sample);
        assert!(split.sidebar_yielded && split.sidebar_drawer && !split.chat_overlay);
    }

    #[test]
    fn closed_chat_draws_a_sidebar_that_cannot_dock() {
        assert!(!resolve(input(1100., 240., 396., false)).sidebar_yielded);
        let split = resolve(input(800., 420., 396., false));
        assert!(split.sidebar_yielded && split.sidebar_drawer && !split.chat_overlay);
    }

    #[test]
    fn window_min_keeps_the_settings_page() {
        assert_eq!(window_min_width(), SETTINGS_FLOOR);
        assert!(SETTINGS_FLOOR > NOTE_FLOOR);
        assert!(SETTINGS_FLOOR < NOTE_FLOOR + CHAT_MIN);
    }

    #[test]
    fn narrowest_window_draws_the_sidebar_and_floats_the_chat() {
        let split = resolve(input(SETTINGS_FLOOR, 240., 396., true));
        assert!(split.sidebar_yielded && split.sidebar_drawer && split.chat_overlay);
        assert!(SETTINGS_FLOOR - note_padding(split, SETTINGS_FLOOR, 240.) >= NOTE_FLOOR - 0.5);
    }

    #[test]
    fn settings_nav_opens_over_the_page_until_the_floor_fits() {
        let narrow = resolve(settings(900., 240.));
        assert!(narrow.sidebar_yielded && narrow.sidebar_drawer && !narrow.chat_overlay);
        let wide = resolve(settings(960., 240.));
        assert!(!wide.sidebar_yielded && !wide.sidebar_drawer);
        assert!(960. - 240. >= SETTINGS_FLOOR);
    }

    #[test]
    fn settings_nav_opens_as_a_drawer_when_it_cannot_dock() {
        let mut sample = settings(800., 240.);
        sample.sidebar_pinned = true;
        let split = resolve(sample);
        assert!(split.sidebar_yielded && split.sidebar_drawer && !split.chat_overlay);
        let mut wide = settings(960., 240.);
        wide.sidebar_pinned = true;
        let docked = resolve(wide);
        assert!(!docked.sidebar_yielded && !docked.sidebar_drawer);
    }
}
