use gpui::{AnyElement, CursorStyle, FontWeight, HighlightStyle, IntoElement, MouseButton, SharedString, StyledText, div, prelude::*, px};
use crate::{editor::{ai::plan_link::{self, ActivePlanLink}, model::{Block, Kind, MarkKind, TableRow}}, theme::tr, ui::{radius, smoke_overlay}};
use super::ai_plan_card::pill;

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
        if bold || yellow {
            let dark = crate::theme::is_dark();
            highlights.push((range, HighlightStyle { font_weight: bold.then_some(FontWeight(700.)),
                color: yellow.then_some(gpui::rgb(if dark { 0xfff3d4 } else { 0x302511 }).into()),
                background_color: yellow.then_some(gpui::rgb(if dark { 0x453a1e } else { 0xfde6b3 }).into()), ..Default::default() }));
        }
    }
    StyledText::new(block.text.clone()).with_highlights(highlights)
}

fn table_row(row: &TableRow, change: i8, header: Option<bool>) -> impl IntoElement {
    let theme = crate::ui::ai_theme::theme();
    div().relative().when(change < 0, |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .flex().w_full().border_b_1().border_color(theme.hairline)
        .when(change < 0, |row| row.bg(gpui::hsla(0., 0.75, 0.5, 0.10)).line_through())
        .when(change > 0, |row| row.bg(gpui::hsla(0.38, 0.6, 0.4, 0.10)))
        .children(row.cells.iter().map(|cell| div().flex_1().min_w_0().p(px(8.)).border_r_1().border_color(theme.hairline)
            .when(header.unwrap_or(cell.header), |cell| cell.font_weight(FontWeight(600.))).child(text(&cell.content))))
}

fn block_view(block: &Block, change: i8) -> AnyElement {
    let theme = crate::ui::ai_theme::theme();
    if let Some(table) = &block.meta.table {
        return div().w_full().border_1().border_color(theme.hairline).children(table.rows.iter().map(|row| table_row(row, change, None))).into_any_element();
    }
    let marker = match block.kind { Kind::Bullet => "• ", Kind::Numbered => "1. ", Kind::Todo => "☐ ", Kind::Quote => "▏ ", _ => "" };
    div().relative().when(change < 0 && !block.text.is_empty(), |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .py(px(4.)).text_size(px(if block.kind == Kind::H1 { 24. } else if block.kind == Kind::H2 { 20. } else if block.kind == Kind::H3 { 17. } else { 16. }))
        .when(matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3), |row| row.font_weight(FontWeight(600.)))
        .when(change < 0, |row| row.bg(gpui::hsla(0., 0.75, 0.5, 0.10)).line_through())
        .when(change > 0, |row| row.bg(gpui::hsla(0.38, 0.6, 0.4, 0.10)))
        .child(marker).child(text(block)).into_any_element()
}

fn line_view(source: &str, header: bool) -> AnyElement {
    let source = source.trim_end_matches(['\r', '\n']);
    if source.trim().is_empty() { return div().h(px(20.)).into_any_element(); }
    if source.starts_with('|') {
        let count = source.chars().filter(|ch| *ch == '|').count().saturating_sub(1).max(1);
        let separator = format!("|{}|", vec![" --- "; count].join("|"));
        let doc = crate::editor::markdown::parse(&format!("{source}\n{separator}\n"));
        if let Some(row) = doc.blocks.iter().find_map(|block| block.meta.table.as_ref()).and_then(|table| table.rows.first()) {
            return table_row(row, 0, Some(header)).into_any_element();
        }
    }
    let doc = crate::editor::markdown::parse(source);
    if let Some(block) = doc.blocks.iter().next() { return block_view(block, 0); }
    div().h(px(12.)).into_any_element()
}

fn contents(link: &ActivePlanLink) -> impl IntoElement {
    let mut body = div().flex().flex_col().gap(px(6.));
    if !link.diff_mode {
        let selected = plan_link::selection::selected(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text).unwrap_or_else(|_| link.after_text.clone());
        let before = crate::editor::markdown::parse(&link.before_text);
        let after = crate::editor::markdown::parse(&selected);
        let before_blocks: Vec<_> = before.blocks.iter().collect();
        let after_blocks: Vec<_> = after.blocks.iter().collect();
        let same = |left: &&Block, right: &&Block| left.kind == right.kind && left.text == right.text && left.spans == right.spans && left.checked == right.checked && left.meta == right.meta;
        let prefix = before_blocks.iter().zip(&after_blocks).take_while(|(left, right)| same(left, right)).count();
        let suffix = before_blocks[prefix..].iter().rev().zip(after_blocks[prefix..].iter().rev()).take_while(|(left, right)| same(left, right)).count();
        if prefix == after_blocks.len() - suffix {
            return body.child(div().text_size(px(12.)).text_color(crate::ui::ai_theme::theme().text_secondary)
                .child(tr("未选择修改，保留原文", "No changes selected; original remains")));
        }
        return body.children(after_blocks[prefix..after_blocks.len() - suffix].iter().map(|block| block_view(block, 0)));
    }
    let lines = match plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text) {
        Ok(lines) => lines,
        Err(_) => return body.child(div().text_size(px(12.)).child(tr("改动过大，仅支持完整预览和应用", "Change too large for individual selection; preview and apply all")))
            .children(link.after_blocks.iter().map(|block| block_view(block, 0))),
    };
    let removed = lines.iter().filter(|line| line.change < 0 && line.included).map(|line| line.selection_index).collect::<std::collections::HashSet<_>>().len();
    let added = lines.iter().filter(|line| line.change > 0 && line.included).map(|line| line.selection_index).collect::<std::collections::HashSet<_>>().len();
    let mut seen_removed = false;
    let mut seen_added = false;
    let mut seen_table_removed = false;
    let mut seen_table_added = false;
    let first = lines.iter().position(|line| line.change != 0).unwrap_or(lines.len());
    let last = lines.iter().rposition(|line| line.change != 0).unwrap_or(first);
    let table_header = lines.iter().enumerate().skip(first).take(last.saturating_sub(first) + 1)
        .find(|(_, line)| line.change != 0 && line.text.trim_start().starts_with('|'))
        .and_then(|(at, _)| (1..at).rev().take_while(|&index| lines[index].text.trim_start().starts_with('|')).find(|&index| {
            let value = lines[index].text.trim();
            value.starts_with('|') && value.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':'))
                && lines[index - 1].text.trim_start().starts_with('|')
        }).map(|index| index - 1))
        .filter(|index| *index < first);
    for line in &lines {
        if (line.index < first || line.index > last) && Some(line.index) != table_header { continue; }
        let trimmed = line.text.trim();
        if trimmed.starts_with('|') && trimmed.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':')) { continue; }
        let index = line.index;
        let selection_index = line.selection_index;
        let row = div().id(SharedString::from(format!("ai-plan-diff-row-{index}"))).relative()
            .when(line.change < 0, |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
            .when(line.change < 0 && !seen_removed, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-remove")))
            .when(line.change > 0 && !seen_added, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-add")))
            .when(trimmed.starts_with('|') && line.change < 0 && !seen_table_removed, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-table-remove")))
            .when(trimmed.starts_with('|') && line.change > 0 && !seen_table_added, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-table-add")))
            .when(line.change != 0, |row| row.children(smoke_overlay::probe(format!("ai-plan-diff-{}-{index}", if line.change < 0 { "remove" } else { "add" }))))
            .flex().items_center().gap(px(6.))
            .when(line.change != 0, |row| row.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                cx.stop_propagation(); plan_link::toggle_diff(selection_index, cx);
            }))
            .when(line.included && line.change < 0, |row| row.bg(gpui::hsla(0., 0.75, 0.5, 0.10)))
            .when(line.included && line.change > 0, |row| row.bg(gpui::hsla(0.38, 0.6, 0.4, 0.10)))
            .child(div().flex_1().min_w_0().child(line_view(&line.text, lines.get(index + 1).is_some_and(|next| {
                let value = next.text.trim(); value.starts_with('|') && value.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':'))
            }))))
            .when(line.change != 0, |row| row.child(div().size(px(18.)).flex_none().rounded(px(5.))
                .border_1().border_color(crate::ui::ai_theme::theme().btn_border).flex().items_center().justify_center()
                .when(line.included, |mark| mark.bg(if line.change < 0 { gpui::rgb(0xce4a4a) } else { gpui::rgb(0x288655) })
                    .child(crate::icons::icon(crate::icons::Icon::Check, px(12.), crate::ui::ai_theme::theme().mono_icon_colors(gpui::rgb(0xffffff).into()))))));
        body = body.child(row);
        seen_removed |= line.change < 0;
        seen_added |= line.change > 0;
        seen_table_removed |= trimmed.starts_with('|') && line.change < 0;
        seen_table_added |= trimmed.starts_with('|') && line.change > 0;
    }
    body.child(div().relative().children(smoke_overlay::probe("ai-plan-diff-counts"))
        .text_size(px(12.)).text_color(crate::ui::ai_theme::theme().text_secondary)
        .child(format!("−{removed}  +{added}")))
}

pub(crate) fn editor_plan_review_card(link: ActivePlanLink) -> impl IntoElement {
    let theme = crate::ui::ai_theme::theme();
    let title = if link.is_create { format!("{} · {}", tr("新建笔记", "New note"), link.page_label) } else {
        link.after_blocks.iter().chain(&link.before_blocks).find(|block| matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3) && !block.text.is_empty())
            .map(|block| block.text.clone()).unwrap_or_else(|| link.page_label.clone())
    };
    let accept = link.clone(); let reject = link.clone();
    div().relative().children(smoke_overlay::probe("ai-editor-plan-card")).flex().flex_col().gap(px(12.)).p(px(16.))
        .rounded(px(10.)).border_1().border_color(theme.hairline).bg(theme.paper)
        .child(div().flex().flex_wrap().items_center().justify_between().gap(px(8.))
            .child(div().font_weight(FontWeight(600.)).child(title))
            .child(div().flex().gap(px(4.))
                .child(pill("ai-plan-mode-clean", tr("修改后预览", "Preview").into(), !link.diff_mode, |_, _, cx| plan_link::set_mode(false, cx)))
                .child(pill("ai-plan-mode-diff", tr("增删对比", "Compare").into(), link.diff_mode, |_, _, cx| plan_link::set_mode(true, cx)))))
        .child(contents(&link))
        .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-dock")).p(px(10.)).rounded(px(10.)).bg(theme.shell)
            .border_1().border_color(theme.hairline).flex().flex_wrap().items_center().justify_between().gap(px(10.))
            .child(div().flex().items_center().gap(px(8.))
                .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-step")).px(px(6.)).rounded(radius::CONTROL).bg(theme.selected)
                    .child(if link.page_ops > 1 {
                        if crate::theme::is_en() { format!("{} changes on this page", link.page_ops) } else { format!("本页 {} 项修改", link.page_ops) }
                    } else if crate::theme::is_en() { format!("Change {} / {}", link.op_idx + 1, link.total_ops) } else { format!("修改 {} / {}", link.op_idx + 1, link.total_ops) }))
                .child(link.summary.clone()))
            .child(div().flex().gap(px(6.))
                .child(pill("ai-plan-inline-reject", if link.is_create { tr("关闭预览", "Close preview") } else { tr("拒绝此项", "Reject change") }.into(), false, move |_, _, cx| plan_link::decide(reject.clone(), false, cx)))
                .child(pill("ai-plan-inline-accept", if link.total_ops > 1 { tr("接受全部修改", "Accept all changes") } else { tr("接受修改", "Accept change") }.into(), true, move |_, _, cx| plan_link::decide(accept.clone(), true, cx)))))
}
