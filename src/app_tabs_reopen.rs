//! Per-workspace LIFO history of closed tabs for `ReopenClosedTab`.
use std::path::{Path, PathBuf};
use gpui::Context;
use super::{Workspace, clear_tab_menu};

const MAX_CLOSED_TABS: usize = 64;

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ClosedTab {
    pub path: PathBuf,
    pub index: usize,
}

#[derive(Default)]
pub(crate) struct ClosedTabs {
    entries: Vec<ClosedTab>,
}

impl ClosedTabs {
    pub fn push(&mut self, path: PathBuf, index: usize) {
        self.entries.retain(|entry| entry.path != path);
        self.entries.push(ClosedTab { path, index });
        if self.entries.len() > MAX_CLOSED_TABS {
            self.entries.remove(0);
        }
    }

    // Record batches right to left so successive pops restore the original order.
    pub fn record_others(&mut self, tabs: &[PathBuf], keep: &Path) {
        for (ix, path) in tabs.iter().enumerate().rev() {
            if path != keep {
                self.push(path.clone(), ix);
            }
        }
    }

    pub fn record_from(&mut self, start: usize, removed: &[PathBuf]) {
        for (offset, path) in removed.iter().enumerate().rev() {
            self.push(path.clone(), start + offset);
        }
    }

    pub fn pop(&mut self) -> Option<ClosedTab> {
        self.entries.pop()
    }

    pub fn clear(&mut self) {
        self.entries.clear();
    }

    pub fn rebase(&mut self, from: &Path, to: &Path) {
        for entry in &mut self.entries {
            if let Some(next) = crate::files::ops::rebase(&entry.path, from, to) {
                entry.path = next;
            }
        }
        let mut i = self.entries.len();
        while i > 0 {
            i -= 1;
            let path = &self.entries[i].path;
            if self.entries[i + 1..].iter().any(|e| &e.path == path) {
                self.entries.remove(i);
            }
        }
    }

    pub fn remove_under(&mut self, prefix: &Path) {
        self.entries.retain(|entry| !entry.path.starts_with(prefix));
    }
}

impl Workspace {
    pub(crate) fn reopen_closed_tab(&mut self, cx: &mut Context<Self>) {
        if self.busy() { return; }
        clear_tab_menu();
        while let Some(entry) = self.closed_tabs.pop() {
            if self.tabs.contains(&entry.path) || !crate::files::ops::is_file(&entry.path) {
                continue;
            }
            self.open_file_with_mode(entry.path.clone(), true, true, cx);
            if let Some(cur_ix) = self.tabs.iter().position(|p| p == &entry.path) {
                let target_ix = entry.index.min(self.tabs.len() - 1);
                if cur_ix != target_ix {
                    let path = self.tabs.remove(cur_ix);
                    self.tabs.insert(target_ix, path);
                    self.save_tabs_state(cx);
                    cx.notify();
                }
            }
            break;
        }
    }
}
