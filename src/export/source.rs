//! Immutable presentation snapshot, scoped to one note and keyed by its original block IDs.
use std::{collections::BTreeMap, path::PathBuf};

#[derive(Clone, Debug, Default, PartialEq)]
pub struct SourceContext {
    pub note: Option<PathBuf>,
    pub font_family: String,
    pub blocks: BTreeMap<u64, BlockLayout>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct BlockLayout {
    pub width: f32,
    pub image_width: Option<f32>,
    pub nowrap: bool,
    pub show_source: bool,
    pub table: Option<TableGeometry>,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct TableGeometry {
    pub columns: Vec<f32>,
    pub rows: Vec<f32>,
}

impl SourceContext {
    pub fn block(&self, id: u64) -> Option<&BlockLayout> { self.blocks.get(&id) }
}
