//! Implicit treap of preorder rows. Lazy hidden-ancestor counts make directory
//! toggles O(log n); rank selection renders only the viewport, with no flatten.
use std::path::{Path, PathBuf};
use std::sync::Arc;
use super::scan::{Node, compare_names};

#[derive(Debug)]
pub struct Row {
    pub path: Arc<Path>,
    pub name: String,
    pub dir: bool,
    pub depth: usize,
    descendants: usize,
    root_name: Arc<str>,
    root_path: Arc<Path>,
    root_dir: bool,
}

type Link = Option<Box<Entry>>;
struct Entry {
    row: Arc<Row>,
    expanded: bool,
    priority: u64,
    left: Link,
    right: Link,
    size: usize,
    hidden: i32,
    minimum: i32,
    minimum_count: usize,
    lazy: i32,
}

#[derive(Default)]
pub struct TreeIndex { root: Link, serial: u64 }

impl TreeIndex {
    pub fn new(nodes: Vec<Node>) -> Self {
        let mut rows = Vec::new();
        for node in nodes {
            let root = (Arc::<str>::from(node.name.as_str()), Arc::<Path>::from(node.path.as_path()), node.dir);
            flatten(node, 0, &root, &mut rows);
        }
        let serial = rows.len() as u64;
        let root = build(&mut rows.into_iter(), serial as usize, 0);
        Self { root, serial }
    }

    pub fn len(&self) -> usize { visible(&self.root) }

    pub fn row(&mut self, rank: usize) -> Option<(usize, Arc<Row>, bool)> {
        locate(&mut self.root, rank, 0)
    }

    pub fn toggle(&mut self, index: usize, path: &Path) {
        let Some(entry) = at(&mut self.root, index) else { return };
        if entry.row.path.as_ref() != path || !entry.row.dir { return; }
        entry.expanded = !entry.expanded;
        let delta = if entry.expanded { -1 } else { 1 };
        let count = entry.row.descendants;
        let (before, rest) = split(self.root.take(), index + 1);
        let (mut children, after) = split(rest, count);
        add(&mut children, delta);
        self.root = merge(merge(before, children), after);
    }

    /// Full path identity, including a stale directory at the same root path.
    pub fn contains_root_path(&self, path: &Path) -> bool {
        let name = path.file_name().unwrap_or_default().to_string_lossy();
        [false, true].into_iter().any(|dir| {
            let rank = insertion_rank(&self.root, dir, &name, path);
            entry_at(&self.root, rank).is_some_and(|entry| entry.row.path.as_ref() == path)
        })
    }

    /// New notes belong to the root; insertion touches only the search path.
    pub fn insert_note(&mut self, path: PathBuf) {
        if self.contains_root_path(&path) { return; }
        let name = path.file_name().unwrap_or_default().to_string_lossy().into_owned();
        let rank = insertion_rank(&self.root, false, &name, &path);
        let row = Row {
            root_name: Arc::from(name.as_str()), root_path: Arc::from(path.as_path()),
            root_dir: false, name, path: Arc::from(path), dir: false, depth: 0, descendants: 0,
        };
        let entry = self.entry(row);
        let (left, right) = split(self.root.take(), rank);
        self.root = merge(merge(left, Some(entry)), right);
    }

    fn entry(&mut self, row: Row) -> Box<Entry> {
        self.serial += 1;
        let mut priority = self.serial.wrapping_add(0x9e3779b97f4a7c15);
        priority = (priority ^ (priority >> 30)).wrapping_mul(0xbf58476d1ce4e5b9);
        priority = (priority ^ (priority >> 27)).wrapping_mul(0x94d049bb133111eb);
        priority ^= priority >> 31;
        let hidden = row.depth as i32;
        Box::new(Entry { row: Arc::new(row), expanded: false, priority,
            left: None, right: None, size: 1, hidden, minimum: hidden,
            minimum_count: 1, lazy: 0 })
    }
}

fn flatten(node: Node, depth: usize, root: &(Arc<str>, Arc<Path>, bool), rows: &mut Vec<Row>) {
    let start = rows.len();
    rows.push(Row { path: Arc::from(node.path), name: node.name, dir: node.dir, depth,
        descendants: 0, root_name: root.0.clone(), root_path: root.1.clone(), root_dir: root.2 });
    for child in node.children { flatten(child, depth + 1, root, rows); }
    rows[start].descendants = rows.len() - start - 1;
}

// Build a balanced heap in linear time. Original priorities dominate subsequent
// random insertions, which form balanced subtrees between the original rows.
fn build(rows: &mut impl Iterator<Item = Row>, count: usize, depth: u64) -> Link {
    if count == 0 { return None; }
    let left = build(rows, count / 2, depth + 1);
    let row = rows.next().unwrap();
    let hidden = row.depth as i32;
    let right = build(rows, count - count / 2 - 1, depth + 1);
    let mut entry = Box::new(Entry { row: Arc::new(row), expanded: false,
        priority: u64::MAX - depth, left, right, size: count, hidden,
        minimum: hidden, minimum_count: 1, lazy: 0 });
    refresh(&mut entry);
    Some(entry)
}

fn size(link: &Link) -> usize { link.as_ref().map_or(0, |n| n.size) }
fn visible(link: &Link) -> usize {
    link.as_ref().map_or(0, |n| if n.minimum == 0 { n.minimum_count } else { 0 })
}
fn add(link: &mut Link, delta: i32) {
    if let Some(n) = link { n.hidden += delta; n.minimum += delta; n.lazy += delta; }
}
fn push(n: &mut Entry) {
    if n.lazy != 0 {
        add(&mut n.left, n.lazy);
        add(&mut n.right, n.lazy);
        n.lazy = 0;
    }
}
fn refresh(n: &mut Entry) {
    n.size = 1 + size(&n.left) + size(&n.right);
    n.minimum = n.hidden;
    for child in [&n.left, &n.right].into_iter().flatten() {
        n.minimum = n.minimum.min(child.minimum);
    }
    n.minimum_count = usize::from(n.hidden == n.minimum);
    for child in [&n.left, &n.right].into_iter().flatten() {
        if child.minimum == n.minimum { n.minimum_count += child.minimum_count; }
    }
}
fn merge(left: Link, right: Link) -> Link {
    match (left, right) {
        (None, node) | (node, None) => node,
        (Some(mut a), Some(mut b)) => {
            if a.priority >= b.priority {
                push(&mut a);
                a.right = merge(a.right.take(), Some(b));
                refresh(&mut a);
                Some(a)
            } else {
                push(&mut b);
                b.left = merge(Some(a), b.left.take());
                refresh(&mut b);
                Some(b)
            }
        }
    }
}
fn split(link: Link, count: usize) -> (Link, Link) {
    let Some(mut n) = link else { return (None, None) };
    push(&mut n);
    let left_size = size(&n.left);
    if count <= left_size {
        let (left, middle) = split(n.left.take(), count);
        n.left = middle;
        refresh(&mut n);
        (left, Some(n))
    } else {
        let (middle, right) = split(n.right.take(), count - left_size - 1);
        n.right = middle;
        refresh(&mut n);
        (Some(n), right)
    }
}
fn at(link: &mut Link, index: usize) -> Option<&mut Entry> {
    let n = link.as_mut()?;
    push(n);
    let left = size(&n.left);
    if index < left { at(&mut n.left, index) }
    else if index == left { Some(n) }
    else { at(&mut n.right, index - left - 1) }
}
fn locate(link: &mut Link, rank: usize, offset: usize) -> Option<(usize, Arc<Row>, bool)> {
    let n = link.as_mut()?;
    push(n);
    let left = visible(&n.left);
    if rank < left { return locate(&mut n.left, rank, offset); }
    let own = usize::from(n.hidden == 0);
    if own == 1 && rank == left { return Some((offset + size(&n.left), n.row.clone(), n.expanded)); }
    let next_offset = offset + size(&n.left) + 1;
    locate(&mut n.right, rank - left - own, next_offset)
}
fn entry_at(link: &Link, index: usize) -> Option<&Entry> {
    let n = link.as_ref()?;
    let left = size(&n.left);
    if index < left { entry_at(&n.left, index) }
    else if index == left { Some(n) }
    else { entry_at(&n.right, index - left - 1) }
}
fn insertion_rank(link: &Link, dir: bool, name: &str, path: &Path) -> usize {
    let Some(n) = link else { return 0 };
    let order = compare_names(n.row.root_dir, &n.row.root_name, dir, name)
        .then_with(|| n.row.root_path.as_ref().cmp(path));
    if order.is_lt() { size(&n.left) + 1 + insertion_rank(&n.right, dir, name, path) }
    else { insertion_rank(&n.left, dir, name, path) }
}
