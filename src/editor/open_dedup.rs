use std::path::{Path, PathBuf};

pub(super) fn duplicate_open(
    path: &Path, pending_open: Option<&Path>, no_pending: bool, loading: bool,
    loading_path: Option<&(PathBuf, u64)>, generation: u64,
) -> bool {
    pending_open == Some(path) || (loading && no_pending
        && loading_path.is_some_and(|(loading, current)| loading == path && *current == generation))
}
