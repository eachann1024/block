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
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum CalloutKind { #[default] Note, Tip, Important, Warning, Caution }
impl CalloutKind {
    pub fn from_alert(line: &str) -> Option<Self> {
        let tag = line.trim().strip_prefix("[!")?.strip_suffix(']')?;
        match tag.to_ascii_uppercase().as_str() {
            "NOTE" => Some(Self::Note), "TIP" => Some(Self::Tip), "IMPORTANT" => Some(Self::Important),
            "WARNING" => Some(Self::Warning), "CAUTION" => Some(Self::Caution), _ => None,
        }
    }
    pub fn tag(self) -> &'static str {
        match self { Self::Note => "NOTE", Self::Tip => "TIP", Self::Important => "IMPORTANT",
            Self::Warning => "WARNING", Self::Caution => "CAUTION" }
    }
}
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
/// Original source slice emitted verbatim only while the block's semantics remain untouched.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct CleanSource { pub source: String, pub fingerprint: u64, pub list_number: Option<u64> }
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct ListCode { pub parent_indent: usize, pub extra_indent: usize, pub blank_before: bool, #[serde(default)] pub same_line: bool }
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct BlockMeta {
    pub indent: usize,
    pub list_start: Option<u64>,
    /// Some("") is an unlabelled fenced code block. Contents have no inline marks.
    pub code_language: Option<String>,
    /// Distinguishes a fenced block containing one empty line from an empty fence.
    pub code_empty_line: bool,
    pub list_code: Option<ListCode>,
    /// Thematic break (`---`, `***`, `___`, `<hr>`).
    pub divider: bool,
    /// Standalone image source (`![alt](src)` or `<img ...>`).
    pub image: Option<String>,
    /// Untouched source slice preserved for lossless disk round-trip; ignored once edited.
    pub clean_source: Option<CleanSource>,
    pub inline: Vec<InlineTag>,
    pub html_paragraph: Option<HtmlParagraph>,
    /// H4–H6 keep the existing heading kind vocabulary and carry the exact level here.
    pub heading_level: Option<u8>,
    pub callout: Option<CalloutKind>,
    /// Structural `>` wrapper depth for non-Quote blocks inside blockquotes (`> - item`, `> # h`).
    pub quote_depth: usize,
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
        let index = self.index_of(id)?;
        let new_id = self.next_id; self.next_id += 1;
        let block = Block::new(new_id, kind, text);
        self.blocks.insert(index + 1, block); Some(new_id)
    }
    pub fn remove(&mut self, id: u64) -> bool {
        let Some(index) = self.index_of(id) else { return false };
        if self.blocks.len() == 1 { *self.get_mut(id).unwrap() = Block::new(id, Kind::Paragraph, String::new()); }
        else { self.blocks.remove(index); }
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
        let Some(cur) = self.get(id) else { return };
        if cur.meta.table.is_some() { return; }
        let plain_target = cur.kind == kind && cur.meta.heading_level.is_none() && cur.meta.callout.is_none()
            && !cur.is_atomic() && cur.meta.code_language.is_none() && cur.meta.list_code.is_none()
            && cur.meta.html_paragraph.is_none() && cur.meta.quote_depth == 0
            && (matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo) || (cur.meta.indent == 0 && cur.meta.list_start.is_none()));
        if plain_target { return; }
        if let Some(block) = self.get_mut(id) { block.kind = kind; block.meta.heading_level = None; block.meta.callout = None; block.meta.quote_depth = 0; block.meta.divider = false; block.meta.image = None; block.meta.clean_source = None; block.meta.code_language = None; block.meta.code_empty_line = false; block.meta.list_code = None; block.meta.html_paragraph = None;
            if !matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo) { block.meta.indent = 0; block.meta.list_start = None; }
        }
        self.refresh_block(id);
    }
    pub fn toggle_checked(&mut self, id: u64) {
        if let Some(block) = self.get_mut(id) { block.checked = !block.checked; block.meta.clean_source = None; }
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
