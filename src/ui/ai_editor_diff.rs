use gpui::{AnyElement, CursorStyle, ElementId, FontWeight, HighlightStyle, Hsla, IntoElement, MouseButton, SharedString, StyledText, div, prelude::*, px, rgb};
use crate::{
    editor::{ai::plan_link::{self, ActivePlanLink}, model::{Block, Kind, MarkKind, TableRow}},
    icons::{Icon, icon},
    theme::{is_dark, tr},
    ui::{radius, smoke_overlay},
};
use super::ai_plan_card::pill;

struct DiffColors {
    del_bg: Hsla, del_border: Hsla, del_text: Hsla, del_badge: Hsla,
    add_bg: Hsla, add_border: Hsla, add_text: Hsla, add_badge: Hsla, code_bg: Hsla,
}

fn palette() -> DiffColors {
    if is_dark() {
        DiffColors {
            del_bg: rgb(0x2a1c1e).into(), del_border: rgb(0xd95858).into(), del_text: rgb(0xf5a3a3).into(), del_badge: rgb(0x3a2022).into(),
            add_bg: rgb(0x182a21).into(), add_border: rgb(0x38a169).into(), add_text: rgb(0x9be4b8).into(), add_badge: rgb(0x1d3528).into(),
            code_bg: rgb(0x202022).into(),
        }
    } else {
        DiffColors {
            del_bg: rgb(0xfdf2f2).into(), del_border: rgb(0xd94848).into(), del_text: rgb(0xb42318).into(), del_badge: rgb(0xfbe9e7).into(),
            add_bg: rgb(0xeffaf3).into(), add_border: rgb(0x288655).into(), add_text: rgb(0x087443).into(), add_badge: rgb(0xe3f6ec).into(),
            code_bg: rgb(0xf6f5f2).into(),
        }
    }
}

fn text(block: &Block) -> StyledText {
    let mut points = vec![0, block.text.len()];
    for span in &block.spans { points.extend([span.start, span.end]); }
    for tag in &block.meta.inline { points.extend([tag.start, tag.end]); }
    points.retain(|at| *at <= block.text.len() && block.text.is_char_boundary(*at));
    points.sort_unstable(); points.dedup();
    let mut highlights = Vec::new();
    let dark = is_dark();
    for range in points.windows(2).map(|pair| pair[0]..pair[1]) {
        let bold = block.spans.iter().any(|span| span.start <= range.start && range.end <= span.end && span.marks.contains(&MarkKind::Bold));
        let code = block.spans.iter().any(|span| span.start <= range.start && range.end <= span.end && span.marks.contains(&MarkKind::Code));
        let yellow = block.meta.inline.iter().any(|tag| tag.start <= range.start && range.end <= tag.end && tag.attrs.iter().any(|(key, value)| key == "style" && value.contains("background-color") && value.contains("yellow")));
        if bold || code || yellow {
            highlights.push((range, HighlightStyle {
                font_weight: bold.then_some(FontWeight(650.)),
                color: yellow.then(|| rgb(if dark { 0xfff3d4 } else { 0x302511 }).into()),
                background_color: if yellow { Some(rgb(if dark { 0x453a1e } else { 0xfde6b3 }).into()) }
                    else if code { Some(rgb(if dark { 0x2c2c2e } else { 0xf1efe9 }).into()) } else { None },
                ..Default::default()
            }));
        }
    }
    StyledText::new(block.text.clone()).with_highlights(highlights)
}

fn table_row(row: &TableRow, change: i8, header: Option<bool>) -> impl IntoElement {
    let (theme, pal) = (crate::ui::ai_theme::theme(), palette());
    let (is_header, count) = (header.unwrap_or_else(|| row.cells.iter().any(|c| c.header)), row.cells.len());
    div().relative().when(change < 0, |el| el.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .flex().w_full().border_b_1().border_color(theme.hairline)
        .when(is_header && change == 0, |el| el.bg(theme.shell))
        .when(change < 0, |el| el.bg(pal.del_bg).text_color(pal.del_text).line_through())
        .when(change > 0, |el| el.bg(pal.add_bg))
        .children(row.cells.iter().enumerate().map(|(idx, cell)| {
            div().flex_1().min_w_0().overflow_hidden().px(px(8.)).py(px(6.)).text_size(px(13.5)).line_height(px(20.))
                .when(idx + 1 < count, |el| el.border_r_1().border_color(theme.hairline))
                .when(header.unwrap_or(cell.header), |el| el.font_weight(FontWeight(600.)))
                .child(text(&cell.content))
        }))
}

fn block_view(block: &Block, change: i8) -> AnyElement {
    let (theme, pal) = (crate::ui::ai_theme::theme(), palette());
    if let Some(table) = &block.meta.table {
        return div().w_full().rounded(px(8.)).border_1().border_color(theme.hairline).overflow_hidden()
            .children(table.rows.iter().map(|row| table_row(row, change, None))).into_any_element();
    }
    let is_heading = matches!(block.kind, Kind::H1 | Kind::H2 | Kind::H3);
    let (font_size, line_h) = match block.kind {
        Kind::H1 => (20., 28.), Kind::H2 => (17., 24.), Kind::H3 => (15.5, 22.), _ => (14.5, 22.),
    };
    let is_tsv = block.kind == Kind::Paragraph && block.text.matches('\t').count() >= 2;
    let is_long_token = block.kind == Kind::Paragraph && (block.text.contains("----") || block.text.split_whitespace().any(|w| w.len() > 56));
    let content: AnyElement = if is_tsv {
        let cells: Vec<_> = block.text.split('\t').map(str::trim).filter(|s| !s.is_empty()).collect();
        div().flex().flex_wrap().items_center().gap(px(6.)).py(px(2.)).children(cells.into_iter().map(|cell| {
            div().px(px(7.)).py(px(2.)).rounded(px(5.)).bg(theme.shell).border_1().border_color(theme.hairline)
                .text_size(px(12.5)).line_height(px(18.)).child(cell.to_owned())
        })).into_any_element()
    } else if is_long_token {
        div().w_full().px(px(10.)).py(px(6.)).rounded(px(6.)).bg(pal.code_bg).border_1().border_color(theme.hairline)
            .font_family("JetBrains Mono").text_size(px(12.5)).line_height(px(19.)).child(text(block)).into_any_element()
    } else if block.kind == Kind::Todo {
        let checked = block.checked;
        div().flex().items_start().gap(px(8.))
            .child(div().mt(px(3.5)).size(px(15.)).flex_none().rounded(px(4.)).border_1()
                .border_color(if checked { theme.accent } else { theme.btn_border })
                .when(checked, |b| b.bg(theme.accent).flex().items_center().justify_center()
                    .child(icon(Icon::Check, px(10.), theme.mono_icon_colors(rgb(0xffffff).into())))))
            .child(div().flex_1().min_w_0().child(text(block))).into_any_element()
    } else if matches!(block.kind, Kind::Bullet | Kind::Numbered) {
        div().flex().items_baseline().gap(px(6.))
            .child(div().flex_none().text_color(theme.text_secondary).child(if block.kind == Kind::Bullet { "•" } else { "1." }))
            .child(div().flex_1().min_w_0().child(text(block))).into_any_element()
    } else if block.kind == Kind::Quote {
        div().border_l_2().border_color(theme.accent).pl(px(10.)).text_color(theme.text_secondary).child(text(block)).into_any_element()
    } else {
        div().w_full().min_w_0().child(text(block)).into_any_element()
    };
    div().relative().when(change < 0 && !block.text.is_empty(), |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
        .py(px(2.)).text_size(px(font_size)).line_height(px(line_h))
        .when(is_heading, |row| row.font_weight(FontWeight(600.)))
        .when(change < 0, |row| row.text_color(pal.del_text).line_through())
        .child(content).into_any_element()
}

fn line_view(source: &str, header: bool, change: i8) -> AnyElement {
    let trimmed = source.trim_end_matches(['\r', '\n']).trim();
    if trimmed.is_empty() { return div().h(px(6.)).into_any_element(); }
    if trimmed.starts_with('|') {
        let count = trimmed.chars().filter(|ch| *ch == '|').count().saturating_sub(1).max(1);
        let separator = format!("|{}|", vec![" --- "; count].join("|"));
        let doc = crate::editor::markdown::parse(&format!("{trimmed}\n{separator}\n"));
        if let Some(row) = doc.blocks.iter().find_map(|block| block.meta.table.as_ref()).and_then(|table| table.rows.first()) {
            return table_row(row, 0, Some(header)).into_any_element();
        }
    }
    let doc = crate::editor::markdown::parse(source.trim_end_matches(['\r', '\n']));
    if let Some(block) = doc.blocks.iter().next() { return block_view(block, change); }
    div().h(px(8.)).into_any_element()
}

fn mode_segment(id: &'static str, label: &'static str, selected: bool, click: impl Fn(&gpui::MouseDownEvent, &mut gpui::Window, &mut gpui::App) + 'static) -> impl IntoElement {
    let (theme, elem_id) = (crate::ui::ai_theme::theme(), ElementId::from(id));
    div().id(elem_id.clone()).relative().children(smoke_overlay::probe_for(&elem_id))
        .h(px(26.)).px(px(10.)).rounded(px(6.)).flex().items_center().justify_center()
        .cursor(CursorStyle::PointingHand).text_size(px(13.)).font_weight(FontWeight(500.))
        .when(selected, |el| el.bg(theme.selected).border_1().border_color(theme.accent).text_color(theme.text))
        .when(!selected, |el| el.text_color(theme.text_secondary).hover(|s| s.bg(theme.hover).text_color(theme.text)))
        .on_mouse_down(MouseButton::Left, click).child(label)
}

fn diff_counts(link: &ActivePlanLink) -> Option<(usize, usize)> {
    let lines = plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text).ok()?;
    let removed = lines.iter().filter(|l| l.change < 0 && l.included && !l.text.trim().is_empty()).map(|l| l.selection_index).collect::<std::collections::HashSet<_>>().len();
    let added = lines.iter().filter(|l| l.change > 0 && l.included && !l.text.trim().is_empty()).map(|l| l.selection_index).collect::<std::collections::HashSet<_>>().len();
    Some((removed, added))
}

fn contents(link: &ActivePlanLink) -> impl IntoElement {
    let (theme, pal) = (crate::ui::ai_theme::theme(), palette());
    let mut body = div().flex().flex_col().gap(px(8.));
    if !link.diff_mode {
        let selected = plan_link::selection::selected(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text).unwrap_or_else(|_| link.after_text.clone());
        let (before, after) = (crate::editor::markdown::parse(&link.before_text), crate::editor::markdown::parse(&selected));
        let (before_blocks, after_blocks): (Vec<_>, Vec<_>) = (before.blocks.iter().collect(), after.blocks.iter().collect());
        let same = |l: &&Block, r: &&Block| l.kind == r.kind && l.text == r.text && l.spans == r.spans && l.checked == r.checked && l.meta == r.meta;
        let prefix = before_blocks.iter().zip(&after_blocks).take_while(|(l, r)| same(l, r)).count();
        let suffix = before_blocks[prefix..].iter().rev().zip(after_blocks[prefix..].iter().rev()).take_while(|(l, r)| same(l, r)).count();
        if prefix == after_blocks.len() - suffix {
            return body.child(div().py(px(6.)).text_size(px(13.)).text_color(theme.text_secondary).child(tr("未选择修改，保留原文", "No changes selected; original remains")));
        }
        return body.children(after_blocks[prefix..after_blocks.len() - suffix].iter().map(|block| block_view(block, 0)));
    }
    let Ok(lines) = plan_link::selection::diff(link.conv_id, link.msg_at, &link.page, &link.before_text, &link.after_text) else {
        return body.child(div().text_size(px(13.)).text_color(theme.text_secondary).child(tr("改动过大，仅支持完整预览和应用", "Change too large for individual selection; preview and apply all")))
            .children(link.after_blocks.iter().map(|block| block_view(block, 0)));
    };
    let (mut seen_removed, mut seen_added, mut seen_table_removed, mut seen_table_added) = (false, false, false, false);
    let first = lines.iter().position(|line| line.change != 0 && !line.text.trim().is_empty()).unwrap_or(lines.len());
    let last = lines.iter().rposition(|line| line.change != 0 && !line.text.trim().is_empty()).unwrap_or(first);
    let table_header = lines.iter().enumerate().skip(first).take(last.saturating_sub(first) + 1)
        .find(|(_, line)| line.change != 0 && line.text.trim_start().starts_with('|'))
        .and_then(|(at, _)| (1..at).rev().take_while(|&index| lines[index].text.trim_start().starts_with('|')).find(|&index| {
            let value = lines[index].text.trim();
            value.starts_with('|') && value.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':')) && lines[index - 1].text.trim_start().starts_with('|')
        }).map(|index| index - 1)).filter(|index| *index < first);
    let visible: Vec<_> = lines.iter().filter(|line| {
        if (line.index < first || line.index > last) && Some(line.index) != table_header { return false; }
        let trimmed = line.text.trim();
        !trimmed.is_empty() && !(trimmed.starts_with('|') && trimmed.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':')))
    }).collect();
    let total_visible = visible.len();
    let mut list = div().w_full().rounded(px(10.)).border_1().border_color(theme.hairline).bg(theme.paper).overflow_hidden().flex().flex_col();
    for (pos, line) in visible.into_iter().enumerate() {
        let (trimmed, index, selection_index) = (line.text.trim(), line.index, line.selection_index);
        let is_table = trimmed.starts_with('|');
        let is_header = lines.get(index + 1).is_some_and(|next| {
            let v = next.text.trim(); v.starts_with('|') && v.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':'))
        });
        let row_bg = if !line.included { theme.paper } else if line.change < 0 { pal.del_bg } else if line.change > 0 { pal.add_bg } else if is_header { theme.shell } else { theme.paper };
        let left_bar = if !line.included && line.change != 0 { theme.btn_border } else if line.change < 0 { pal.del_border } else if line.change > 0 { pal.add_border } else { gpui::transparent_black() };
        let gutter_symbol = if line.change < 0 { "−" } else if line.change > 0 { "+" } else { "" };
        let gutter_color = if !line.included { theme.text_secondary } else if line.change < 0 { pal.del_text } else { pal.add_text };
        let row = div().id(SharedString::from(format!("ai-plan-diff-row-{index}"))).relative()
            .when(line.change < 0, |row| row.children(smoke_overlay::probe("ai-plan-diff-del-row")))
            .when(line.change < 0 && !seen_removed, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-remove")))
            .when(line.change > 0 && !seen_added, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-add")))
            .when(is_table && line.change < 0 && !seen_table_removed, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-table-remove")))
            .when(is_table && line.change > 0 && !seen_table_added, |row| row.children(smoke_overlay::probe("ai-plan-diff-first-table-add")))
            .when(line.change != 0, |row| row.children(smoke_overlay::probe(format!("ai-plan-diff-{}-{index}", if line.change < 0 { "remove" } else { "add" }))))
            .flex().items_center().w_full().bg(row_bg).border_l(px(3.)).border_color(left_bar)
            .when(pos + 1 < total_visible, |row| row.border_b_1().border_color(theme.hairline))
            .when(line.change != 0, |row| row.cursor(CursorStyle::PointingHand).on_mouse_down(MouseButton::Left, move |_, _, cx| {
                cx.stop_propagation(); plan_link::toggle_diff(selection_index, cx);
            }))
            .child(div().w(px(26.)).flex_none().flex().items_center().justify_center()
                .font_family("JetBrains Mono").text_size(px(13.)).font_weight(FontWeight(600.)).text_color(gutter_color).child(gutter_symbol))
            .child(div().flex_1().min_w_0().overflow_hidden()
                .when(!is_table, |el| el.py(px(6.)).pr(px(8.)))
                .when(line.change > 0 && !line.included, |el| el.text_color(theme.text_secondary).line_through())
                .child(line_view(&line.text, is_header, if line.included { line.change } else { 0 })))
            .when(line.change != 0, |row| row.child(div().flex_none().px(px(10.)).flex().items_center().gap(px(6.))
                .when(!line.included, |el| el.child(div().px(px(5.)).py(px(1.)).rounded(px(4.)).bg(theme.shell).text_size(px(11.5)).text_color(theme.text_secondary)
                    .child(if line.change < 0 { tr("保留", "Kept") } else { tr("跳过", "Skipped") })))
                .child(div().size(px(16.)).flex_none().rounded(px(4.)).border_1()
                    .border_color(if line.included { theme.accent } else { theme.btn_border })
                    .bg(if line.included { theme.accent } else { theme.paper }).flex().items_center().justify_center()
                    .when(line.included, |mark| mark.child(icon(Icon::Check, px(11.), theme.mono_icon_colors(rgb(0xffffff).into())))))));
        list = list.child(row);
        seen_removed |= line.change < 0; seen_added |= line.change > 0;
        seen_table_removed |= is_table && line.change < 0; seen_table_added |= is_table && line.change > 0;
    }
    body.child(list)
}

pub(crate) fn editor_plan_review_card(link: ActivePlanLink) -> impl IntoElement {
    let (theme, pal) = (crate::ui::ai_theme::theme(), palette());
    let title = if link.is_create { format!("{} · {}", tr("新建笔记", "New note"), link.page_label) } else {
        link.after_blocks.iter().chain(&link.before_blocks).find(|b| matches!(b.kind, Kind::H1 | Kind::H2 | Kind::H3) && !b.text.is_empty())
            .map(|b| b.text.clone()).unwrap_or_else(|| link.page_label.clone())
    };
    let (counts, accept, reject) = (diff_counts(&link), link.clone(), link.clone());
    div().relative().children(smoke_overlay::probe("ai-editor-plan-card"))
        .flex().flex_col().rounded(px(12.)).border_1().border_color(theme.accent).bg(theme.paper).overflow_hidden()
        .child(div().px(px(14.)).py(px(10.)).bg(theme.shell).border_b_1().border_color(theme.hairline)
            .flex().flex_wrap().items_center().justify_between().gap(px(10.))
            .child(div().flex().items_center().gap(px(8.)).min_w_0()
                .child(div().px(px(7.)).py(px(2.)).rounded(px(5.)).bg(theme.selected).border_1().border_color(theme.accent)
                    .flex().items_center().gap(px(4.)).text_size(px(12.)).font_weight(FontWeight(500.)).text_color(theme.accent)
                    .child(icon(Icon::Sparkles, px(12.), theme.mono_icon_colors(theme.accent))).child(tr("AI 审阅", "AI Review")))
                .child(div().text_size(px(14.5)).font_weight(FontWeight(600.)).text_color(theme.text).child(title))
                .when_some(counts, |el, (removed, added)| el.child(div().relative().children(smoke_overlay::probe("ai-plan-diff-counts"))
                    .flex().items_center().gap(px(4.)).font_family("JetBrains Mono").text_size(px(12.)).font_weight(FontWeight(500.))
                    .child(div().px(px(6.)).py(px(1.)).rounded(px(4.)).bg(pal.del_badge).text_color(pal.del_text).child(format!("−{removed}")))
                    .child(div().px(px(6.)).py(px(1.)).rounded(px(4.)).bg(pal.add_badge).text_color(pal.add_text).child(format!("+{added}"))))))
            .child(div().p(px(2.)).rounded(px(8.)).bg(theme.paper).border_1().border_color(theme.btn_border).flex().items_center().gap(px(2.))
                .child(mode_segment("ai-plan-mode-clean", tr("修改后预览", "Preview"), !link.diff_mode, |_, _, cx| plan_link::set_mode(false, cx)))
                .child(mode_segment("ai-plan-mode-diff", tr("增删对比", "Compare"), link.diff_mode, |_, _, cx| plan_link::set_mode(true, cx)))))
        .child(div().p(px(14.)).child(contents(&link)))
        .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-dock"))
            .px(px(14.)).py(px(10.)).bg(theme.shell).border_t_1().border_color(theme.hairline)
            .flex().flex_wrap().items_center().justify_between().gap(px(10.))
            .child(div().flex().items_center().gap(px(8.)).min_w_0()
                .child(div().relative().children(smoke_overlay::probe("ai-plan-inline-step"))
                    .px(px(7.)).py(px(2.)).rounded(radius::CONTROL).bg(theme.selected).text_color(theme.accent).text_size(px(12.5)).font_weight(FontWeight(500.))
                    .child(if link.page_ops > 1 {
                        if crate::theme::is_en() { format!("{} changes on this page", link.page_ops) } else { format!("本页 {} 项修改", link.page_ops) }
                    } else if crate::theme::is_en() { format!("Change {} / {}", link.op_idx + 1, link.total_ops) }
                    else { format!("修改 {} / {}", link.op_idx + 1, link.total_ops) }))
                .child(div().text_size(px(13.5)).text_color(theme.text_secondary).child(link.summary.clone())))
            .child(div().flex().items_center().gap(px(6.))
                .child(pill("ai-plan-inline-reject", if link.is_create { tr("关闭预览", "Close preview") } else { tr("拒绝此项", "Reject change") }.into(), false, move |_, _, cx| plan_link::decide(reject.clone(), false, cx)))
                .child(pill("ai-plan-inline-accept", if link.total_ops > 1 { tr("接受全部修改", "Accept all changes") } else { tr("接受修改", "Accept change") }.into(), true, move |_, _, cx| plan_link::decide(accept.clone(), true, cx)))))
}
