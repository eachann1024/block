//! Shared keyboard navigation for menus, pickers, and submenus.
//!
//! Up and down clamp at the ends, skip separators and disabled rows, and scroll
//! the selection toward the middle of the viewport as soon as it passes that
//! line. Callers invoke this from key handling only — never from hover or paint
//! of an unchanged list — so pointer movement does not move the scroll offset.
use std::cell::Cell;
use gpui::{ScrollHandle, Styled, point, px};

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Slot {
    Item,
    Skip,
}

pub fn has_item(slots: &[Slot]) -> bool {
    slots.iter().any(|slot| *slot == Slot::Item)
}

/// Nearest selectable slot. An index on a separator or past the list lands on
/// the following item, or the previous one when nothing follows.
pub fn clamp_index(index: usize, slots: &[Slot]) -> usize {
    if !has_item(slots) { return 0; }
    let index = index.min(slots.len() - 1);
    if slots[index] == Slot::Item { return index; }
    let mut down = index + 1;
    while down < slots.len() {
        if slots[down] == Slot::Item { return down; }
        down += 1;
    }
    let mut up = index;
    while up > 0 {
        up -= 1;
        if slots[up] == Slot::Item { return up; }
    }
    0
}

/// One step up or down. Stays put at the first and last selectable row.
/// An index that is not selectable first lands on the clamped row.
pub fn step(index: usize, slots: &[Slot], down: bool) -> usize {
    let start = clamp_index(index, slots);
    if !has_item(slots) { return start; }
    if index >= slots.len() || slots[index] != Slot::Item { return start; }
    let dir: isize = if down { 1 } else { -1 };
    let mut i = start as isize;
    loop {
        let next = i + dir;
        if next < 0 || next >= slots.len() as isize { return start; }
        i = next;
        if slots[i as usize] == Slot::Item { return i as usize; }
    }
}

/// Uniform selectable rows (no separators). Clamps at both ends.
pub fn step_uniform(index: usize, count: usize, down: bool) -> usize {
    if count == 0 { return 0; }
    let index = index.min(count - 1);
    if down { index.saturating_add(1).min(count - 1) } else { index.saturating_sub(1) }
}

enum Reveal {
    Applied,
    Unchanged,
    Stale,
}

/// Places `child_ix` on the vertical center of the viewport, then clamps so the
/// first rows stay at the top and the last rows stay fully visible.
fn place(handle: &ScrollHandle, child_ix: usize, expected: usize) -> Reveal {
    if expected == 0 || handle.children_count() != expected { return Reveal::Stale; }
    let Some(item) = handle.bounds_for_item(child_ix) else { return Reveal::Stale };
    let view = handle.bounds();
    if view.size.height < px(1.) { return Reveal::Stale; }
    let max = handle.max_offset();
    let desired = if item.size.height >= view.size.height {
        view.top() - item.top()
    } else {
        view.top() + (view.size.height - item.size.height) / 2. - item.top()
    };
    let y = desired.clamp(-max.y, px(0.));
    let offset = handle.offset();
    if offset.y == y { return Reveal::Unchanged; }
    handle.set_offset(point(offset.x, y));
    Reveal::Applied
}

struct Pending {
    handle: ScrollHandle,
    index: usize,
    expected: usize,
    waits: u8,
}

thread_local! {
    static PENDING: Cell<Option<Pending>> = const { Cell::new(None) };
}

/// Scrolls now when this frame's layout matches `expected`. Otherwise waits
/// until the next paint, after the filtered rows have been measured.
pub fn reveal_or_defer(handle: &ScrollHandle, child_ix: usize, expected: usize) {
    if expected == 0 { return; }
    match place(handle, child_ix, expected) {
        Reveal::Stale => PENDING.with(|slot| slot.set(Some(Pending {
            handle: handle.clone(), index: child_ix, expected, waits: 0,
        }))),
        Reveal::Applied | Reveal::Unchanged => PENDING.with(|slot| slot.set(None)),
    }
}

/// Runs from a paint callback. Returns whether the offset changed and the
/// window should draw again.
pub fn flush_pending() -> bool {
    let Some(pending) = PENDING.with(|slot| slot.take()) else { return false };
    match place(&pending.handle, pending.index, pending.expected) {
        Reveal::Applied => true,
        Reveal::Unchanged => false,
        Reveal::Stale => {
            // The menu has not been laid out yet. Give it a couple of frames,
            // then drop the request so a closed menu cannot scroll a later one.
            if pending.handle.children_count() == 0 && pending.waits < 2 {
                PENDING.with(|slot| slot.set(Some(Pending { waits: pending.waits + 1, ..pending })));
            }
            false
        }
    }
}

pub fn reveal_uniform(handle: &gpui::UniformListScrollHandle, index: usize) {
    handle.scroll_to_item_strict(index, gpui::ScrollStrategy::Center);
}

thread_local! {
    static SCROLLS: std::cell::RefCell<std::collections::HashMap<&'static str, ScrollHandle>> =
        std::cell::RefCell::new(std::collections::HashMap::new());
}

pub fn scroll(key: &'static str) -> ScrollHandle {
    SCROLLS.with(|map| map.borrow_mut().entry(key).or_insert_with(ScrollHandle::new).clone())
}

pub fn reset_scroll(key: &'static str) {
    scroll(key).set_offset(point(px(0.), px(0.)));
}

/// Zero-size canvas that applies a deferred menu scroll after layout.
pub fn flush_layer<V: 'static>(cx: &mut gpui::Context<V>) -> impl gpui::IntoElement {
    let entity = cx.entity();
    gpui::canvas(|_, _, _| {}, move |_, _, _, cx| {
        if flush_pending() { entity.update(cx, |_, cx| cx.notify()); }
    }).absolute().size_full()
}
