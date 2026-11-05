//! Block-level diff between a historical snapshot and the current Markdown.
//!
//! [`compute_block_diff`] is pure and `Send`. Run it off the UI thread and pass the result into
//! `render_preview_surface`; the preview does not diff while painting.
use std::hash::{DefaultHasher, Hash, Hasher};
use crate::editor::{markdown, model::{Block, BlockMeta, Doc, Kind}};
use super::types::{DiffBlock, DiffStatus};

#[path = "diff_myers.rs"]
mod myers;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum DiffOp { Keep, Delete, Insert }

/// Synchronous block diff. Safe to move onto a background task; the returned value is `Send`.
pub fn compute_block_diff(old_source: &str, cur_source: &str) -> Vec<DiffBlock> {
    let old_blocks = meaningful_blocks(markdown::parse(old_source));
    let cur_blocks = meaningful_blocks(markdown::parse(cur_source));
    if old_blocks.is_empty() && cur_blocks.is_empty() {
        return Vec::new();
    }
    if old_blocks.is_empty() {
        return cur_blocks.iter().map(|block| snapshot(block, DiffStatus::Added)).collect();
    }
    if cur_blocks.is_empty() {
        return old_blocks.iter().map(|block| snapshot(block, DiffStatus::Deleted)).collect();
    }

    let old_hashes: Vec<u64> = old_blocks.iter().map(block_hash).collect();
    let cur_hashes: Vec<u64> = cur_blocks.iter().map(block_hash).collect();
    if old_hashes == cur_hashes {
        return cur_blocks.iter().map(|block| snapshot(block, DiffStatus::Unchanged)).collect();
    }

    let script = myers::myers_diff(&old_hashes, &cur_hashes);
    let mut out = Vec::with_capacity(script.len());
    let mut old_idx = 0;
    let mut cur_idx = 0;
    for op in script {
        match op {
            DiffOp::Keep => {
                if old_idx >= old_blocks.len() || cur_idx >= cur_blocks.len() { break; }
                out.push(snapshot(&cur_blocks[cur_idx], DiffStatus::Unchanged));
                old_idx += 1;
                cur_idx += 1;
            }
            DiffOp::Delete => {
                if old_idx >= old_blocks.len() { break; }
                out.push(snapshot(&old_blocks[old_idx], DiffStatus::Deleted));
                old_idx += 1;
            }
            DiffOp::Insert => {
                if cur_idx >= cur_blocks.len() { break; }
                out.push(snapshot(&cur_blocks[cur_idx], DiffStatus::Added));
                cur_idx += 1;
            }
        }
    }
    while old_idx < old_blocks.len() {
        out.push(snapshot(&old_blocks[old_idx], DiffStatus::Deleted));
        old_idx += 1;
    }
    while cur_idx < cur_blocks.len() {
        out.push(snapshot(&cur_blocks[cur_idx], DiffStatus::Added));
        cur_idx += 1;
    }
    out
}

fn snapshot(block: &Block, status: DiffStatus) -> DiffBlock {
    DiffBlock {
        status,
        text: block.text.clone(),
        kind: block.kind,
        markdown: block_markdown(block),
    }
}

fn meaningful_blocks(doc: Doc) -> Vec<Block> {
    let blocks: Vec<Block> = doc.blocks.iter().cloned().collect();
    if blocks.len() == 1 && is_empty_document(&blocks[0]) {
        Vec::new()
    } else {
        blocks
    }
}

fn is_empty_document(block: &Block) -> bool {
    canonical_block(block) == canonical_block(&Block::new(0, Kind::Paragraph, String::new()))
}

/// Hash the whole block. Identity is cleared so position does not look like a content change.
fn block_hash(block: &Block) -> u64 {
    let mut hasher = DefaultHasher::new();
    canonical_block(block).hash(&mut hasher);
    hasher.finish()
}

fn canonical_block(block: &Block) -> String {
    let mut block = block.clone();
    zero_ids(&mut block);
    serde_json::to_string(&block).unwrap_or_else(|_| fallback_canonical(&block))
}

fn zero_ids(block: &mut Block) {
    block.id = 0;
    if let Some(table) = block.meta.table.as_mut() {
        for row in &mut table.rows {
            for cell in &mut row.cells {
                zero_ids(&mut cell.content);
                zero_ids(&mut cell.original);
            }
        }
    }
}

/// Last resort if JSON serialization fails. Still covers every field, including nested tables.
fn fallback_canonical(block: &Block) -> String {
    format!(
        "{:?}|{:?}|{:?}|{:?}|{:?}",
        block.kind, block.text, block.checked, block.spans, meta_parts(&block.meta)
    )
}

fn meta_parts(meta: &BlockMeta) -> String {
    format!("{meta:?}")
}

fn block_markdown(block: &Block) -> String {
    let doc = Doc::from_blocks(vec![block.clone()]);
    let source = markdown::serialize(&doc);
    source.trim_end_matches(['\r', '\n']).to_string()
}
