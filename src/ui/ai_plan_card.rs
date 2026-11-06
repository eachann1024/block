use gpui::{AnyElement, CursorStyle, ElementId, FontWeight, HighlightStyle, MouseButton, SharedString, StyledText, div, prelude::*, px};
use crate::{editor::{ai::plan_link::{self, ActivePlanLink}, model::{Block, Kind, MarkKind, TableRow}}, icons::{icon, Icon}, theme::{Theme, tr}, ui::{radius, smoke_overlay}};

pub(crate) fn pill(id: impl Into<SharedString>, label: String, selected: bool,
    click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> gpui::Stateful<gpui::Div> {
    let theme = Theme::ocean(); let id = ElementId::from(id.into());
    div().id(id.clone()).relative().children(smoke_overlay::probe_for(&id)).px(px(10.)).py(px(5.))
        .rounded(radius::CONTROL).border_1().border_color(if selected { theme.accent } else { theme.btn_border })
        .bg(if selected { theme.selected } else { theme.paper }).cursor(CursorStyle::PointingHand)
        .text_size(px(14.)).child(label).on_mouse_down(MouseButton::Left, click)
}

pub(crate) fn chat_plan_card(title: String, badge: &'static str, note: String, pending: bool,
    operations: Vec<AnyElement>, actions: Vec<AnyElement>) -> impl IntoElement {
    let theme = Theme::ocean();
    div().relative().children(smoke_overlay::probe("ai-plan-card")).flex().flex_col().gap(px(8.)).p(px(12.))
        .rounded(radius::SURFACE).border_1().border_color(theme.btn_border).bg(theme.shell)
        .child(div().flex().items_center().gap(px(8.))
            .child(icon(Icon::Sparkles, px(14.), theme.mono_icon_colors(theme.accent)))
            .child(div().flex_1().min_w_0().text_size(px(14.)).font_weight(FontWeight(600.)).child(title))
            .child(div().px(px(6.)).py(px(2.)).rounded(radius::CONTROL).bg(if pending { theme.selected } else { theme.hover })
                .text_color(if pending { theme.accent } else { theme.text_secondary }).text_size(px(14.)).child(badge)))
        .child(div().flex().flex_wrap().gap(px(6.)).children(operations))
        .when(!note.is_empty(), |card| card.child(div().text_size(px(14.)).text_color(theme.text_secondary).child(note)))
        .child(div().flex().flex_wrap().gap(px(8.)).children(actions))
}

fn text(block: &Block) -> StyledText {
    let mut points = vec![0, block.text.len()];
    for span in &block.spans { points.extend([span.start, span.end]); }
    for tag in &block.meta.inline { points.extend([tag.start, tag.end]); }
    points.retain(|at| *at <= block.text.len() && block.text.is_char_boundary(*at));
    points.sort_unstable(); points.dedup();
    let mut highlights = Vec::new();
    for range in points.windows(2).map(|pair| pair[0]..pair[1]) {
        let bold = block.spans.iter().any(|span| span.start <= range.start && range.end <= span.end && span.marks.contains(&MarkKind::Bold));
        let yellow = block.meta.inline.iter().any(|tag| tag.start <= range.start && range.end <= tag.end && tag.attrs.iter().any(|(key, value)| key == "style" && value.contains("background-color") && value.contains("yellow")));
        if bold || yellow { highlights.push((range, HighlightStyle { font_weight: bold.then_some(FontWeight(700.)), background_color: yellow.then_some(gpui::rgb(0xfde6b3).into()), ..Default::default() })); }
    }
    StyledText::new(block.text.clone()).with_highlights(highlights)
}

fn row_equal(left: &TableRow, right: &TableRow) -> bool {
    left.cells.len() == right.cells.len() && left.cells.iter().zip(&right.cells).all(|(left, right)| {
        left.content.text == right.content.text && left.content.spans == right.content.spans && left.content.meta.inline == right.content.meta.inline
    })
}

fn table_row(row: &TableRow, change: i8) -> impl IntoElement {
    let theme = Theme::ocean();
    div().relative().when(change < 0, |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .flex().w_full().border_b_1().border_color(theme.hairline)
        .when(change < 0, |row| row.bg(gpui::hsla(0., 0.75, 0.5, 0.10)).line_through())
        .when(change > 0, |row| row.bg(gpui::hsla(0.38, 0.6, 0.4, 0.10)))
        .children(row.cells.iter().map(|cell| div().flex_1().min_w_0().p(px(8.)).border_r_1().border_color(theme.hairline)
            .when(cell.header, |cell| cell.font_weight(FontWeight(600.))).child(text(&cell.content))))
}

fn block_view(block: &Block, change: i8) -> AnyElement {
    let theme = Theme::ocean();
    if let Some(table) = &block.meta.table {
        return div().w_full().border_1().border_color(theme.hairline).children(table.rows.iter().map(|row| table_row(row, change))).into_any_element();
    }
    let marker = match block.kind { Kind::Bullet => "• ", Kind::Numbered => "1. ", Kind::Todo => "☐ ", Kind::Quote => "▏ ", _ => "" };
    div().relative().when(change < 0 && !block.text.is_empty(), |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .py(px(4.)).text_size(px(if block.kind == Kind::H1 { 24. } else if block.kind == Kind::H2 { 20. } else if block.kind == Kind::H3 { 17. } else { 16. }))
        .when(matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3), |row| row.font_weight(FontWeight(600.)))
        .when(change < 0, |row| row.bg(gpui::hsla(0., 0.75, 0.5, 0.10)).line_through())
        .when(change > 0, |row| row.bg(gpui::hsla(0.38, 0.6, 0.4, 0.10)))
        .child(marker).child(text(block)).into_any_element()
}

fn contents(link: &ActivePlanLink) -> impl IntoElement {
    let mut body = div().flex().flex_col().gap(px(6.));
    if !link.diff_mode { return body.children(link.after_blocks.iter().map(|block| block_view(block, 0))); }
    let before_table = link.before_blocks.iter().find_map(|block| block.meta.table.as_ref());
    let after_table = link.after_blocks.iter().find_map(|block| block.meta.table.as_ref());
    for block in &link.before_blocks {
        if let (Some(table), Some(after)) = (&block.meta.table, after_table) {
            for row in &table.rows { if !after.rows.iter().any(|other| row_equal(row, other)) { body = body.child(table_row(row, -1)); } }
        } else { body = body.child(block_view(block, -1)); }
    }
    for block in &link.after_blocks {
        if let (Some(table), Some(before)) = (&block.meta.table, before_table) {
            for row in &table.rows { body = body.child(table_row(row, if before.rows.iter().any(|other| row_equal(row, other)) { 0 } else { 1 })); }
        } else { body = body.child(block_view(block, 1)); }
    }
    body
}

pub(crate) fn editor_plan_review_card(link: ActivePlanLink) -> impl IntoElement {
    let theme = Theme::ocean();
    let title = link.after_blocks.iter().chain(&link.before_blocks).find(|block| matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3) && !block.text.is_empty())
        .map(|block| block.text.clone()).unwrap_or_else(|| link.page_label.clone());
    let accept = link.clone(); let reject = link.clone();
    div().relative().children(smoke_overlay::probe("ai-editor-plan-card")).flex().flex_col().gap(px(12.)).p(px(16.))
        .rounded(radius::SURFACE).border_1().border_dashed().border_color(theme.accent).bg(theme.paper)
        .child(div().flex().flex_wrap().items_center().justify_between().gap(px(8.))
            .child(div().flex().items_center().gap(px(8.)).font_weight(FontWeight(600.)).child(title)
                .child(div().px(px(6.)).py(px(2.)).rounded(radius::CONTROL).bg(theme.selected).text_color(theme.accent).text_size(px(14.)).child(tr("AI 建议修改块", "AI suggested change"))))
            .child(div().flex().gap(px(4.))
                .child(pill("ai-plan-mode-clean", tr("修改后预览", "Preview").into(), !link.diff_mode, |_, _, cx| plan_link::set_mode(false, cx)))
                .child(pill("ai-plan-mode-diff", tr("增删对比", "Compare").into(), link.diff_mode, |_, _, cx| plan_link::set_mode(true, cx)))))
        .child(contents(&link))
        .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-dock")).p(px(10.)).rounded(px(10.)).bg(theme.shell)
            .border_1().border_color(theme.accent).flex().flex_wrap().items_center().justify_between().gap(px(10.))
            .child(div().flex().items_center().gap(px(8.))
                .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-step")).px(px(6.)).rounded(radius::CONTROL).bg(theme.selected)
                    .child(if crate::theme::is_en() { format!("Change {} / {}", link.op_idx + 1, link.total_ops) } else { format!("修改 {} / {}", link.op_idx + 1, link.total_ops) }))
                .child(link.summary.clone()))
            .child(div().flex().gap(px(6.))
                .child(pill("ai-plan-inline-reject", tr("拒绝此项", "Reject change").into(), false, move |_, _, cx| plan_link::decide(reject.clone(), false, cx)))
                .child(pill("ai-plan-inline-accept", tr("接受修改", "Accept change").into(), true, move |_, _, cx| plan_link::decide(accept.clone(), true, cx)))))
}
