//! Global search palette (⌘K) and the workspace-level key bindings.
use std::ops::Range;
use std::time::Duration;
use gpui::{
    AnyElement, App, AppContext, Context, CursorStyle, FontWeight, HighlightStyle, KeyBinding, MouseButton,
    ScrollHandle, SharedString, StyledText, Window, actions, div, prelude::*, px,
};
use gpui_component::input::{Input, InputEvent, InputState};
use crate::files::search::{Hit, search_notes};
use crate::icons::{Icon, icon};
use crate::theme::{Theme, format_hotkey, is_dark, search_shortcut};
use super::Workspace;

actions!(workspace, [OpenSearch, OpenSettings, NewNote, NewFolder, Dismiss, SearchUp, SearchDown]);

const LIMIT: usize = 50;
const DEBOUNCE: Duration = Duration::from_millis(150);

pub(crate) struct Search {
    input: gpui::Entity<InputState>,
    hits: Vec<Hit>,
    selected: usize,
    epoch: u64,
    searching: bool,
    asked: bool,
    scroll: ScrollHandle,
    _sub: gpui::Subscription,
}

/// Registered from `Workspace::new`, so `main.rs` needs no extra call.
pub(super) fn bind_keys(cx: &mut App) {
    let search_key = search_shortcut();
    cx.bind_keys([
        KeyBinding::new(&search_key, OpenSearch, None),
        KeyBinding::new("cmd-k", OpenSearch, None),
        KeyBinding::new("cmd-shift-k", OpenSearch, None),
        KeyBinding::new("alt-cmd-k", OpenSearch, None),
        KeyBinding::new("cmd-,", OpenSettings, None),
        KeyBinding::new("cmd-n", NewNote, Some("Workspace")),
        KeyBinding::new("cmd-shift-n", NewFolder, Some("Workspace")),
        KeyBinding::new("escape", Dismiss, Some("Workspace")),
        KeyBinding::new("up", SearchUp, Some("SearchPalette")),
        KeyBinding::new("down", SearchDown, Some("SearchPalette")),
    ]);
}

pub(super) fn kbd(label: impl Into<SharedString>) -> gpui::Div {
    let theme = Theme::ocean();
    div().h(px(20.)).px(px(6.)).flex().flex_none().items_center().justify_center().rounded(px(4.))
        .bg(theme.shell).border_1().border_color(theme.btn_border)
        .text_size(px(11.)).text_color(theme.text_secondary).child(label.into())
}

fn highlighted(text: String, marks: &[Range<usize>]) -> StyledText {
    let bg = if is_dark() { gpui::rgb(0x5c4813) } else { gpui::rgb(0xfbe7a6) };
    let style = HighlightStyle { background_color: Some(bg.into()), ..Default::default() };
    let marks = marks.iter().filter(|r| r.end <= text.len()).map(|r| (r.clone(), style)).collect::<Vec<_>>();
    StyledText::new(text).with_highlights(marks)
}

impl Workspace {
    pub(crate) fn summon_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.open_search(window, cx);
    }

    pub(super) fn open_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.folder.is_none() { return; }
        if let Some(search) = &self.search {
            search.input.update(cx, |input, cx| input.focus(window, cx));
            return;
        }
        if self.renaming.is_some() { self.cancel_rename(window, cx); }
        if self.settings.is_some() { self.close_settings(window, cx); }
        self.close_overlays(cx);
        let input = cx.new(|cx| InputState::new(window, cx).placeholder("搜索笔记标题与正文"));
        let sub = cx.subscribe_in(&input, window, |this, input, event: &InputEvent, window, cx| match event {
            InputEvent::Change => { let query = input.read(cx).value().to_string(); this.run_search(query, cx); }
            InputEvent::PressEnter { .. } => this.open_hit(window, cx),
            _ => {}
        });
        input.update(cx, |input, cx| input.focus(window, cx));
        let focus_input = input.clone();
        cx.defer_in(window, move |_, window, cx| focus_input.update(cx, |input, cx| input.focus(window, cx)));
        self.search = Some(Search { input, hits: Vec::new(), selected: 0, epoch: 0, searching: false,
            asked: false, scroll: ScrollHandle::new(), _sub: sub });
        cx.notify();
    }

    pub(super) fn close_search(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.search.take().is_none() { return; }
        self.restore_focus(window, cx);
        cx.notify();
    }

    fn run_search(&mut self, query: String, cx: &mut Context<Self>) {
        let Some(root) = self.folder.clone() else { return };
        let Some(search) = &mut self.search else { return };
        search.epoch += 1;
        let epoch = search.epoch;
        search.asked = !query.trim().is_empty();
        if !search.asked {
            search.hits.clear();
            search.searching = false;
            search.selected = 0;
            cx.notify();
            return;
        }
        search.searching = true;
        cx.spawn(async move |this, cx| {
            cx.background_executor().timer(DEBOUNCE).await;
            let alive = this.update(cx, |this, _| this.search.as_ref().is_some_and(|s| s.epoch == epoch)).unwrap_or(false);
            if !alive { return; }
            let hits = cx.background_executor().spawn(async move { search_notes(&root, &query, LIMIT) }).await;
            let _ = this.update(cx, |this, cx| {
                let Some(search) = this.search.as_mut().filter(|s| s.epoch == epoch) else { return };
                search.hits = hits;
                search.selected = 0;
                search.searching = false;
                search.scroll.scroll_to_item(0);
                cx.notify();
            });
        }).detach();
    }

    fn move_selection(&mut self, delta: isize, cx: &mut Context<Self>) {
        let Some(search) = &mut self.search else { return };
        if search.hits.is_empty() { return; }
        let last = search.hits.len() - 1;
        search.selected = (search.selected as isize + delta).clamp(0, last as isize) as usize;
        let titles = search.hits.iter().filter(|h| h.title_match).count();
        // Group labels are children of the scroll area too.
        let child = if search.selected < titles { search.selected + 1 } else { search.selected + 1 + (titles > 0) as usize };
        search.scroll.scroll_to_item(child);
        cx.notify();
    }

    fn open_hit(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let Some(path) = self.search.as_ref().and_then(|s| s.hits.get(s.selected)).map(|h| h.path.clone()) else { return };
        self.close_search(window, cx);
        if self.busy() { return; }
        self.editor.update(cx, |editor, cx| editor.open(path, cx));
    }

    pub(super) fn search_view(&self, cx: &mut Context<Self>) -> Option<AnyElement> {
        let search = self.search.as_ref()?;
        let theme = Theme::ocean();
        let border = theme.hairline;
        let hotkey_label = format_hotkey(&search_shortcut());
        let mut rows: Vec<AnyElement> = Vec::new();
        for (title_group, label) in [(true, "标题匹配"), (false, "正文匹配")] {
            let group = search.hits.iter().enumerate().filter(|(_, h)| h.title_match == title_group).collect::<Vec<_>>();
            if group.is_empty() { continue; }
            let header = if title_group { label.to_owned() } else { format!("{label} · {} 处", group.len()) };
            rows.push(div().px(px(12.)).pt(px(8.)).pb(px(4.)).text_size(px(11.)).font_weight(FontWeight(600.)).text_color(theme.text_secondary)
                .child(header).into_any_element());
            rows.extend(group.into_iter().map(|(ix, hit)| self.hit_row(ix, hit, ix == search.selected, cx)));
        }
        let empty = search.hits.is_empty();
        let hint = if !search.asked { "输入关键词搜索标题与正文" } else if search.searching { "正在搜索…" } else { "没有找到匹配的笔记" };
        Some(div().id("search-overlay").absolute().top_0().left_0().size_full().occlude()
            .bg(gpui::rgba(0x20242c2e)).flex().flex_col().items_center().pt(px(96.)).px(px(24.))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| this.close_search(window, cx)))
            .child(div().id("search-dialog").key_context("SearchPalette").w(px(640.)).max_w_full()
                .flex().flex_col().overflow_hidden().rounded(px(12.)).bg(theme.menu)
                .border_1().border_color(theme.menu_border).shadow_lg().text_color(theme.text)
                .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
                .on_action(cx.listener(|this, _: &SearchUp, _, cx| this.move_selection(-1, cx)))
                .on_action(cx.listener(|this, _: &SearchDown, _, cx| this.move_selection(1, cx)))
                .child(div().h(px(52.)).flex_none().px(px(16.)).flex().items_center().gap(px(10.)).border_b_1().border_color(border)
                    .child(icon(Icon::FindSearch, px(18.), theme.icon_colors(theme.text_secondary)))
                    .child(div().flex_1().min_w_0().text_size(px(16.))
                        .child(Input::new(&search.input).appearance(false).bordered(false).focus_bordered(false)))
                    .child(kbd("esc")))
                .child(if empty {
                    div().h(px(96.)).flex().items_center().justify_center().text_size(px(13.))
                        .text_color(theme.text_secondary).child(hint).into_any_element()
                } else {
                    div().id("search-results").max_h(px(460.)).p(px(8.)).flex().flex_col().gap(px(2.))
                        .overflow_y_scroll().track_scroll(&search.scroll).children(rows).into_any_element()
                })
                .child(div().h(px(36.)).flex_none().px(px(16.)).flex().items_center().gap(px(16.))
                    .border_t_1().border_color(border).bg(theme.sidebar)
                    .text_size(px(12.)).text_color(theme.text_secondary)
                    .child(div().flex().items_center().gap(px(6.)).child(kbd("↑↓")).child("选择"))
                    .child(div().flex().items_center().gap(px(6.)).child(kbd("↵")).child("打开"))
                    .child(div().flex().items_center().gap(px(6.)).child(kbd("esc")).child("关闭"))
                    .child(div().ml_auto().child(format!("{hotkey_label} 搜索与全局唤起")))))
            .into_any_element())
    }

    fn hit_row(&self, ix: usize, hit: &Hit, selected: bool, cx: &mut Context<Self>) -> AnyElement {
        let theme = Theme::ocean();
        div().id(("hit", ix)).px(px(12.)).py(px(8.)).flex().flex_col().gap(px(4.)).rounded(px(6.))
            .border_1().border_color(if selected { theme.accent } else { gpui::transparent_black() })
            .when(selected, |row| row.bg(theme.selected))
            .when(!selected, |row| row.hover(|style| style.bg(theme.hover)))
            .cursor(CursorStyle::PointingHand)
            .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                if let Some(search) = &mut this.search { search.selected = ix; }
                this.open_hit(window, cx);
            }))
            .child(div().flex().items_center().gap(px(8.))
                .child(div().flex_none().child(icon(Icon::Note, px(14.), theme.icon_colors(theme.text_secondary))))
                .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                    .text_size(px(14.)).font_weight(FontWeight(500.))
                    .child(highlighted(hit.title.clone(), &hit.title_marks)))
                .child(div().flex_none().max_w(px(220.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
                    .text_size(px(12.)).text_color(theme.text_secondary).child(hit.folder.clone())))
            .when(!hit.snippet.is_empty(), |row| row.child(div().pl(px(22.)).overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(13.)).text_color(theme.text_secondary)
                .child(highlighted(hit.snippet.clone(), &hit.marks))))
            .into_any_element()
    }
}
