use std::ops::Range;
use gpui::{
    AnyElement, App, Context, CursorStyle, Div, FontWeight, HighlightStyle, MouseButton,
    MouseDownEvent, SharedString, StyledText, Window, div, prelude::*, px,
};
use gpui_component::input::Input;
use crate::files::search::Hit;
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{Theme, is_dark, is_en, tr};
use crate::ui::{dropdown, scrollbar};
use super::{SearchScope, SearchState, folder_name, kbd};

pub fn highlighted(text: String, marks: &[Range<usize>]) -> StyledText {
    let bg = if is_dark() { gpui::rgb(0x5c4813) } else { gpui::rgb(0xfbe7a6) };
    let style = HighlightStyle { background_color: Some(bg.into()), ..Default::default() };
    let marks = marks.iter().filter(|range| range.end <= text.len()).map(|range| (range.clone(), style)).collect::<Vec<_>>();
    StyledText::new(text).with_highlights(marks)
}

pub fn render_header(
    state: &SearchState,
    on_toggle_scope: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Div {
    let theme = Theme::ocean();
    div().h(px(52.)).flex_none().px(px(16.)).flex().items_center().gap(px(10.))
        .border_b_1().border_color(theme.hairline)
        .child(icon(Icon::FindSearch, px(18.), theme.mono_icon_colors(theme.text_secondary)))
        .child(div().flex_1().min_w_0().text_size(px(16.))
            .child(Input::new(&state.input).appearance(false).bordered(false).focus_bordered(false)))
        .child(crate::ui::scope_dropdown::trigger_capsule("search-scope-trigger", state.scope_label(), state.scope_menu_open)
            .on_mouse_down(MouseButton::Left, move |event, window, cx| {
                cx.stop_propagation();
                on_toggle_scope(event, window, cx);
            }))
}

pub fn render_scope_menu<V: 'static>(
    key: &'static str,
    state: &SearchState,
    cx: &mut Context<V>,
    on_pick: fn(&mut V, usize, &mut Window, &mut Context<V>),
) -> Option<AnyElement> {
    let (phase, pop_gen) = crate::ui::motion::popup_motion(key, state.scope_menu_open)?;
    crate::ui::motion::schedule_popup_finish(key, pop_gen, cx);
    let cursor = state.scope_cursor;
    let mut rows = vec![scope_row(0, tr("全部文件夹", "All Folders"), matches!(state.scope, SearchScope::All), cursor == 0, cx, on_pick)];
    if !state.folders.is_empty() { rows.push(dropdown::separator().into_any_element()); }
    for (index, path) in state.folders.iter().enumerate() {
        let checked = matches!(&state.scope, SearchScope::Folder(folder) if folder == path);
        rows.push(scope_row(index + 1, folder_name(path), checked, cursor == index + 1, cx, on_pick));
    }
    crate::ui::scope_dropdown::menu_popover("search-scope-menu", rows, &state.scope_scroll, phase, pop_gen)
}

fn scope_row<V: 'static>(
    index: usize,
    label: impl Into<SharedString>,
    checked: bool,
    highlighted: bool,
    cx: &mut Context<V>,
    on_pick: fn(&mut V, usize, &mut Window, &mut Context<V>),
) -> AnyElement {
    dropdown::row_with(("search-scope", index), dropdown::RowSpec {
        icon: Some(Icon::Folder), label: label.into(), checked, highlighted, ..Default::default()
    })
        .relative()
        .children(crate::ui::smoke_overlay::probe(format!("search-scope-{index}")))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| on_pick(this, index, window, cx)))
        .into_any_element()
}

pub fn render_footer() -> Div {
    let theme = Theme::ocean();
    div().h(px(36.)).flex_none().px(px(16.)).flex().items_center().gap(px(12.))
        .rounded_b(crate::ui::radius::SURFACE_INNER).border_t_1().border_color(theme.hairline).bg(theme.sidebar)
        .text_size(px(12.)).text_color(theme.text_secondary)
        .child(div().flex().items_center().gap(px(6.)).child(kbd("↑")).child(kbd("↓")).child(tr("选择", "Select")))
        .child(div().flex().items_center().gap(px(6.)).child(kbd("↵")).child(tr("打开", "Open")))
        .child(div().flex().items_center().gap(px(6.)).child(kbd("Tab")).child(tr("切换文件夹", "Switch Folder")))
        .child(div().flex().items_center().gap(px(6.)).child(kbd("esc")).child(tr("关闭", "Close")))
}

pub fn render_body<V: 'static>(
    state: &SearchState,
    fill_height: bool,
    cx: &mut Context<V>,
    on_pick: fn(&mut V, usize, &mut Window, &mut Context<V>),
) -> AnyElement {
    let theme = Theme::ocean();
    if state.folders.is_empty() || state.hits.is_empty() {
        let hint = if state.folders.is_empty() {
            tr("请先在主窗口打开文件夹", "Open a folder in the main window first")
        } else if !state.asked {
            if matches!(state.scope, SearchScope::All) {
                tr("输入关键词搜索全部文件夹的标题与正文", "Type keywords to search titles and content across all folders")
            } else {
                tr("输入关键词搜索标题与正文", "Type keywords to search titles and content")
            }
        } else if state.searching {
            tr("正在搜索…", "Searching…")
        } else {
            tr("没有找到匹配的笔记", "No matching notes found")
        };
        let empty = if fill_height { div().flex_1().min_h_0() } else { div().h(px(96.)) };
        return empty.flex().items_center().justify_center().text_size(px(13.))
            .text_color(theme.text_secondary).child(hint).into_any_element();
    }
    let mut rows: Vec<AnyElement> = Vec::new();
    for (title_group, label) in [(true, tr("标题匹配", "Title Matches")), (false, tr("正文匹配", "Content Matches"))] {
        let group = state.hits.iter().enumerate().filter(|(_, hit)| hit.title_match == title_group).collect::<Vec<_>>();
        if group.is_empty() { continue; }
        let total: usize = group.iter().map(|(_, hit)| hit.count.max(1)).sum();
        let header = if title_group { label.to_owned() } else if is_en() { format!("{label} · {total} matches") } else { format!("{label} · {total} 处") };
        let pt = if rows.is_empty() { px(6.) } else { px(10.) };
        rows.push(div().px(px(12.)).pt(pt).pb(px(2.)).text_size(px(12.)).line_height(px(16.))
            .text_color(theme.text_secondary).child(header).into_any_element());
        rows.extend(group.into_iter().map(|(ix, hit)| hit_row(ix, hit, ix == state.selected, cx, on_pick)));
    }
    let list = div().id("search-results").py(px(8.)).pl(px(8.)).pr(px(scrollbar::GUTTER)).flex().flex_col().gap(px(2.))
        .overflow_y_scroll().track_scroll(&state.scroll).children(rows);
    let bar = scrollbar::vertical("search-scrollbar", &state.scroll, px(0.));
    if fill_height {
        div().relative().flex_1().min_h_0().child(list.size_full()).child(bar).into_any_element()
    } else {
        div().relative().child(list.max_h(px(460.))).child(bar).into_any_element()
    }
}

fn hit_row<V: 'static>(
    ix: usize, hit: &Hit, selected: bool, cx: &mut Context<V>,
    on_pick: fn(&mut V, usize, &mut Window, &mut Context<V>),
) -> AnyElement {
    let theme = Theme::ocean();
    let right = if !hit.title_match && hit.count > 1 {
        if is_en() { format!("{} matches", hit.count) } else { format!("{} 处", hit.count) }
    } else { hit.folder.clone() };
    let hit_group = SharedString::from(format!("search-hit-{ix}"));
    div().id(("hit", ix)).group(hit_group.clone()).relative()
        .when(ix == 0, |row| row.children(crate::ui::smoke_overlay::probe("search-hit-0")))
        .px(px(12.)).py(px(8.)).flex().flex_col().gap(px(2.)).rounded(crate::ui::radius::CONTROL)
        .border_1().border_color(if selected { theme.accent } else { gpui::transparent_black() })
        .when(selected, |row| row.bg(theme.selected))
        .when(!selected, |row| row.hover(|style| style.bg(theme.hover)))
        .cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| on_pick(this, ix, window, cx)))
        .child(div().flex().items_center().gap(px(8.))
            .child(div().flex_none().child(stateful_icon(hit_group, Icon::Note, px(14.), selected, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if selected { theme.accent } else { theme.text }))))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(14.)).font_weight(FontWeight(500.)).child(highlighted(hit.title.clone(), &hit.title_marks)))
            .child(div().flex_none().max_w(px(220.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(12.)).text_color(theme.text_secondary).child(right)))
        .when(!hit.snippet.is_empty(), |row| row.child(div().pl(px(22.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
            .text_size(px(13.)).text_color(theme.text_secondary).child(highlighted(hit.snippet.clone(), &hit.marks))))
        .into_any_element()
}
