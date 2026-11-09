use std::path::PathBuf;
use gpui::{AnyElement, Context, MouseButton, SharedString, div, prelude::*, px};
use crate::{
    icons::{Icon, icon},
    theme::{Theme, tr},
    ui::segmented_control::SegmentSpec,
};
use super::tab_menu::{clear_tab_menu, hover_tinted_icon, is_page_menu_open, toggle_page_menu};
use super::Workspace;

impl Workspace {
    pub(super) fn annotation_actions(&self, cx: &mut Context<Self>) -> Option<gpui::Stateful<gpui::Div>> {
        if !crate::editor::ai::provider::config().enabled { return None; }
        let (drafts, generating, has_any) = self.editor.read(cx).annotation_summary();
        if !has_any { return None; }
        let label: SharedString = if generating { tr("生成中…", "Generating…").into() } else { tr("统一发送", "Send all").into() };
        let enabled = drafts > 0 && !generating;
        let editor = self.editor.clone();
        Some(crate::ui::annotation_card::send_all_button(
            "annotations-send-all", label, (drafts > 0 && !generating).then_some(drafts), generating, enabled,
            move |_, _, cx| { cx.stop_propagation(); editor.update(cx, |editor, cx| editor.generate_pending_annotations(cx)); },
        ))
    }

    pub(super) fn render_titlebar_tail(&self, show_active_tools: bool, cx: &mut Context<Self>) -> gpui::Div {
        let theme = Theme::ocean();
        if self.ai_immersive(cx) {
            return div().h(px(40.)).h_full().flex_none().flex().items_center()
                .child(crate::ui::window_controls::window_controls("main-win", true, true));
        }
        if self.settings.is_some() {
            return div().h(px(40.)).h_full().flex_none().flex().items_center()
                .child(crate::ui::window_controls::window_controls("main-win", true, true));
        }
        let surface = if show_active_tools { self.editor.read(cx).titlebar_surface() } else { crate::editor::view::TitlebarSurface::Empty };
        let active_path = self.selected.clone().or_else(|| self.editor.read(cx).current_path().map(std::path::Path::to_path_buf));
        let mut context_tools: Vec<AnyElement> = Vec::new();
        match surface {
            crate::editor::view::TitlebarSurface::Note => context_tools.push(self.note_more_pill(cx).into_any_element()),
            crate::editor::view::TitlebarSurface::ImagePreview => {
                if let Some((pct, fit)) = self.editor.read(cx).preview_zoom_state(cx) { push_zoom(&mut context_tools, "tb-img-zoom", pct, fit, cx); }
                push_reveal(&mut context_tools, &active_path, cx);
            }
            crate::editor::view::TitlebarSurface::Svg { show_source } => {
                push_status(&mut context_tools, self.editor.read(cx).source_status(), theme);
                context_tools.push(self.svg_mode_switch(show_source, cx).into_any_element());
                if !show_source && let Some((pct, fit)) = self.editor.read(cx).preview_zoom_state(cx) { push_zoom(&mut context_tools, "tb-svg-zoom", pct, fit, cx); }
                push_reveal(&mut context_tools, &active_path, cx);
            }
            crate::editor::view::TitlebarSurface::HtmlSource => {
                push_status(&mut context_tools, self.editor.read(cx).source_status(), theme);
                if let Some(opened) = active_path.clone() { context_tools.push(browser_button(opened, cx).into_any_element()); }
                push_reveal(&mut context_tools, &active_path, cx);
            }
            crate::editor::view::TitlebarSurface::CodeSource => {
                push_status(&mut context_tools, self.editor.read(cx).source_status(), theme);
                push_reveal(&mut context_tools, &active_path, cx);
            }
            crate::editor::view::TitlebarSurface::Blocked | crate::editor::view::TitlebarSurface::Empty => {}
        }
        let ent = cx.entity();
        let feedback_btn = crate::ui::feedback_controls::titlebar_feedback_button(
            crate::feedback::is_open(), crate::feedback::has_draft(),
            move |window, cx| { ent.update(cx, |this, cx| this.close_overlays(cx)); clear_tab_menu(); crate::feedback::toggle(window, cx); },
        );
        let ent = cx.entity();
        let ai_btn = crate::ui::ai_controls::titlebar_ai_button(crate::editor::ai::chat::is_open(cx), move |window, cx| {
            ent.update(cx, |this, cx| { if this.settings.is_some() { return; } this.close_overlays(cx); clear_tab_menu(); crate::editor::ai::chat::toggle(this.editor.downgrade(), window, cx); if !crate::editor::ai::chat::is_open(cx) { this.restore_focus(window, cx); } cx.notify(); });
        });
        let tools = div().h_full().flex().items_center().gap(px(4.));
        // Notes show only the more menu beside feedback; those two swap so feedback sits on the left.
        let tools = if matches!(surface, crate::editor::view::TitlebarSurface::Note) {
            tools.child(feedback_btn).child(ai_btn).children(context_tools)
        } else {
            tools.children(context_tools).child(feedback_btn).child(ai_btn)
        };
        div().h(px(40.)).h_full().flex_none().flex().items_center()
            .child(tools)
            .child(crate::ui::window_controls::window_controls("main-win", true, true))
    }

    fn note_more_pill(&self, cx: &mut Context<Self>) -> gpui::Stateful<gpui::Div> {
        let theme = Theme::ocean();
        let page_open = is_page_menu_open();
        let more_group: SharedString = "titlebar-more-btn".into();
        let send_seg = self.annotation_actions(cx);
        let (sz, rd) = if send_seg.is_some() { (px(24.), px(6.)) } else { (px(26.), crate::ui::radius::CONTROL) };
        let more = div().id("titlebar-more").relative().children(crate::ui::smoke_overlay::probe("titlebar-more")).child(crate::ui::placement::track_bounds(super::tab_menu::PAGE_TRIGGER.with(Clone::clone), page_open)).group(more_group.clone()).size(sz).rounded(rd)
            .flex_none().flex().items_center().justify_center().cursor(gpui::CursorStyle::PointingHand)
            .when(page_open, |b| b.bg(theme.selected)).when(!page_open, |b| b.hover(|s| s.bg(theme.hover)))
            .on_mouse_down(MouseButton::Left, cx.listener(|this, event: &gpui::MouseDownEvent, _, cx| {
                cx.stop_propagation();
                this.close_overlays(cx);
                toggle_page_menu(event.position);
                cx.notify();
            }))
            .child(if page_open { icon(Icon::More, px(18.), theme.icon_colors(theme.accent)) } else { hover_tinted_icon(more_group, Icon::More, 18., theme.text_secondary, theme.text, theme.icon_fill) });
        crate::ui::annotation_card::titlebar_split_pill(send_seg, more)
    }

    fn svg_mode_switch(&self, show_source: bool, _cx: &mut Context<Self>) -> impl gpui::IntoElement {
        let editor = self.editor.clone();
        let source_editor = editor.clone();
        crate::ui::segmented_control::segmented_control("svg-mode", vec![
            SegmentSpec {
                label: tr("预览", "Preview").into(), selected: !show_source,
                on_select: Box::new(move |_, _, app| { app.stop_propagation(); editor.update(app, |e, cx| e.select_svg_source(false, cx)); }),
            },
            SegmentSpec {
                label: tr("源码", "Source").into(), selected: show_source,
                on_select: Box::new(move |_, _, app| { app.stop_propagation(); source_editor.update(app, |e, cx| e.select_svg_source(true, cx)); }),
            },
        ])
    }
}

fn push_status(tools: &mut Vec<AnyElement>, status: &str, theme: Theme) {
    if status.is_empty() { return; }
    tools.push(div().flex_none().whitespace_nowrap().text_size(px(12.)).text_color(theme.text_secondary).child(status.to_owned()).into_any_element());
}

fn push_zoom(tools: &mut Vec<AnyElement>, id: &'static str, pct: String, fit: bool, cx: &mut Context<Workspace>) {
    tools.push(crate::ui::titlebar_context::zoom_cluster(
        id, pct, fit,
        cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.editor.update(cx, |e, cx| e.preview_zoom_step(1.0_f32 / 1.2_f32, cx)); }),
        cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.editor.update(cx, |e, cx| e.preview_zoom_actual(cx)); }),
        cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.editor.update(cx, |e, cx| e.preview_zoom_step(1.2_f32, cx)); }),
        cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.editor.update(cx, |e, cx| e.preview_zoom_fit(cx)); }),
    ).into_any_element());
}

fn push_reveal(tools: &mut Vec<AnyElement>, path: &Option<PathBuf>, cx: &mut Context<Workspace>) {
    let Some(path) = path.clone() else { return };
    tools.push(crate::ui::titlebar_context::reveal_button("tb-reveal", cx.listener(move |_, _, _, cx| {
        cx.stop_propagation();
        crate::files::ops::reveal_in_finder(&path);
    })).into_any_element());
}

fn browser_button(opened: PathBuf, cx: &mut Context<Workspace>) -> impl gpui::IntoElement {
    crate::ui::file_fallback::open_button("html-browser", tr("浏览器打开", "Open in Browser"), cx.listener(move |_, _, _, cx| {
        cx.stop_propagation();
        let opened = opened.clone();
        cx.spawn(async move |this, cx| {
            let result = cx.background_executor().spawn(async move { crate::files::open_with::open_in_browser(&opened) }).await;
            if let Err(err) = result {
                let msg = err.to_string();
                crate::log::error(&format!("open in browser: {msg}"));
                let _ = this.update(cx, |this, cx| this.editor.update(cx, |e, cx| e.set_source_status(msg, cx)));
            }
        }).detach();
    }))
}
