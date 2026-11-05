//! Bounded table layout: each merged cell owns one frame and one rich-text buffer.
use super::{
    Cancellation, ExportError, budget,
    layout::{self, PAGE_WIDTH, Row, SCALE},
    palette::ExportTheme,
    rich, style,
};
use crate::editor::model::{CellAlignment, Kind, Table};
use cosmic_text::{Align, CacheKeyFlags, Color, FontSystem, Weight};

const PAD: f32 = 8.;
const FONT: f32 = 17.;
const LINE: f32 = FONT * 1.5;

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
    if rows > 256 || table.columns > 32 {
        return Err(ExportError::ResourceLimit(tr("表格最多 256 行 / 32 列", "Max 256 rows / 32 columns in table")));
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

fn width(value: &str, content_width: f32) -> Result<f32, ExportError> {
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
        .filter(|n| n.is_finite())
        .ok_or(ExportError::UnsupportedTable(crate::theme::tr(
            "正像素或百分比以外的宽度",
            "Table width must be positive pixels or percentage",
        )))
}

fn columns(table: &Table, content_width: f32) -> Result<Vec<f32>, ExportError> {
    let mut widths = vec![None; table.columns];
    for (slot, value) in widths.iter_mut().zip(&table.column_widths) {
        *slot = value
            .as_deref()
            .map(|v| width(v, content_width))
            .transpose()?;
    }
    for row in &table.rows {
        for cell in &row.cells {
            let explicit = style::css(&cell.attrs, "width").or_else(|| cell.attr("width"));
            if let Some(value) = explicit {
                let each = width(value, content_width)? / cell.colspan as f32;
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
        xs.push(xs.last().unwrap() + value / total * content_width);
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
    rows: &mut Vec<Row>,
    y: f32,
    margin_left: f32,
    content_width: f32,
    theme: ExportTheme,
    fonts: &mut FontSystem,
    cancel: &Cancellation,
) -> Result<f32, ExportError> {
    let xs = columns(table, content_width)?;
    let mut heights = vec![LINE + PAD * 2.; table.rows.len()];
    let mut cells = Vec::new();
    for (r, row) in table.rows.iter().enumerate() {
        for cell in &row.cells {
            cancel.check()?;
            let width = xs[cell.column + cell.colspan] - xs[cell.column];
            let pad = PAD.min((width * 0.15).max(2.0));
            let inner_w = (width - pad * 2.).max(16.0);
            let mut attrs = layout::attributes(theme, Kind::Paragraph, &[], false);
            if cell.header || style::bold(&table.attrs) || style::bold(&cell.attrs) {
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
                FONT,
                LINE,
                inner_w,
                Some(align),
                attrs,
            )?;
            let required = shaped
                .buffer
                .layout_runs()
                .map(|r| r.line_top + r.line_height)
                .fold(LINE, f32::max)
                + pad * 2.;
            if cell.rowspan == 1 {
                heights[r] = heights[r].max(required);
            }
            cells.push((r, cell, width, pad, inner_w, shaped, required));
        }
    }
    for (r, cell, _, _, _, _, required) in &cells {
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
    for (r, cell, width, pad, inner_w, shaped, _) in cells {
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
            x: x + pad,
            marker_x: x,
            y: top,
            text_y: top + pad,
            height,
            width: inner_w,
            line_height: LINE,
        });
    }
    Ok(end)
}
