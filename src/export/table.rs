//! Bounded table layout: each merged cell owns one frame and one rich-text buffer.
use super::{
    Cancellation, ExportError, budget,
    layout::{self, PAGE_WIDTH, Row, SCALE},
    palette::ExportTheme,
    rich, style,
};
use crate::editor::model::{CellAlignment, Kind, Table};
use cosmic_text::{Family, Align, CacheKeyFlags, Color, FontSystem, Weight};

use crate::editor::table_layout::{FONT_SIZE, LINE_HEIGHT, PAD_X, PAD_Y, BORDER, MIN_ROW};

pub(super) struct Frame {
    pub x: f32,
    pub y: f32,
    pub width: f32,
    pub height: f32,
    pub background: Option<Color>,
}

pub(super) fn validate(table: &Table) -> Result<(), ExportError> {
    use crate::theme::tr;
    let rows = table.rows.len();
    if rows == 0 || table.columns == 0 {
        return Err(ExportError::InvalidInput(tr("表格必须包含行和列", "Table must have rows and columns")));
    }
    if (!table.column_widths.is_empty() && table.column_widths.len() != table.columns)
        || table.alignments.len() > table.columns
    {
        return Err(ExportError::InvalidInput(tr("表格列信息与网格不一致", "Table column metadata does not match grid")));
    }
    let mut occupied = vec![false; rows * table.columns];
    for (r, row) in table.rows.iter().enumerate() {
        for cell in &row.cells {
            if cell.row != r
                || cell.colspan == 0
                || cell.rowspan == 0
                || cell.column >= table.columns
                || cell.colspan > table.columns - cell.column
                || cell.rowspan > rows - r
            {
                return Err(ExportError::InvalidInput(tr("表格合并范围超出网格", "Merged table cell exceeds grid bounds")));
            }
            for y in r..r + cell.rowspan {
                for x in cell.column..cell.column + cell.colspan {
                    if std::mem::replace(&mut occupied[y * table.columns + x], true) {
                        return Err(ExportError::InvalidInput(tr("表格合并单元格重叠", "Merged table cells overlap")));
                    }
                }
            }
        }
    }
    if occupied.contains(&false) {
        return Err(ExportError::InvalidInput(tr("表格网格存在空缺", "Table grid has missing cells")));
    }
    Ok(())
}

fn width(value: &str, content_width: f32) -> Option<f32> {
    let value = value.trim();
    let (number, factor) = if let Some(number) = value.strip_suffix('%') {
        (number, content_width / 100.)
    } else {
        (value.strip_suffix("px").unwrap_or(value), 1.)
    };
    number
        .parse::<f32>()
        .ok()
        .filter(|n| n.is_finite() && *n > 0.)
        .map(|n| n * factor)
        .filter(|n| n.is_finite() && *n > 0.)
}

pub(super) fn columns(table: &Table, content_width: f32) -> Result<Vec<f32>, ExportError> {
    let mut widths = vec![None; table.columns];
    for (slot, value) in widths.iter_mut().zip(&table.column_widths) {
        *slot = value.as_deref().and_then(|v| width(v, content_width));
    }
    for row in &table.rows {
        for cell in &row.cells {
            let explicit = style::css(&cell.attrs, "width").or_else(|| cell.attr("width"));
            if let Some(each) = explicit
                .and_then(|v| width(v, content_width))
                .map(|w| w / cell.colspan as f32)
            {
                for slot in &mut widths[cell.column..cell.column + cell.colspan] {
                    if slot.is_none() {
                        *slot = Some(each);
                    }
                }
            }
        }
    }
    let widths: Vec<_> = widths
        .into_iter()
        .map(|v| v.unwrap_or(content_width / table.columns as f32))
        .collect();
    let total: f32 = widths.iter().sum();
    if !total.is_finite() {
        return Err(ExportError::ResourceLimit(crate::theme::tr("表格列宽溢出", "Table column width overflow")));
    }
    let mut xs = vec![0.];
    for value in widths {
        xs.push(xs.last().unwrap() + value * (content_width / total).min(1.));
    }
    Ok(xs)
}

fn alignment(attrs: &[(String, String)]) -> Option<Align> {
    let value = attrs
        .iter()
        .find(|(name, _)| {
            matches!(
                name.as_str(),
                "data-thinkly-text-alignment" | "data-goose-text-alignment"
            )
        })
        .map(|(_, v)| v.as_str())
        .or_else(|| style::css(attrs, "text-align"))
        .or_else(|| {
            attrs
                .iter()
                .find(|(name, _)| name == "align")
                .map(|(_, v)| v.as_str())
        })?;
    match value {
        "left" | "start" => Some(Align::Left),
        "center" => Some(Align::Center),
        "right" | "end" => Some(Align::Right),
        _ => None,
    }
}

#[allow(clippy::too_many_arguments)]
pub(super) fn append(
    table: &Table,
    source: &super::SourceContext,
    block_id: u64,
    rows: &mut Vec<Row>,
    y: f32,
    margin_left: f32,
    content_width: f32,
    theme: ExportTheme,
    fonts: &mut FontSystem,
    cancel: &Cancellation,
) -> Result<f32, ExportError> {
    let xs = columns(table, content_width)?;
    let geometry = source.block(block_id).and_then(|block| block.table.as_ref());
    let natural = geometry.map(|g| g.columns.iter().sum::<f32>()).unwrap_or(*xs.last().unwrap_or(&content_width));
    let scale = (content_width / natural.max(1.)).min(1.);
    let (font, line, pad_x, pad_y) = (FONT_SIZE * scale, LINE_HEIGHT * scale, (PAD_X + BORDER / 2.) * scale, (PAD_Y + BORDER / 2.) * scale);
    let mut heights: Vec<f32> = (0..table.rows.len()).map(|r|
        geometry.and_then(|g| g.rows.get(r)).copied().unwrap_or(MIN_ROW) * scale).collect();
    let mut cells = Vec::new();
    for (r, row) in table.rows.iter().enumerate() {
        for cell in &row.cells {
            cancel.check()?;
            let width = xs[cell.column + cell.colspan] - xs[cell.column];
            let inner_w = (width - pad_x * 2.).max(1.);
            let mut attrs = layout::attributes(theme, Kind::Paragraph, &[], false);
            if !source.font_family.is_empty() { attrs = attrs.family(Family::Name(&source.font_family)); }
            if (geometry.is_none() && cell.header) || style::bold(&table.attrs) || style::bold(&cell.attrs) {
                attrs = attrs.weight(Weight::BOLD);
            }
            if style::italic(&table.attrs) || style::italic(&cell.attrs) {
                attrs = attrs.cache_key_flags(CacheKeyFlags::FAKE_ITALIC);
            }
            if let Some(color) = style::attribute_color(&cell.attrs, false)
                .or_else(|| style::attribute_color(&table.attrs, false))
            {
                attrs = attrs.color(color);
            }
            let align = alignment(&cell.attrs)
                .or_else(|| alignment(&table.attrs))
                .unwrap_or_else(|| {
                    match table
                        .alignments
                        .get(cell.column)
                        .copied()
                        .unwrap_or_default()
                    {
                        CellAlignment::Left => Align::Left,
                        CellAlignment::Center => Align::Center,
                        CellAlignment::Right => Align::Right,
                    }
                });
            let shaped = rich::shape(
                &cell.content,
                fonts,
                font,
                line,
                inner_w,
                Some(align),
                attrs,
            )?;
            let required = shaped
                .buffer
                .layout_runs()
                .map(|r| r.line_top + r.line_height)
                .fold(line, f32::max)
                + pad_y * 2.;
            if cell.rowspan == 1 {
                heights[r] = heights[r].max(required);
            }
            cells.push((r, cell, width, inner_w, shaped, required));
        }
    }
    for (r, cell, _, _, _, required) in &cells {
        let used: f32 = heights[*r..*r + cell.rowspan].iter().sum();
        if *required > used {
            heights[*r + cell.rowspan - 1] += required - used;
        }
    }
    let mut ys = vec![y];
    for height in heights {
        ys.push(ys.last().unwrap() + height);
    }
    let end = *ys.last().unwrap();
    budget::canvas(PAGE_WIDTH * SCALE, (end + 48.0) * SCALE)?;
    for (r, cell, width, inner_w, shaped, _) in cells {
        let x = margin_left + xs[cell.column];
        let top = ys[r];
        let height = ys[r + cell.rowspan] - top;
        let background = style::attribute_color(&cell.attrs, true)
            .or_else(|| style::attribute_color(&table.attrs, true))
            .or_else(|| cell.header.then_some(Color::rgb(242, 244, 247)));
        rows.push(Row {
            buffer: shaped.buffer,
            backgrounds: shaped.backgrounds,
            frame: Some(Frame {
                x,
                y: top,
                width,
                height,
                background,
            }),
            marker: None,
            label: None,
            kind: Kind::Paragraph,
            checked: false,
            code: false,
            callout: None,
            rule: false,
            x: x + pad_x,
            marker_x: x,
            y: top,
            text_y: top + pad_y,
            height,
            width: inner_w,
            line_height: line,
        });
    }
    Ok(end)
}
