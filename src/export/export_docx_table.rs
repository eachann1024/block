//! Native editable grids and padded block containers.
use super::export_docx_assets::Assets;
use super::export_docx_xml::*;
use super::{ExportError, RenderOptions, document, style};
use crate::editor::model::{Block, Table};
use cosmic_text::Color;
fn cell(
    width: u32,
    span: usize,
    merge: Option<&str>,
    fill: Option<Color>,
    content: String,
) -> String {
    let span = if span > 1 {
        format!("<w:gridSpan w:val=\"{span}\"/>")
    } else {
        String::new()
    };
    let merge = merge.map_or(String::new(), |m| format!("<w:vMerge{m}/>"));
    let fill = fill.map_or(String::new(), shade);
    format!(
        "<w:tc><w:tcPr><w:tcW w:w=\"{width}\" w:type=\"dxa\"/>{span}{merge}{fill}<w:vAlign w:val=\"top\"/></w:tcPr>{content}</w:tc>"
    )
}
fn fill(
    pending: &mut [(usize, usize)],
    at: &mut usize,
    upto: usize,
    widths: &[u32],
    out: &mut String,
) {
    while *at < upto.min(pending.len()) {
        let (left, span) = pending[*at];
        if left > 0 {
            pending[*at].0 -= 1;
            out.push_str(&cell(
                widths[*at..*at + span].iter().sum(),
                span,
                Some(""),
                None,
                para(None, "", ""),
            ));
            *at += span;
        } else {
            out.push_str(&cell(widths[*at], 1, None, None, para(None, "", "")));
            *at += 1;
        }
    }
}
pub(super) fn table(
    table: &Table,
    block: &Block,
    options: &RenderOptions,
    width: f32,
    assets: &mut Assets,
) -> Result<String, ExportError> {
    use crate::editor::table_layout::{FONT_SIZE, LINE_HEIGHT, MIN_ROW, PAD_X, PAD_Y};
    let pal = options.theme.palette();
    let (columns, scale) = document::table_columns(table, &options.source, block.id, width)?;
    let widths: Vec<_> = columns.into_iter().map(twips).collect();
    let total: u32 = widths.iter().sum();
    let borders = ["top", "left", "bottom", "right", "insideH", "insideV"]
        .map(|s| border(s, pal.table_border, 0.5))
        .concat();
    let margins = ["top", "bottom", "left", "right"]
        .map(|s| {
            format!(
                "<w:{s} w:w=\"{}\" w:type=\"dxa\"/>",
                twips(if matches!(s, "top" | "bottom") {
                    PAD_Y * scale
                } else {
                    PAD_X * scale
                })
            )
        })
        .concat();
    let mut out = format!(
        "<w:tbl><w:tblPr><w:tblW w:w=\"{total}\" w:type=\"dxa\"/><w:tblInd w:w=\"{}\" w:type=\"dxa\"/><w:tblBorders>{borders}</w:tblBorders><w:tblLayout w:type=\"fixed\"/><w:tblCellMar>{margins}</w:tblCellMar></w:tblPr><w:tblGrid>{}</w:tblGrid>",
        twips(document::indent(
            block,
            super::layout::PAGE_WIDTH - options.theme.margins().1 - options.theme.margins().3
        )),
        widths
            .iter()
            .map(|w| format!("<w:gridCol w:w=\"{w}\"/>"))
            .collect::<String>()
    );
    let geometry = options
        .source
        .block(block.id)
        .and_then(|b| b.table.as_ref());
    let family = if options.source.font_family.is_empty() {
        options.theme.body_family()
    } else {
        &options.source.font_family
    };
    let mut pending = vec![(0usize, 1usize); table.columns];
    for (r, row) in table.rows.iter().enumerate() {
        let height = geometry
            .and_then(|g| g.rows.get(r))
            .copied()
            .unwrap_or(MIN_ROW)
            * scale;
        out.push_str(&format!(
            "<w:tr><w:trPr><w:trHeight w:val=\"{}\" w:hRule=\"atLeast\"/></w:trPr>",
            twips(height)
        ));
        let mut at = 0;
        let mut cells: Vec<_> = row.cells.iter().collect();
        cells.sort_by_key(|c| c.column);
        for item in cells {
            fill(&mut pending, &mut at, item.column, &widths, &mut out);
            if at != item.column || at >= table.columns {
                continue;
            }
            let span = item.colspan.clamp(1, table.columns - at);
            if item.rowspan > 1 {
                pending[at] = (item.rowspan - 1, span);
            }
            let align = super::table::alignment(&item.attrs)
                .or_else(|| super::table::alignment(&table.attrs))
                .unwrap_or_else(|| {
                    match table
                        .alignments
                        .get(item.column)
                        .copied()
                        .unwrap_or_default()
                    {
                        crate::editor::model::CellAlignment::Left => cosmic_text::Align::Left,
                        crate::editor::model::CellAlignment::Center => cosmic_text::Align::Center,
                        crate::editor::model::CellAlignment::Right => cosmic_text::Align::Right,
                    }
                });
            let jc = match align {
                cosmic_text::Align::Center => "center",
                cosmic_text::Align::Right => "right",
                _ => "left",
            };
            let base = Base {
                bold: (geometry.is_none() && item.header)
                    || style::bold(&table.attrs)
                    || style::bold(&item.attrs),
                italic: style::italic(&table.attrs) || style::italic(&item.attrs),
                family: Some(family),
                px: Some(FONT_SIZE * scale),
                color: style::cell_foreground(item, table, &pal),
            };
            let props = format!(
                "{}<w:jc w:val=\"{jc}\"/>",
                spacing(0., 0., LINE_HEIGHT * scale)
            );
            out.push_str(&cell(
                widths[at..at + span].iter().sum(),
                span,
                (item.rowspan > 1).then_some(" w:val=\"restart\""),
                style::cell_background(item, table, &pal),
                para(None, &props, &runs(&item.content, &pal, base, assets)),
            ));
            at += span;
        }
        fill(&mut pending, &mut at, table.columns, &widths, &mut out);
        out.push_str("</w:tr>");
    }
    out.push_str("</w:tbl>");
    Ok(out)
}

pub(super) fn boxed(
    content: String,
    width: f32,
    indent: f32,
    pads: (f32, f32, f32),
    bg: Color,
    borders: &str,
) -> String {
    let (x, t, b) = pads;
    let mar = [("left", x), ("right", x), ("top", t), ("bottom", b)]
        .map(|(s, v)| format!("<w:{s} w:w=\"{}\" w:type=\"dxa\"/>", twips(v)))
        .concat();
    format!(
        "<w:tbl><w:tblPr><w:tblW w:w=\"{}\" w:type=\"dxa\"/><w:tblInd w:w=\"{}\" w:type=\"dxa\"/><w:tblBorders>{borders}</w:tblBorders><w:tblLayout w:type=\"fixed\"/><w:tblCellMar>{mar}</w:tblCellMar></w:tblPr><w:tblGrid><w:gridCol w:w=\"{}\"/></w:tblGrid><w:tr>{}</w:tr></w:tbl>",
        twips(width),
        twips(indent),
        twips(width),
        cell(twips(width), 1, None, (bg.a() > 0).then_some(bg), content)
    )
}
