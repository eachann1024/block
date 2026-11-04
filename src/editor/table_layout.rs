//! Native table adapter: cells use the same rich runs, caret and IME geometry as paragraphs.
use gpui::{Bounds, FontWeight, Pixels, SharedString, TextAlign, Window, point, px, size};
use super::{model::{Table, CellAlignment}, text_runs, view::layout::{TextLayout, CellLayout}};
use crate::theme::Theme;

// Reference cells: 44px minimum row, 9px/12px padding, 14px text on a 24px line.
const PAD_Y: f32 = 9.;
const PAD_X: f32 = 12.;
const MIN_ROW: f32 = 44.;
/// Room kept right of and below the grid for the add-column and add-row buttons.
pub(super) const EXTEND_ROOM: f32 = 28.;
pub(super) fn measure(table: &Table, width: Pixels, selected: Option<std::ops::Range<usize>>, window: &mut Window) -> TextLayout {
    let theme = Theme::ocean(); let font_size = px(14.); let line_height = px(24.);
    let width = (width - px(EXTEND_ROOM)).max(px(1.));
    let count = table.columns.max(1);
    let mut widths: Vec<f32> = (0..count).map(|index| table.column_widths.get(index)
        .and_then(|value| value.as_deref()).and_then(|value| value.strip_suffix("px"))
        .and_then(|value| value.parse::<f32>().ok()).filter(|value| value.is_finite() && *value > 0.)
        .unwrap_or(120.)).collect();
    let total = widths.iter().sum::<f32>(); let available = f32::from(width).max(1.);
    for value in &mut widths { *value = *value / total * available; }
    let mut xs = vec![px(0.)];
    for value in widths { xs.push(*xs.last().unwrap() + px(value)); }
    let mut heights = vec![px(MIN_ROW); table.rows.len()];
    let ranges = table.cell_ranges(); let mut cells = Vec::new();
    for (row, cell_index, range) in ranges {
        let cell = &table.rows[row].cells[cell_index];
        let cell_width = xs[cell.column+cell.colspan] - xs[cell.column];
        let text: SharedString = if cell.content.text.is_empty() { "\u{200b}".into() }
            else { cell.content.text.clone().into() };
        let foreground = text_runs::attribute_color(&cell.attrs, false)
            .or_else(|| text_runs::attribute_color(&table.attrs, false)).unwrap_or(theme.text);
        let mut runs = text_runs::runs(&cell.content, &text, foreground);
        if cell.header { for run in &mut runs { run.font.weight = FontWeight::MEDIUM; } }
        let cell_sel = selected.as_ref().and_then(|sel| {
            let a = sel.start.max(range.start) - range.start;
            let b = sel.end.min(range.end) - range.start;
            (a < b).then_some(a..b)
        });
        let runs = text_runs::tint_selection(&text, runs, cell_sel, theme.selection_text);
        let lines = window.text_system().shape_text(text, font_size, &runs,
            Some((cell_width-px(PAD_X*2.)).max(px(1.))), None).unwrap_or_default();
        let mut at = 0;
        let lines = lines.into_iter().map(|line| { let offset = at; at += line.len()+1; (offset,line) }).collect();
        let alignment = if cell.attrs.is_empty() {
            table.alignments.get(cell.column).copied().unwrap_or_default()
        } else { cell.alignment() };
        let alignment = match alignment { CellAlignment::Left => TextAlign::Left,
            CellAlignment::Center => TextAlign::Center, CellAlignment::Right => TextAlign::Right };
        let text = TextLayout { bounds:Bounds::default(), lines, line_height, cells:Vec::new(), alignment };
        let required = (text.height().max(line_height) + px(PAD_Y*2.)).max(px(MIN_ROW));
        if cell.rowspan == 1 { heights[row] = heights[row].max(required); }
        cells.push((row, cell_index, range, text, required));
    }
    // Satisfy merged-cell height after the single-row minimums are known.
    for (row, index, _, _, required) in &cells {
        let cell = &table.rows[*row].cells[*index];
        let used = heights[*row..*row+cell.rowspan].iter().copied().fold(px(0.), |a,b| a+b);
        if *required > used { heights[*row+cell.rowspan-1] += *required-used; }
    }
    let mut ys = vec![px(0.)];
    for height in heights { ys.push(*ys.last().unwrap() + height); }
    let cells = cells.into_iter().map(|(row,index,range,mut text,_)| {
        let cell = &table.rows[row].cells[index];
        let frame = Bounds::new(point(xs[cell.column],ys[row]),
            size(xs[cell.column+cell.colspan]-xs[cell.column], ys[row+cell.rowspan]-ys[row]));
        // Vertically centred like `vertical-align: middle`.
        let text_height = text.height().max(line_height);
        let top = ((frame.size.height - text_height) / 2.).max(px(PAD_Y));
        text.bounds = Bounds::new(frame.origin+point(px(PAD_X),top),
            size((frame.size.width-px(PAD_X*2.)).max(px(1.)), text_height));
        let background = text_runs::attribute_color(&cell.attrs, true)
            .or_else(|| text_runs::attribute_color(&table.attrs, true))
            .or_else(|| cell.header.then_some(theme.hover));
        CellLayout { range, frame, text:Box::new(text), background }
    }).collect();
    TextLayout { bounds:Bounds::new(point(px(0.),px(0.)), size(width,*ys.last().unwrap_or(&line_height))),
        lines:Vec::new(), line_height, cells, alignment:TextAlign::Left }
}

pub(super) fn paint_background(layout: &TextLayout, window: &mut Window) {
    let border = Theme::ocean().menu_border;
    let Some(grid) = grid_rect(layout) else { return };
    for cell in &layout.cells {
        if let Some(color) = cell.background { window.paint_quad(gpui::fill(cell.frame,color)); }
    }
    // Collapsed 1px grid: every cell draws its top and left edge; the outer frame closes it.
    for cell in &layout.cells {
        let frame = cell.frame;
        window.paint_quad(gpui::fill(Bounds::new(frame.origin,size(frame.size.width,px(1.))),border));
        window.paint_quad(gpui::fill(Bounds::new(frame.origin,size(px(1.),frame.size.height)),border));
    }
    window.paint_quad(gpui::fill(Bounds::new(point(grid.right()-px(1.),grid.top()),size(px(1.),grid.size.height)),border));
    window.paint_quad(gpui::fill(Bounds::new(point(grid.left(),grid.bottom()-px(1.)),size(grid.size.width,px(1.))),border));
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
