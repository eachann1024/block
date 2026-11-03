use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Kind {
    Paragraph,
    H1,
    H2,
    H3,
    Bullet,
    Numbered,
    Quote,
    Todo,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum MarkKind {
    Bold,
    Italic,
    Strike,
    Code,
}

/// A styled slice of a block, in byte offsets of `Block::text`.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Span {
    pub start: usize,
    pub end: usize,
    pub marks: Vec<MarkKind>,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Block {
    pub id: u64,
    pub kind: Kind,
    pub text: String,
    pub checked: bool,
    pub spans: Vec<Span>,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Doc {
    pub blocks: Vec<Block>,
    pub(super) next_id: u64,
}

impl Doc {
    pub fn empty() -> Self {
        let mut doc = Self { blocks: Vec::new(), next_id: 1 };
        doc.push(Kind::Paragraph, String::new());
        doc
    }

    pub fn push(&mut self, kind: Kind, text: String) -> u64 {
        let id = self.next_id;
        self.next_id += 1;
        self.blocks.push(Block { id, kind, text, checked: false, spans: Vec::new() });
        id
    }

    pub fn index_of(&self, id: u64) -> Option<usize> {
        self.blocks.iter().position(|block| block.id == id)
    }

    pub fn insert_after(&mut self, id: u64, kind: Kind, text: String) -> Option<u64> {
        let index = self.index_of(id)?;
        let new_id = self.next_id;
        self.next_id += 1;
        self.blocks.insert(
            index + 1,
            Block { id: new_id, kind, text, checked: false, spans: Vec::new() },
        );
        Some(new_id)
    }

    pub fn remove(&mut self, id: u64) -> bool {
        let Some(index) = self.index_of(id) else { return false };
        if self.blocks.len() == 1 {
            self.blocks[0].text.clear();
            self.blocks[0].spans.clear();
            self.blocks[0].kind = Kind::Paragraph;
            return true;
        }
        self.blocks.remove(index);
        true
    }

    /// Moves the block so it ends up at `to` in the final ordering.
pub fn move_block(&mut self, id: u64, to: usize) -> bool {
        let Some(from) = self.index_of(id) else { return false };
        let to = to.min(self.blocks.len() - 1);
        if from == to {
            return true;
        }
        let block = self.blocks.remove(from);
        self.blocks.insert(to, block);
        true
    }

    pub fn set_text(&mut self, id: u64, text: String) {
        if let Some(block) = self.blocks.iter_mut().find(|b| b.id == id) {
            block.spans = remap(&block.spans, &block.text, &text);
            block.text = text;
        }
    }

    pub fn set_kind(&mut self, id: u64, kind: Kind) {
        if let Some(block) = self.blocks.iter_mut().find(|b| b.id == id) {
            block.kind = kind;
        }
    }

    pub fn toggle_checked(&mut self, id: u64) {
        if let Some(block) = self.blocks.iter_mut().find(|b| b.id == id) {
            block.checked = !block.checked;
        }
    }

    /// Toggles `mark` across a byte range, splitting and merging spans.
    pub fn toggle_mark(&mut self, id: u64, range: std::ops::Range<usize>, mark: MarkKind) {
        let Some(block) = self.blocks.iter_mut().find(|b| b.id == id) else { return };
        let end = range.end.min(block.text.len());
        let start = range.start.min(end);
        if start == end {
            return;
        }
        let covered = block
            .spans
            .iter()
            .filter(|span| span.start < end && span.end > start && span.marks.contains(&mark))
            .map(|span| span.end.min(end) - span.start.max(start))
            .sum::<usize>();
        let add = covered * 2 <= end - start;
        let mut next = Vec::new();
        for span in block.spans.drain(..) {
            if span.end <= start || span.start >= end {
                next.push(span);
                continue;
            }
            if span.start < start {
                next.push(Span { end: start, ..span.clone() });
            }
            let mut middle = Span {
                start: span.start.max(start),
                end: span.end.min(end),
                marks: span.marks.clone(),
            };
            middle.marks.retain(|item| *item != mark);
            if add {
                middle.marks.push(mark);
            }
            if !middle.marks.is_empty() {
                next.push(middle);
            }
            if span.end > end {
                next.push(Span { start: end, ..span });
            }
        }
        if add && !next.iter().any(|span| span.start <= start && span.end >= end) {
            next.push(Span { start, end, marks: vec![mark] });
        }
        next.sort_by_key(|span| span.start);
        block.spans = merge(next);
    }
}

/// Keeps marks whose text survived an edit unchanged, dropping the rest.
fn remap(spans: &[Span], old: &str, new: &str) -> Vec<Span> {
    let (prefix, suffix) = shared_edges(old, new);
    let delta = new.len() as isize - old.len() as isize;
    let mut out = Vec::new();
    for span in spans {
        let (start, end) = if span.end <= prefix {
            (span.start, span.end)
        } else if span.start >= old.len() - suffix {
            let start = (span.start as isize + delta) as usize;
            (start, (span.end as isize + delta) as usize)
        } else {
            continue;
        };
        if start < end && end <= new.len() {
            out.push(Span { start, end, marks: span.marks.clone() });
        }
    }
    out
}

fn shared_edges(old: &str, new: &str) -> (usize, usize) {
    let prefix = old.bytes().zip(new.bytes()).take_while(|(a, b)| a == b).count();
    let tail = (old.len() - prefix).min(new.len() - prefix);
    let suffix = (0..tail)
        .take_while(|i| old.as_bytes()[old.len() - 1 - i] == new.as_bytes()[new.len() - 1 - i])
        .count();
    (prefix, suffix)
}

fn merge(mut spans: Vec<Span>) -> Vec<Span> {
    spans.retain(|span| span.start < span.end);
    let mut out: Vec<Span> = Vec::new();
    for span in spans {
        if let Some(last) = out.last_mut()
            && last.end == span.start
            && last.marks == span.marks
        {
            last.end = span.end;
            continue;
        }
        out.push(span);
    }
    out
}

/// What typing `marker` at the start of an empty-of-content line turns it into.
pub fn marker_kind(text: &str) -> Option<(Kind, usize)> {
    for (marker, kind) in [
        ("### ", Kind::H3),
        ("## ", Kind::H2),
        ("# ", Kind::H1),
        ("- ", Kind::Bullet),
        ("> ", Kind::Quote),
        ("[] ", Kind::Todo),
    ] {
        if text.starts_with(marker) {
            return Some((kind, marker.len()));
        }
    }
    let digits = text.chars().take_while(|ch| ch.is_ascii_digit()).count();
    let marker = digits + ". ".len();
    if digits > 0 && text.is_char_boundary(marker) && text[digits..].starts_with(". ") {
        return Some((Kind::Numbered, marker));
    }
    None
}
