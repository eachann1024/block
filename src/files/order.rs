//! Manual sibling order follows file identities across notebook switches and moves.
use std::collections::{BTreeMap, HashSet};
use std::io;
use std::path::{Path, PathBuf};

pub type Snapshot = BTreeMap<PathBuf, Vec<PathBuf>>;

pub fn save(snapshot: Snapshot) -> io::Result<()> {
    if snapshot.is_empty() { return Ok(()); }
    super::update_state(|state| {
        for (parent, children) in snapshot {
            // Filtering a folder must not discard the saved positions of hidden rows.
            let visible: HashSet<_> = children.iter().cloned().collect();
            let mut children = children.into_iter();
            let previous = state.file_order.remove(&parent).unwrap_or_default();
            let mut order: Vec<_> = previous.into_iter().filter_map(|child| {
                if visible.contains(&child) { children.next() } else { Some(child) }
            }).collect();
            order.extend(children);
            state.file_order.insert(parent, order);
        }
    }).map(|_| ())
}

pub fn path_moved(from: &Path, to: &Path) {
    let result = super::update_state(|state| {
        let mut moved = Snapshot::new();
        let same_parent = from.parent() == to.parent();
        for (parent, children) in std::mem::take(&mut state.file_order) {
            let parent = super::ops::rebase(&parent, from, to).unwrap_or(parent);
            let children = children.into_iter().filter_map(|child| {
                if !same_parent && child == from { return None; }
                Some(super::ops::rebase(&child, from, to).unwrap_or(child))
            }).collect();
            moved.insert(parent, children);
        }
        if !same_parent && let Some(children) = to.parent().and_then(|p| moved.get_mut(p)) {
            children.retain(|child| child != to);
            children.push(to.to_path_buf());
        }
        state.file_order = moved;
    });
    if let Err(error) = result {
        crate::log::warn(&format!("could not update file order after moving {}: {error}", from.display()));
    }
}
