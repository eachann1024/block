//! Read-only probes for the `--smoke` run; they expose private workspace state to `crate::smoke`.
use super::Workspace;

impl Workspace {
    pub(crate) fn search_is_open(&self) -> bool { self.search.is_some() }
    pub(crate) fn switcher_is_open(&self) -> bool { self.switcher_open }
    pub(crate) fn settings_is_open(&self) -> bool { self.settings.is_some() }
    pub(crate) fn tab_count(&self) -> usize { self.tabs.len() }
}
