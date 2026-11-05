//! Data models for Page History snapshots, diff items, and error states.
use serde::{Deserialize, Serialize};
use crate::editor::model::Kind;

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SnapshotMeta {
    pub id: String,
    pub timestamp: u64,
    pub sha256: String,
    pub file_name: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HistoryVersion {
    pub id: String,
    pub timestamp: u64,
    pub display_time: String,
    pub sha256: String,
    pub is_current: bool,
    pub same_as_current: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum PreviewMode {
    #[default]
    Clean,
    Diff,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum DiffStatus {
    Unchanged,
    Deleted,
    Added,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DiffBlock {
    pub status: DiffStatus,
    /// Plain block text. Empty for some tables and images; render `markdown` instead.
    pub text: String,
    pub kind: Kind,
    /// Canonical Markdown of this single block, including tables, marks, and images.
    pub markdown: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum HistoryError {
    LoadFailed(String),
    RestoreFailed(String),
    ReadOnly,
    ExternalConflict,
}

impl HistoryError {
    pub fn message(&self) -> String {
        match self {
            Self::LoadFailed(_) => crate::theme::tr("历史记录加载失败", "Failed to load history").to_string(),
            Self::RestoreFailed(_) => crate::theme::tr("恢复失败，当前内容未更改。", "Restore failed; current content untouched.").to_string(),
            Self::ReadOnly => crate::theme::tr("此页面只读，无法恢复。", "This page is read-only; cannot restore.").to_string(),
            Self::ExternalConflict => crate::theme::tr("页面已更新，请重新对比。", "Page has been updated externally; please re-diff.").to_string(),
        }
    }
}
