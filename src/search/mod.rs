//! Shared search state, debounced query runner, and result list rendering
//! used by both the in-app search overlay (`app_search.rs`) and the standalone
//! floating search window (`search/window.rs`).
#[cfg(not(target_family = "wasm"))]
pub mod window;

use std::ops::Range;
use std::path::PathBuf;
use std::time::Duration;
use gpui::{
    AnyElement, App, Context, CursorStyle, Div, Entity, FontWeight, HighlightStyle,
    MouseButton, MouseDownEvent, ScrollHandle, SharedString, StyledText, Window, div, prelude::*,
    px,
};
use gpui_component::input::{Input, InputState};
use crate::files::search::{Hit, search_notes};
use crate::icons::{Icon, icon, stateful_icon};
use crate::theme::{Theme, format_hotkey, is_dark, is_en, search_shortcut, tr};
use crate::ui::scrollbar;

const LIMIT: usize = 50;
const DEBOUNCE: Duration = Duration::from_millis(150);

pub struct SearchState {
    pub input: Entity<InputState>,
    pub hits: Vec<Hit>,
    pub selected: usize,
    pub epoch: u64,
    pub searching: bool,
    pub asked: bool,
    pub scroll: ScrollHandle,
}

impl SearchState {
    pub fn new(input: Entity<InputState>) -> Self {
        Self {
            input,
            hits: Vec::new(),
            selected: 0,
            epoch: 0,
            searching: false,
            asked: false,
            scroll: ScrollHandle::new(),
        }
    }

    pub fn selected_path(&self) -> Option<PathBuf> {
        self.hits.get(self.selected).map(|h| h.path.clone())
    }

    pub fn move_selection(&mut self, delta: isize) {
        if self.hits.is_empty() { return; }
        let last = self.hits.len() - 1;
        self.selected = (self.selected as isize + delta).clamp(0, last as isize) as usize;
        let titles = self.hits.iter().filter(|h| h.title_match).count();
        // Group labels are children of the scroll area too.
        let child = if self.selected < titles {
            self.selected + 1
        } else {
            self.selected + 1 + (titles > 0) as usize
        };
        self.scroll.scroll_to_item(child);
    }

    pub fn run_search<V: 'static>(
        &mut self,
        root: Option<PathBuf>,
        query: String,
        cx: &mut Context<V>,
        get: fn(&mut V) -> Option<&mut SearchState>,
    ) {
        let Some(root) = root else {
            self.hits.clear();
            self.searching = false;
            self.selected = 0;
            self.asked = false;
            cx.notify();
            return;
        };
        self.epoch += 1;
        let epoch = self.epoch;
        self.asked = !query.trim().is_empty();
        if !self.asked {
            self.hits.clear();
            self.searching = false;
            self.selected = 0;
            cx.notify();
            return;
        }
        self.searching = true;
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(DEBOUNCE).await;
            let alive = this.update(cx, |this, _| get(this).is_some_and(|s| s.epoch == epoch)).unwrap_or(false);
            if !alive { return; }
            let hits = cx.background_executor().spawn(async move { search_notes(&root, &query, LIMIT) }).await;
            let _ = this.update(cx, |this, cx| {
                let Some(search) = get(this).filter(|s| s.epoch == epoch) else { return };
                search.hits = hits;
                search.selected = 0;
                search.searching = false;
                search.scroll.scroll_to_item(0);
                cx.notify();
            });
        }).detach();
    }
}

pub fn kbd(label: impl Into<SharedString>) -> Div {
    let theme = Theme::ocean();
    div().h(px(20.)).px(px(6.)).flex().flex_none().items_center().justify_center().rounded(px(4.))
        .bg(theme.shell).border_1().border_color(theme.btn_border)
        .text_size(px(11.)).text_color(theme.text_secondary).child(label.into())
}

pub fn highlighted(text: String, marks: &[Range<usize>]) -> StyledText {
    let bg = if is_dark() { gpui::rgb(0x5c4813) } else { gpui::rgb(0xfbe7a6) };
    let style = HighlightStyle { background_color: Some(bg.into()), ..Default::default() };
    let marks = marks.iter().filter(|r| r.end <= text.len()).map(|r| (r.clone(), style)).collect::<Vec<_>>();
    StyledText::new(text).with_highlights(marks)
}

pub fn render_header(
    input: &Entity<InputState>,
    on_close: impl Fn(&MouseDownEvent, &mut Window, &mut App) + 'static,
) -> Div {
    let theme = Theme::ocean();
    div().h(px(52.)).flex_none().px(px(16.)).flex().items_center().gap(px(10.))
        .border_b_1().border_color(theme.hairline)
        .child(icon(Icon::FindSearch, px(18.), theme.mono_icon_colors(theme.text_secondary)))
        .child(div().flex_1().min_w_0().text_size(px(16.))
            .child(Input::new(input).appearance(false).bordered(false).focus_bordered(false)))
        .child(div().id("search-esc").cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, on_close)
            .child(kbd("esc")))
}

pub fn render_footer() -> Div {
    let theme = Theme::ocean();
    let hotkey_label = format_hotkey(&search_shortcut());
    div().h(px(36.)).flex_none().px(px(16.)).flex().items_center().gap(px(16.))
        .rounded_b(px(11.)).border_t_1().border_color(theme.hairline).bg(theme.sidebar)
        .text_size(px(12.)).text_color(theme.text_secondary)
        .child(div().flex().items_center().gap(px(6.)).child(kbd("↑")).child(kbd("↓")).child(tr("选择", "Select")))
        .child(div().flex().items_center().gap(px(6.)).child(kbd("↵")).child(tr("打开", "Open")))
        .child(div().flex().items_center().gap(px(6.)).child(kbd("esc")).child(tr("关闭", "Close")))
        .child(div().ml_auto().child(format!("{hotkey_label} {}", tr("全局唤出", "Global Summon"))))
}

pub fn render_body<V: 'static>(
    state: &SearchState,
    has_folder: bool,
    fill_height: bool,
    cx: &mut Context<V>,
    on_pick: fn(&mut V, usize, &mut Window, &mut Context<V>),
) -> AnyElement {
    let theme = Theme::ocean();
    if !has_folder || state.hits.is_empty() {
        let hint = if !has_folder {
            tr("请先在主窗口打开文件夹", "Open a folder in the main window first")
        } else if !state.asked {
            tr("输入关键词搜索标题与正文", "Type keywords to search titles and content")
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
        let group = state.hits.iter().enumerate().filter(|(_, h)| h.title_match == title_group).collect::<Vec<_>>();
        if group.is_empty() { continue; }
        let total: usize = group.iter().map(|(_, h)| h.count.max(1)).sum();
        let header = if title_group {
            label.to_owned()
        } else if is_en() {
            format!("{label} · {total} matches")
        } else {
            format!("{label} · {total} 处")
        };
        let pt = if rows.is_empty() { px(6.) } else { px(10.) };
        rows.push(div().px(px(12.)).pt(pt).pb(px(2.)).text_size(px(12.)).line_height(px(16.))
            .text_color(theme.text_secondary).child(header).into_any_element());
        rows.extend(group.into_iter().map(|(ix, hit)| hit_row(ix, hit, ix == state.selected, cx, on_pick)));
    }

    // The right padding is the scrollbar gutter, so the bar never covers or shifts the rows.
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
    ix: usize,
    hit: &Hit,
    selected: bool,
    cx: &mut Context<V>,
    on_pick: fn(&mut V, usize, &mut Window, &mut Context<V>),
) -> AnyElement {
    let theme = Theme::ocean();
    let right = if !hit.title_match && hit.count > 1 {
        if is_en() { format!("{} matches", hit.count) } else { format!("{} 处", hit.count) }
    } else {
        hit.folder.clone()
    };
    let hit_group = SharedString::from(format!("search-hit-{ix}"));
    div().id(("hit", ix)).group(hit_group.clone()).relative().when(ix == 0, |r| r.children(crate::ui::smoke_overlay::probe("search-hit-0"))).px(px(12.)).py(px(8.)).flex().flex_col().gap(px(2.)).rounded(px(6.))
        .border_1().border_color(if selected { theme.accent } else { gpui::transparent_black() })
        .when(selected, |row| row.bg(theme.selected))
        .when(!selected, |row| row.hover(|style| style.bg(theme.hover)))
        .cursor(CursorStyle::PointingHand)
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| on_pick(this, ix, window, cx)))
        .child(div().flex().items_center().gap(px(8.))
            .child(div().flex_none().child(stateful_icon(hit_group, Icon::Note, px(14.), selected, theme.mono_icon_colors(theme.text_secondary), theme.icon_colors(if selected { theme.accent } else { theme.text }))))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(14.)).font_weight(FontWeight(500.))
                .child(highlighted(hit.title.clone(), &hit.title_marks)))
            .child(div().flex_none().max_w(px(220.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(12.)).text_color(theme.text_secondary).child(right)))
        .when(!hit.snippet.is_empty(), |row| row.child(div().pl(px(22.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
            .text_size(px(13.)).text_color(theme.text_secondary)
            .child(highlighted(hit.snippet.clone(), &hit.marks))))
        .into_any_element()
}
