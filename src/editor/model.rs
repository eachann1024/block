use serde::{Deserialize, Serialize};
use std::ops::Range;
#[path = "model_sequence.rs"] mod sequence;
#[path = "model_spans.rs"] mod spans;
#[path = "model_blocks.rs"] mod block_ops;
#[path = "model_inline.rs"] mod inline_ops;
#[path = "model_table.rs"] mod table;
pub use table::{Table, TableRow, TableCell, TableFormat, CellAlignment};
pub use sequence::Blocks;
pub use block_ops::{Numbering, list_numbers};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Kind { Paragraph, H1, H2, H3, Bullet, Numbered, Quote, Todo }
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub enum MarkKind { Bold, Italic, Strike, Code }
/// Ordered, disjoint UTF-8 byte ranges. Adjacent equal mark sets are coalesced.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Span { pub start: usize, pub end: usize, pub marks: Vec<MarkKind> }
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct InlineTag {
    pub start: usize, pub end: usize, pub name: String,
    pub attrs: Vec<(String, String)>,
}
/// Imported HTML paragraph shell; inline content remains ordinary editable rich text.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct HtmlParagraph { pub source: String, pub content: Range<usize> }
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct BlockMeta {
    pub indent: usize,
    pub list_start: Option<u64>,
    /// Some("") is an unlabelled fenced code block. Contents have no inline marks.
    pub code_language: Option<String>,
    /// Distinguishes a fenced block containing one empty line from an empty fence.
    pub code_empty_line: bool,
    /// Unsupported Markdown is literal editable source, never silently interpreted.
    pub raw_source: Option<String>,
    /// Display why a source block cannot be edited visually without data loss.
    pub unsupported_reason: Option<String>,
    pub inline: Vec<InlineTag>,
    pub html_paragraph: Option<HtmlParagraph>,
    /// H4–H6 keep the existing heading kind vocabulary and carry the exact level here.
    pub heading_level: Option<u8>,
    pub table: Option<Table>,
    /// The source had no blank line after this block where the serializer would write one.
    pub tight: bool,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Block {
    pub id: u64, pub kind: Kind, pub text: String, pub checked: bool, pub spans: Vec<Span>,
    #[serde(default)] pub meta: BlockMeta,
}
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Doc { pub blocks: Blocks, pub(super) next_id: u64 }
impl Doc {
    pub fn empty() -> Self { Self::from_blocks(vec![Block::new(1, Kind::Paragraph, String::new())]) }
    pub fn from_blocks(blocks: Vec<Block>) -> Self {
        if blocks.is_empty() { return Self::empty(); }
        let next_id = blocks.iter().map(|b| b.id).max().unwrap_or(0) + 1;
        Self { blocks: blocks.into_iter().collect(), next_id }
    }
    pub fn get(&self, id: u64) -> Option<&Block> { self.blocks.by_id(id) }
    /// Mutate content only; IDs are immutable, structure belongs to Doc methods.
    pub fn get_mut(&mut self, id: u64) -> Option<&mut Block> { self.blocks.by_id_mut(id) }
    pub fn refresh_block(&mut self, id: u64) { self.blocks.refresh(id); }
    pub fn list_number(&self, id: u64) -> Option<u64> { self.blocks.list_number(id) }
    pub fn index_of(&self, id: u64) -> Option<usize> { self.blocks.index_of(id) }
    pub fn push(&mut self, kind: Kind, text: String) -> u64 {
        let id = self.next_id; self.next_id += 1;
        self.blocks.insert(self.blocks.len(), Block::new(id, kind, text)); id
    }
    pub fn insert_after(&mut self, id: u64, kind: Kind, text: String) -> Option<u64> {
        let index = self.index_of(id)?; let new_id = self.next_id; self.next_id += 1;
        self.blocks.insert(index + 1, Block::new(new_id, kind, text)); Some(new_id)
    }
    pub fn remove(&mut self, id: u64) -> bool {
        let Some(index) = self.index_of(id) else { return false };
        if self.blocks.len() == 1 { *self.get_mut(id).unwrap() = Block::new(id, Kind::Paragraph, String::new()); }
        else { self.blocks.remove(index); }
        true
    }
    pub fn move_block(&mut self, id: u64, to: usize) -> bool {
        let Some(from) = self.index_of(id) else { return false };
        let to = to.min(self.blocks.len() - 1);
        if from != to { let block = self.blocks.remove(from); self.blocks.insert(to, block); }
        true
    }
    pub fn set_text(&mut self, id: u64, text: String) {
        let Some(block) = self.get_mut(id) else { return };
        let old = &block.text;
        let mut prefix = old.bytes().zip(text.bytes()).take_while(|(a,b)| a == b).count();
        while !old.is_char_boundary(prefix) || !text.is_char_boundary(prefix) { prefix -= 1; }
        let mut suffix = old[prefix..].bytes().rev().zip(text[prefix..].bytes().rev()).take_while(|(a,b)| a == b).count();
        while !old.is_char_boundary(old.len()-suffix) || !text.is_char_boundary(text.len()-suffix) { suffix -= 1; }
        block.replace_range(prefix..old.len()-suffix, &text[prefix..text.len()-suffix]);
        self.refresh_block(id);
    }
    pub fn set_kind(&mut self, id: u64, kind: Kind) {
        if self.get(id).is_some_and(|block| block.meta.table.is_some()) { return; }
        if let Some(block) = self.get_mut(id) { block.kind = kind; block.meta.heading_level = None; block.meta.raw_source = None; block.meta.unsupported_reason = None; block.meta.code_language = None; block.meta.code_empty_line = false; block.meta.html_paragraph = None;
            if !matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { block.meta.indent = 0; block.meta.list_start = None; }
        }
        self.refresh_block(id);
    }
    pub fn toggle_checked(&mut self, id: u64) {
        if let Some(block) = self.get_mut(id) { block.checked = !block.checked; }
        self.refresh_block(id);
    }
    pub fn toggle_mark(&mut self, id: u64, range: Range<usize>, mark: MarkKind) {
        if let Some(block) = self.get_mut(id) { block.toggle_mark(range, mark); }
        self.refresh_block(id);
    }
    pub fn replace_range(&mut self, id: u64, range: Range<usize>, text: &str) -> bool {
        let changed = self.get_mut(id).is_some_and(|b| b.replace_range(range, text));
        self.refresh_block(id); changed
    }
    pub fn split_block(&mut self, id: u64, at: usize, kind: Kind) -> Option<u64> {
        let index = self.index_of(id)?; let new_id = self.next_id;
        let right = self.get_mut(id)?.split_off(at, new_id, kind)?;
        self.next_id += 1; self.refresh_block(id); self.blocks.insert(index + 1, right); Some(new_id)
    }
    /// Merge adjacent blocks, retaining left metadata; returns the join byte offset.
    pub fn merge_blocks(&mut self, left: u64, right: u64) -> Option<usize> {
        let i = self.index_of(left)?;
        if self.index_of(right)? != i + 1 || self.get(left)?.meta.table.is_some()
            || self.get(right)?.meta.table.is_some() { return None; }
        let other = self.blocks.remove(i + 1); let block = self.get_mut(left)?;
        let at = block.text.len(); block.append(&other); self.refresh_block(left); Some(at)
    }
    /// Replacement IDs must be unique; zero IDs are assigned fresh identities.
    pub fn splice_blocks(&mut self, range: Range<usize>, mut replacement: Vec<Block>) -> Vec<Block> {
        assert!(range.start <= range.end && range.end <= self.blocks.len());
        let mut removed = Vec::with_capacity(range.len());
        for _ in range.clone() { removed.push(self.blocks.remove(range.start)); }
        for (offset, block) in replacement.iter_mut().enumerate() {
            if block.id == 0 { block.id = self.next_id; self.next_id += 1; }
            self.next_id = self.next_id.max(block.id + 1);
            block.normalize_spans(); self.blocks.insert(range.start + offset, block.clone());
        }
        if self.blocks.is_empty() { self.push(Kind::Paragraph, String::new()); }
        removed
    }
}
/// Typing marker recognition; exact consumed bytes include multi-digit numbering.
pub fn marker_kind(text: &str) -> Option<(Kind, usize)> {
    for (marker, kind) in [("### ",Kind::H3),("## ",Kind::H2),("# ",Kind::H1),
        ("- ",Kind::Bullet),("> ",Kind::Quote),("[] ",Kind::Todo)] {
        if text.starts_with(marker) { return Some((kind, marker.len())); }
    }
    let digits = text.bytes().take_while(u8::is_ascii_digit).count();
    if digits > 0 && text[digits..].starts_with(". ") { return Some((Kind::Numbered, digits + 2)); }
    None
}
