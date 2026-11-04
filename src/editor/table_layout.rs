//! Native table adapter: cells use the same rich runs, caret and IME geometry as paragraphs.
use std::ops::Range;
use gpui::{Bounds, Pixels, SharedString, TextAlign, TextRun, Window, WrappedLine, point, px, size};
use super::{model::{Table, CellAlignment}, text_runs, view::layout::{TextLayout, CellLayout}};
use crate::theme::Theme;

mod draw;
mod scroll;
pub(super) use draw::{paint_background, paint_cell_selection, paint_selection};
pub(super) use scroll::{paint_edge, settle, track_wheel, visible};

// Reference cells: 15.04px text on a 21.808px line, 6px/10px padding and a collapsed 1px border.
const FONT_SIZE: f32 = 15.04;
const LINE_HEIGHT: f32 = 21.808;
const PAD_Y: f32 = 6.;
const PAD_X: f32 = 10.;
const BORDER: f32 = 1.;
const MIN_ROW: f32 = 34.8;
/// Auto-sized columns follow their widest cell, within these bounds.
const MIN_COL: f32 = 80.;
const MAX_COL: f32 = 381.5;
/// Room kept right of and below the grid for the add-column and add-row buttons.
pub(super) const EXTEND_ROOM: f32 = 24.;

struct Pending {
    row: usize, index: usize, range: Range<usize>,
    text: SharedString, runs: Vec<TextRun>,
    /// Shaped without wrapping; reused whenever the column is wide enough.
    lines: Vec<WrappedLine>, natural: Pixels,
}

pub(super) fn measure(table: &Table, width: Pixels, selected: Option<Range<usize>>, link_ranges: &[Range<usize>], window: &mut Window) -> TextLayout {
    let theme = Theme::ocean();
    let font_size = px(FONT_SIZE);
    let line_height = px(LINE_HEIGHT);
    let width = (width - px(EXTEND_ROOM)).max(px(1.));
    let count = table.columns.max(1);
    let mut pending = Vec::new();
    for (row, index, range) in table.cell_ranges() {
        let cell = &table.rows[row].cells[index];
        let text: SharedString = if cell.content.text.is_empty() { "\u{200b}".into() }
            else { cell.content.text.clone().into() };
        let default_fg: gpui::Hsla = match (crate::theme::is_dark(), cell.header) {
            (false, true) => gpui::rgb(0x52514e).into(),
            (false, false) => gpui::rgb(0x1f1e1d).into(),
            (true, true) => gpui::rgb(0xb4b4b4).into(),
            (true, false) => theme.text,
        };
        let foreground = text_runs::attribute_color(&cell.attrs, false)
            .or_else(|| text_runs::attribute_color(&table.attrs, false)).unwrap_or(default_fg);
        let runs = text_runs::runs(&cell.content, &text, foreground);
        let cell_links: Vec<Range<usize>> = link_ranges.iter().filter_map(|r| {
            let a = r.start.max(range.start);
            let b = r.end.min(range.end);
            (a < b).then(|| (a - range.start)..(b - range.start))
        }).collect();
        let runs = text_runs::underline_ranges(&text, runs, &cell_links);
        let cell_sel = selected.as_ref().and_then(|sel| {
            let a = sel.start.max(range.start);
            let b = sel.end.min(range.end);
            (a < b).then(|| (a - range.start)..(b - range.start))
        });
        let runs = text_runs::tint_selection(&text, runs, cell_sel, theme.selection_text);
        let lines: Vec<WrappedLine> = window.text_system().shape_text(text.clone(), font_size, &runs, None, None)
            .map(|lines| lines.into_iter().collect()).unwrap_or_default();
        let natural = lines.iter().map(|line| line.width()).fold(px(0.), |a, b| a.max(b));
        pending.push(Pending { row, index, range, text, runs, lines, natural });
    }
    // Columns are as wide as their content (like `table-layout: auto`), never stretched to the page.
    let explicit: Vec<Option<f32>> = (0..count).map(|index| table.column_widths.get(index)
        .and_then(|value| value.as_deref()).and_then(|value| value.strip_suffix("px"))
        .and_then(|value| value.parse::<f32>().ok()).filter(|value| value.is_finite() && *value > 0.)).collect();
    let mut widths: Vec<f32> = explicit.iter().map(|value| value.unwrap_or(MIN_COL)).collect();
    let mut order: Vec<&Pending> = pending.iter().collect();
    order.sort_by_key(|p| table.rows[p.row].cells[p.index].colspan);
    for p in order {
        let cell = &table.rows[p.row].cells[p.index];
        let need = (f32::from(p.natural) + PAD_X * 2. + BORDER).min(MAX_COL);
        let first = cell.column.min(count - 1);
        let last = (cell.column + cell.colspan).saturating_sub(1).min(count - 1);
        let free: Vec<usize> = (first..=last).filter(|c| explicit[*c].is_none()).collect();
        let have: f32 = widths[first..=last].iter().sum();
        if need > have && !free.is_empty() {
            let extra = (need - have) / free.len() as f32;
            for c in free { widths[c] += extra; }
        }
    }
    let col_spans: Vec<(Pixels, Pixels)> = {
        let mut xs = vec![px(0.)];
        for value in widths { xs.push(*xs.last().unwrap() + px(value)); }
        (0..count).map(|c| (xs[c], xs[c + 1])).collect()
    };
    let mut heights = vec![px(MIN_ROW); table.rows.len()];
    let mut cells = Vec::new();
    for Pending { row, index, range, text, runs, lines, natural } in pending {
        let cell = &table.rows[row].cells[index];
        let start_col = cell.column.min(count - 1);
        let end_col = (cell.column + cell.colspan).saturating_sub(1).min(count - 1).max(start_col);
        let cell_width = col_spans[end_col].1 - col_spans[start_col].0;
        let inner = (cell_width - px(PAD_X * 2. + BORDER)).max(px(1.));
        let lines: Vec<WrappedLine> = if natural <= inner + px(0.5) { lines } else {
            window.text_system().shape_text(text, font_size, &runs, Some(inner), None)
                .map(|lines| lines.into_iter().collect()).unwrap_or_default()
        };
        let mut at = 0;
        let lines = lines.into_iter().map(|line| { let offset = at; at += line.len()+1; (offset,line) }).collect();
        let alignment = if cell.attrs.is_empty() {
            table.alignments.get(cell.column).copied().unwrap_or_default()
        } else { cell.alignment() };
        let alignment = match alignment { CellAlignment::Left => TextAlign::Left,
            CellAlignment::Center => TextAlign::Center, CellAlignment::Right => TextAlign::Right };
        let text = TextLayout { bounds:Bounds::default(), lines, line_height, cells:Vec::new(), alignment };
        let required = (text.height().max(line_height) + px(PAD_Y * 2. + BORDER)).max(px(MIN_ROW));
        if cell.rowspan <= 1 { heights[row] = heights[row].max(required); }
        cells.push((row, index, range, text, required));
    }
    // Satisfy merged-cell height after the single-row minimums are known.
    for (row, index, _, _, required) in &cells {
        let cell = &table.rows[*row].cells[*index];
        let end_row = (*row + cell.rowspan.max(1)).min(heights.len()).max(*row + 1);
        let used = heights[*row..end_row].iter().copied().fold(px(0.), |a,b| a+b);
        if *required > used { heights[end_row - 1] += *required-used; }
    }
    let mut ys = vec![px(0.)];
    for height in heights { ys.push(*ys.last().unwrap() + height); }
    let cells = cells.into_iter().map(|(row,index,range,mut text,_)| {
        let cell = &table.rows[row].cells[index];
        let start_col = cell.column.min(count - 1);
        let end_col = (cell.column+cell.colspan).saturating_sub(1).min(count-1).max(start_col);
        let (left, right) = (col_spans[start_col].0, col_spans[end_col].1);
        let end_row = (row + cell.rowspan.max(1)).min(ys.len() - 1).max(row + 1);
        let frame = Bounds::new(point(left,ys[row]),
            size(right - left, ys[end_row]-ys[row]));
        // `vertical-align: top`: padding plus half of the collapsed border.
        let text_height = text.height().max(line_height);
        text.bounds = Bounds::new(frame.origin+point(px(PAD_X + BORDER / 2.), px(PAD_Y + BORDER / 2.)),
            size((frame.size.width-px(PAD_X*2.+BORDER)).max(px(1.)), text_height));
        // `td` is transparent; only `th` carries a fill.
        let default_bg: Option<gpui::Hsla> = match (crate::theme::is_dark(), cell.header) {
            (false, true) => Some(gpui::rgb(0xf5f3ed).into()),
            (true, true) => Some(gpui::rgba(0xffffff08).into()),
            (_, false) => None,
        };
        let background = text_runs::attribute_color(&cell.attrs, true)
            .or_else(|| text_runs::attribute_color(&table.attrs, true))
            .or(default_bg);
        CellLayout { range, frame, text:Box::new(text), background }
    }).collect();
    // Bounds span the visible column so wide grids scroll instead of squeezing.
    TextLayout { bounds:Bounds::new(point(px(0.),px(0.)), size(width,*ys.last().unwrap_or(&line_height))),
        lines:Vec::new(), line_height, cells, alignment:TextAlign::Left }
}

/// The painted grid, excluding the extend-button room the block reserves.
pub(super) fn grid_rect(layout: &TextLayout) -> Option<Bounds<Pixels>> {
    let first = layout.cells.first()?.frame;
    Some(layout.cells.iter().fold(first, |acc, cell| acc.union(&cell.frame)))
}
/// Distinct row (top, bottom) and column (left, right) spans of the painted cells.
pub(super) fn spans(layout: &TextLayout) -> (Vec<(Pixels, Pixels)>, Vec<(Pixels, Pixels)>) {
    let mut rows: Vec<(Pixels, Pixels)> = Vec::new(); let mut columns: Vec<(Pixels, Pixels)> = Vec::new();
    for cell in &layout.cells {
        let (r, c) = ((cell.frame.top(), cell.frame.bottom()), (cell.frame.left(), cell.frame.right()));
        if !rows.iter().any(|x| x.0 == r.0) { rows.push(r); }
        if !columns.iter().any(|x| x.0 == c.0) { columns.push(c); }
    }
    rows.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
    columns.sort_by(|a, b| a.0.partial_cmp(&b.0).unwrap_or(std::cmp::Ordering::Equal));
    (rows, columns)
}
/// Index of the span holding `at`, clamped to the first or last span when outside.
pub(super) fn span_index(spans: &[(Pixels, Pixels)], at: Pixels) -> Option<usize> {
    if spans.is_empty() { return None; }
    Some(spans.iter().position(|s| at < s.1).unwrap_or(spans.len() - 1))
}
