//! In-note find (Cmd+F): case-insensitive matching over block text plus match navigation.
use std::ops::Range;
use gpui::{AppContext as _, Context, Entity, KeyBinding, Subscription, Window};
use gpui_component::input::{Enter as InputEnter, InputEvent, InputState};
use super::view::{Editor, selection::Position};

pub(super) struct FindState {
    pub input: Entity<InputState>,
    pub matches: Vec<(u64, Range<usize>)>,
    pub current: usize,
    pub(super) _subs: Vec<Subscription>,
}

/// Byte ranges of every case-insensitive occurrence of `query` in `text`.
pub(super) fn find_all(text: &str, query: &str) -> Vec<Range<usize>> {
    let needle: Vec<char> = query.chars().flat_map(char::to_lowercase).collect();
    let mut out = Vec::new();
    if needle.is_empty() { return out; }
    let mut resume = 0;
    for (start, _) in text.char_indices() {
        if start < resume { continue; }
        let (mut next, mut end, mut ok) = (0, start, true);
        'scan: for (offset, c) in text[start..].char_indices() {
            for lower in c.to_lowercase() {
                if needle.get(next) == Some(&lower) { next += 1; } else { ok = false; break 'scan; }
            }
            end = start + offset + c.len_utf8();
            if next == needle.len() { break; }
        }
        if ok && next == needle.len() { out.push(start..end); resume = end; }
    }
    out
}

impl Editor {
    pub(crate) fn open_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.path.is_none() { return; }
        let seed = self.single_block_selection();
        if let Some(find) = &self.find {
            let input = find.input.clone();
            if let Some(seed) = seed {
                input.update(cx, |input, cx| input.set_value(seed, window, cx));
            }
            cx.defer_in(window, move |_, window, cx| input.update(cx, |input, cx| input.focus(window, cx)));
            return;
        }
        cx.bind_keys([KeyBinding::new("shift-enter", InputEnter { secondary: true, shift: false }, Some("FindBar"))]);
        let input = cx.new(|cx| InputState::new(window, cx));
        let sub = cx.subscribe_in(&input, window, |this, _, event: &InputEvent, window, cx| match event {
            InputEvent::Change => this.refresh_find(true, window, cx),
            InputEvent::PressEnter { secondary: false, .. } => this.find_step(true, window, cx),
            InputEvent::PressEnter { secondary: true, .. } => this.find_step(false, window, cx),
            _ => {}
        });
        self.find = Some(FindState { input: input.clone(), matches: Vec::new(), current: 0, _subs: vec![sub] });
        if let Some(seed) = seed {
            input.update(cx, |input, cx| input.set_value(seed, window, cx));
        }
        cx.defer_in(window, move |_, window, cx| {
            input.update(cx, |input, cx| input.focus(window, cx));
        });
        self.refresh_find(true, window, cx);
        cx.notify();
    }
    /// Text of a short selection inside one block, used to prefill the query.
    fn single_block_selection(&self) -> Option<String> {
        if self.caret.is_empty() || self.caret.anchor.block != self.caret.head.block { return None; }
        let (a, b, ..) = self.ordered();
        let text = &self.doc.get(a.block)?.text;
        let part = text.get(a.offset..b.offset)?;
        (!part.is_empty() && part.len() <= 80 && !part.contains('\n')).then(|| part.to_owned())
    }
    /// Recompute matches for the current query; `jump` selects the first one at or after the caret.
    pub(super) fn refresh_find(&mut self, jump: bool, window: &mut Window, cx: &mut Context<Self>) {
        let Some(find) = &self.find else { return };
        let query = find.input.read(cx).value().to_string();
        let mut matches = Vec::new();
        for block in &self.doc.blocks {
            for range in find_all(&block.text, &query) { matches.push((block.id, range)); }
        }
        let caret = self.caret.head;
        let at = (self.doc.index_of(caret.block).unwrap_or(0), caret.offset);
        let current = matches.iter().position(|(id, r)| (self.doc.index_of(*id).unwrap_or(0), r.start) >= at).unwrap_or(0);
        if let Some(find) = &mut self.find { find.matches = matches; find.current = current; }
        if jump { self.goto_match(window, cx); }
        cx.notify();
    }
    pub(super) fn find_step(&mut self, forward: bool, window: &mut Window, cx: &mut Context<Self>) {
        // Content may have changed since the last query; rescan before stepping.
        let previous = self.find.as_ref().map(|f| f.current).unwrap_or(0);
        self.refresh_find(false, window, cx);
        let Some(find) = &mut self.find else { return };
        let count = find.matches.len();
        if count == 0 { return; }
        find.current = if forward { (previous + 1) % count } else { (previous + count - 1) % count };
        self.goto_match(window, cx);
    }
    /// Put the selection on the current match and scroll it into view.
    fn goto_match(&mut self, _: &mut Window, cx: &mut Context<Self>) {
        let Some((id, range)) = self.find.as_ref().and_then(|f| f.matches.get(f.current).cloned()) else { return };
        self.caret.anchor = Position { block: id, offset: range.start };
        self.caret.head = Position { block: id, offset: range.end };
        self.caret.composing = None;
        self.reveal_caret = true;
        if let Some(index) = self.doc.index_of(id) { self.list.scroll_to_reveal_item(index); }
        cx.notify();
    }
    pub(super) fn close_find(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.find.take().is_some() { window.focus(&self.focus, cx); cx.notify(); }
    }
    /// All match ranges inside `block` plus the one that is current, for the paint layer.
    pub(super) fn find_ranges(&self, block: u64) -> Option<(Vec<Range<usize>>, Option<Range<usize>>)> {
        let find = self.find.as_ref()?;
        let text = &self.doc.get(block)?.text;
        let valid = |r: &Range<usize>| r.end <= text.len() && text.is_char_boundary(r.start) && text.is_char_boundary(r.end);
        let all: Vec<_> = find.matches.iter().filter(|(id, r)| *id == block && valid(r)).map(|(_, r)| r.clone()).collect();
        let current = find.matches.get(find.current).filter(|(id, r)| *id == block && valid(r)).map(|(_, r)| r.clone());
        (!all.is_empty()).then_some((all, current))
    }
}
