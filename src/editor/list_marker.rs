//! List chrome: depth-aware bullets, accent numbers, todo boxes and nesting guide lines.
use gpui::{AnyElement, CursorStyle, Div, FontWeight, Hsla, MouseButton, div, prelude::*, px, rgb};
use crate::{icons::{Icon, IconColors, icon}, theme::{Theme, is_dark}};
use super::{chrome::{HANDLE_WIDTH, INDENT_PER_CHAR, MARKER_WIDTH, ROW_GAP}, model::{Block, Kind}, view::Editor};

/// Distance from a marker column's left edge to its bullet centre; guides hang from here.
const MARK_CENTER: f32 = MARKER_WIDTH - 11.;
/// Todo boxes sit left in the column (4.8px inset + half of 16px).
const TODO_CENTER: f32 = 12.8;
/// Bounded look-back so long flat lists stay cheap to render.
const MAX_SCAN: usize = 256;

pub(super) struct ListCtx { pub number: Option<u64>, pub ancestors: Vec<Ancestor> }
/// An enclosing list item: its indent and where its marker's centre sits in the column.
#[derive(Clone, Copy)]
pub(super) struct Ancestor { indent: usize, center: f32 }

pub(super) fn is_list(block: &Block) -> bool { matches!(block.kind, Kind::Bullet | Kind::Numbered | Kind::Todo) }

pub(super) fn ctx(this: &Editor, index: usize) -> ListCtx {
    let block = &this.doc.blocks[index];
    let number = (block.kind == Kind::Numbered).then(|| this.doc.list_number(block.id)).flatten();
    // Indented continuation content inside an item also carries the guides.
    let nested = is_list(block) || block.meta.indent > 0;
    ListCtx { number, ancestors: if nested { ancestors(this, index) } else { Vec::new() } }
}

/// Indents of the enclosing list items, outermost last.
fn ancestors(this: &Editor, index: usize) -> Vec<Ancestor> {
    let mut out = Vec::new();
    let mut threshold = this.doc.blocks[index].meta.indent;
    for prev in (index.saturating_sub(MAX_SCAN)..index).rev() {
        if threshold == 0 { break; }
        let b = &this.doc.blocks[prev];
        if b.meta.list_code.is_some() { continue; }
        if !is_list(b) {
            if b.meta.indent > 0 { continue; }
            break;
        }
        if b.meta.indent < threshold { out.push(Ancestor { indent: b.meta.indent, center: if b.kind == Kind::Todo { TODO_CENTER } else { MARK_CENTER } }); threshold = b.meta.indent; }
    }
    out
}

/// Bullet or number for the marker column; `None` for todos and plain blocks.
pub(super) fn marker(block: &Block, list: &ListCtx, size: f32, line: f32, zoom: f32) -> Option<AnyElement> {
    let theme = Theme::ocean();
    let depth = list.ancestors.len();
    let cell = div().h(px(line)).flex().items_center();
    match block.kind {
        Kind::Bullet => {
            let d = if depth == 0 { 6. } else { 5. };
            let dot = div().size(px(d * zoom)).rounded_full();
            let dot = match depth { 0 => dot.bg(theme.accent), 1 => dot.bg(bullet_color()), _ => dot.border(px(1.25 * zoom)).border_color(bullet_color()) };
            Some(cell.pr(px((11. - d / 2.) * zoom)).child(dot).into_any_element())
        }
        Kind::Numbered => Some(cell.pr(px(8. * zoom))
            .font_family(crate::theme::mono_font_family()).text_size(px(size * 0.84)).font_weight(FontWeight::MEDIUM)
            .text_color(if depth == 0 { theme.accent } else { theme.text_secondary })
            .child(list.number.unwrap_or(1).to_string()).into_any_element()),
        _ => None,
    }
}

/// One hairline per enclosing item, spanning the whole row so consecutive rows join.
pub(super) fn guides(list: &ListCtx, zoom: f32) -> impl Iterator<Item = Div> + '_ {
    let color: Hsla = if is_dark() { rgb(0x333333).into() } else { rgb(0xe6e4df).into() };
    list.ancestors.iter().map(move |a| {
        let x = HANDLE_WIDTH + ROW_GAP + (a.indent.min(12) as f32 * INDENT_PER_CHAR + a.center) * zoom;
        div().absolute().top_0().bottom_0().left(px(x.round())).w(px(1.)).bg(color)
    })
}

fn bullet_color() -> Hsla { if is_dark() { rgb(0xa9a8a4).into() } else { rgb(0x6f6e69).into() } }

/// Paper-tag checkbox: tinted fill with a hairline ring; checked shows an accent tick.
pub(super) fn todo_box(checked: bool, top: f32, zoom: f32, toggle: impl Fn(&mut gpui::App) + 'static) -> impl IntoElement {
    let dark = is_dark();
    let (bg, ring, hover): (Hsla, Hsla, Hsla) = if dark { (rgb(0x242424).into(), rgb(0x3a3a3a).into(), rgb(0x5a5a5a).into()) }
        else { (rgb(0xf3f1ec).into(), rgb(0xdedcd6).into(), rgb(0xc4c2bb).into()) };
    let accent = Theme::ocean().accent;
    div().mt(px(top)).ml(px(4.8 * zoom)).size(px(16. * zoom)).relative().children(crate::ui::smoke_overlay::probe("todo-checkbox"))
        .flex().items_center().justify_center().rounded(px(5. * zoom)).cursor(CursorStyle::PointingHand)
        .bg(bg).border_1().border_color(ring).hover(move |s| s.border_color(hover))
        .when(checked, |b| b.child(icon(Icon::Check, px(12. * zoom), IconColors { line: accent, fill: gpui::transparent_black(), accent })))
        .on_mouse_down(MouseButton::Left, move |_, _, cx| { toggle(cx); cx.stop_propagation(); })
}
