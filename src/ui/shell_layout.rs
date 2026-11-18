//! How the note, the file tree, and the chat share a narrow window.
//!
//! When space runs short, the left file tree steps aside first so the note stays
//! centered beside the AI chat. The right AI chat panel stays docked rather than
//! floating over the page, shrinking down to [`CHAT_MIN`] only after the file tree
//! has yielded. The hidden file tree remains reachable as a drawer.

use std::cell::Cell;

use gpui::px;

/// Narrowest note column in a two-column split.
pub const NOTE_FLOOR: f32 = 480.;
/// Minimum note column kept before the file tree docks alongside an open chat,
/// accounting for the block handle gutter so prose stays balanced and centered.
pub const THREE_COLUMN_NOTE_FLOOR: f32 = 560.;
/// Settings page. The agent split needs the list beside a detail that can ellipsize.
pub const SETTINGS_FLOOR: f32 = 680.;
/// Chat sash limits. The panel never docks narrower than this.
pub const CHAT_MIN: f32 = 320.;
pub const CHAT_MAX: f32 = 720.;

/// Smallest window. Settings keeps its page; a narrower main window docks the chat beside the note.
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
    if input.chat_open && !input.chat_fullscreen {
        let floor = input.content_floor.max(THREE_COLUMN_NOTE_FLOOR);
        sidebar + floor + clamped_chat(input) <= input.window_width + 0.5
    } else {
        sidebar + input.content_floor <= input.window_width + 0.5
    }
}

/// Widest chat width a drag may commit without pushing the note under its floor.
/// A docked sidebar keeps its column. A yielded sidebar does not, so the chat may use that width.
pub fn chat_limit(window: f32, sidebar: f32, collapsed: bool, chat_min: f32, chat_max: f32) -> f32 {
    let three_col_floor = NOTE_FLOOR.max(THREE_COLUMN_NOTE_FLOOR);
    let docks = !collapsed && sidebar + three_col_floor + chat_min <= window + 0.5;
    let room = if docks { window - sidebar - three_col_floor } else { window - NOTE_FLOOR };
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

    if !input.chat_open || input.chat_fullscreen {
        let can_dock = expanded && sidebar + floor <= input.window_width + 0.5;
        let yielded = expanded && !can_dock;
        return ShellSplit {
            sidebar_yielded: yielded,
            sidebar_drawer: yielded && input.sidebar_pinned,
            chat_overlay: false,
            chat_width: desired,
        };
    }

    let three_col_floor = floor.max(THREE_COLUMN_NOTE_FLOOR);
    let can_dock = expanded && sidebar + three_col_floor + input.chat_min <= input.window_width + 0.5;
    let yielded = expanded && !can_dock;
    let drawer = yielded && input.sidebar_pinned;
    let (docked, page_floor) = if yielded || !expanded { (0., floor) } else { (sidebar, three_col_floor) };
    let free = input.window_width - docked - page_floor;
    let chat_width = free.clamp(input.chat_min, desired).min(input.window_width.max(0.));

    ShellSplit {
        sidebar_yielded: yielded,
        sidebar_drawer: drawer,
        chat_overlay: false,
        chat_width,
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
        assert!(note(1400., 240., split) >= THREE_COLUMN_NOTE_FLOOR);
    }

    #[test]
    fn three_column_window_shrinks_chat_above_three_column_floor() {
        let split = resolve(input(1150., 240., 396., true));
        assert!(!split.sidebar_yielded && !split.chat_overlay);
        assert_eq!(split.chat_width, 350.);
        assert_eq!(note(1150., 240., split), THREE_COLUMN_NOTE_FLOOR);
    }

    #[test]
    fn left_sidebar_hides_first_and_right_chat_stays_docked() {
        let split = resolve(input(1018., 240., 457., true));
        assert!(split.sidebar_yielded && !split.sidebar_drawer && !split.chat_overlay);
        assert_eq!(split.chat_width, 457.);
        assert!(note(1018., 240., split) >= NOTE_FLOOR);
    }

    #[test]
    fn narrow_two_column_window_shrinks_docked_chat_without_floating() {
        let split = resolve(input(840., 240., 396., true));
        assert!(split.sidebar_yielded && !split.sidebar_drawer && !split.chat_overlay);
        assert_eq!(split.chat_width, 360.);
        assert_eq!(note(840., 240., split), NOTE_FLOOR);
    }

    #[test]
    fn minimum_window_keeps_chat_docked_and_hides_sidebar() {
        let split = resolve(input(SETTINGS_FLOOR, 240., 396., true));
        assert!(split.sidebar_yielded && !split.sidebar_drawer && !split.chat_overlay);
        assert_eq!(split.chat_width, CHAT_MIN);
        assert_eq!(note(SETTINGS_FLOOR, 240., split), SETTINGS_FLOOR - CHAT_MIN);
    }

    #[test]
    fn collapsed_sidebar_stays_hidden_and_chat_never_floats() {
        let mut sample = input(800., 240., 396., true);
        sample.sidebar_collapsed = true;
        let split = resolve(sample);
        assert!(!split.sidebar_yielded && !split.sidebar_drawer && !split.chat_overlay);
        assert_eq!(split.chat_width, CHAT_MIN);
    }

    #[test]
    fn pinned_file_tree_becomes_drawer_while_chat_stays_docked() {
        let mut sample = input(1000., 240., 396., true);
        sample.sidebar_pinned = true;
        let split = resolve(sample);
        assert!(split.sidebar_yielded && split.sidebar_drawer && !split.chat_overlay);
        assert_eq!(split.chat_width, 396.);
        assert!(note(1000., 240., split) >= NOTE_FLOOR);
    }

    #[test]
    fn closed_chat_hides_a_sidebar_that_cannot_dock() {
        assert!(!resolve(input(1100., 240., 396., false)).sidebar_yielded);
        let split = resolve(input(800., 420., 396., false));
        assert!(split.sidebar_yielded && !split.sidebar_drawer && !split.chat_overlay);
    }

    #[test]
    fn window_min_keeps_the_settings_page() {
        assert_eq!(window_min_width(), SETTINGS_FLOOR);
        assert!(SETTINGS_FLOOR > NOTE_FLOOR);
        assert!(SETTINGS_FLOOR < NOTE_FLOOR + CHAT_MIN);
    }

    #[test]
    fn settings_nav_hides_until_the_floor_fits() {
        let narrow = resolve(settings(900., 240.));
        assert!(narrow.sidebar_yielded && !narrow.sidebar_drawer && !narrow.chat_overlay);
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
