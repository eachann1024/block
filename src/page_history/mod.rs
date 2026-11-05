//! Page History (页面历史) - local document snapshot archive, visual diffing, and safe restoration.

pub mod diff;
pub mod hash;
pub mod modal;
pub mod panel;
pub mod preview;
pub mod storage;
pub mod time;
pub mod types;

pub use panel::render_history_panel;
pub use preview::{render_preview_surface, render_restore_button, restore_availability};
pub use modal::render_restore_modal;
pub use storage::{
    is_file_readonly, load_version_content, load_versions, on_file_saved, on_path_moved,
    record_baseline_if_empty, restore_content_to_file, restore_version_to_file,
};
pub use types::{DiffBlock, DiffStatus, HistoryError, HistoryVersion, PreviewMode};
