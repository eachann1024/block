//! Output formats offered by the export view, and the Markdown renderer.
use super::{RenderOptions, WATERMARK_LABEL};
use crate::editor::markdown;
use crate::editor::model::{Block, Doc, Kind};
use crate::theme::tr;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum ExportFormat {
    #[default]
    Png,
    Pdf,
    Word,
    Markdown,
    Html,
}

impl ExportFormat {
    pub const ALL: [Self; 5] = [Self::Png, Self::Pdf, Self::Word, Self::Markdown, Self::Html];

    pub fn label(self) -> &'static str {
        match self {
            Self::Png => "PNG",
            Self::Pdf => "PDF",
            Self::Word => "Word",
            Self::Markdown => "MD",
            Self::Html => "HTML",
        }
    }

    pub fn extension(self) -> &'static str {
        match self {
            Self::Png => "png",
            Self::Pdf => "pdf",
            Self::Word => "docx",
            Self::Markdown => "md",
            Self::Html => "html",
        }
    }

    pub fn save_label(self) -> &'static str {
        match self {
            Self::Png => tr("保存 PNG", "Save PNG"),
            Self::Pdf => tr("保存 PDF", "Save PDF"),
            Self::Word => tr("保存 Word", "Save Word"),
            Self::Markdown => tr("保存 Markdown", "Save Markdown"),
            Self::Html => tr("保存 HTML", "Save HTML"),
        }
    }

    /// Formats whose preview and output are the rasterized paper.
    pub fn is_visual(self) -> bool {
        matches!(self, Self::Png | Self::Pdf)
    }
}

pub fn render_markdown(blocks: &[Block], options: &RenderOptions) -> String {
    let mut normalized = blocks.to_vec();
    for block in &mut normalized {
        if let Some(table) = block.meta.table.as_mut() && table.to_pipe() {
            markdown::rebuild_table_source(table);
        }
    }
    let mut out = markdown::serialize(&Doc::from_blocks(normalized));
    let title = options.title.trim();
    if options.show_title && !title.is_empty() {
        let duplicated = blocks
            .iter()
            .find(|block| !block.text.trim().is_empty() || block.meta.table.is_some())
            .is_some_and(|block| block.kind == Kind::H1 && block.text.trim() == title);
        if !duplicated {
            out.insert_str(0, &format!("# {title}\n\n"));
        }
    }
    if options.footer {
        if !out.ends_with('\n') {
            out.push('\n');
        }
        out.push_str(&format!("\n---\n*{WATERMARK_LABEL}*\n"));
    }
    out
}

pub(super) use crate::editor::markdown::render::{heading_level, is_rule};
