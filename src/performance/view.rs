use std::{rc::Rc, time::UNIX_EPOCH};
use gpui::{
    AnyElement, Context, CursorStyle, FontWeight, MouseButton, MouseDownEvent, MouseMoveEvent,
    MouseUpEvent, Window, div, prelude::*, px, rgba,
};
use crate::{app::Workspace, icons::Icon, theme::{Theme, tr}, ui::{
    icon_button::icon_button, modal, motion::modal_enter,
    performance_rows::{loading_screen, metric, refresh_button},
    performance_treemap::{PerformanceSelect, TreemapItem, format_bytes, treemap}, smoke_overlay, tooltip,
}};
use super::{STATE, State};

pub fn view(window: &mut Window, cx: &mut Context<Workspace>) -> Option<AnyElement> {
    let state = STATE.lock().unwrap_or_else(|error| error.into_inner());
    if !state.open { return None; }
    let selected = state.selected.clone();
    let offset = state.offset;
    let dragging = state.drag_anchor.is_some();
    let disk = disk_items(&state);
    let processes = process_items(&state);
    let (stats, scanned_at) = metrics(&state, &disk);
    let loading = state.disk_loading || state.process.is_none();
    let error = match &state.disk {
        Some(Ok(snapshot)) if snapshot.partial => snapshot.errors.join("\n"),
        Some(Err(error)) => error.clone(), _ => String::new(),
    };
    let process_error = match &state.process {
        Some(Ok(snapshot)) => {
            let mut errors = snapshot.errors.clone();
            if snapshot.processes.iter().any(|row| row.rss_bytes.is_none()) {
                errors.push(tr("部分进程内存不可用", "Some process memory is unavailable").into());
            }
            errors.join("\n")
        },
        Some(Err(error)) => error.clone(), _ => String::new(),
    };
    drop(state);
    let theme = Theme::ocean();
    let owner = cx.entity().downgrade();
    let select: PerformanceSelect = Rc::new(move |id, _, cx| {
        let mut state = STATE.lock().unwrap_or_else(|error| error.into_inner());
        if state.selected.as_deref() != Some(id) {
            state.selected = Some(id.to_owned());
            drop(state);
            let _ = owner.update(cx, |_, cx| cx.notify());
        }
    });
    let viewport = window.viewport_size();
    let max_h = (viewport.height - px(40.)).max(px(1.)).min(px(440.));
    let drag_limit = (
        ((f32::from(viewport.width) - 720.).max(0.) * 0.5 + 120.).max(40.),
        ((f32::from(viewport.height) - 380.).max(0.) * 0.5).max(20.),
    );
    let card = modal::card("performance-card", px(720.)).relative().max_h(max_h)
        .left(px(offset.0)).top(px(offset.1))
        .children(smoke_overlay::probe("performance-card"))
        .child(div().id("performance-header").h(px(46.)).flex_none().flex().items_center().px(px(20.)).gap(px(8.))
            .border_b_1().border_color(theme.hairline)
            .cursor(if dragging { CursorStyle::ClosedHand } else { CursorStyle::OpenHand })
            .on_mouse_down(MouseButton::Left, cx.listener(|_, event: &MouseDownEvent, _, cx| {
                super::begin_drag((f32::from(event.position.x), f32::from(event.position.y)));
                cx.notify();
            }))
            .child(div().flex_1().text_size(px(15.)).font_weight(FontWeight(560.)).child(tr("性能与占用", "Performance & Usage")))
            .child(refresh_button(loading, cx.listener(|_, _, _, cx| { super::refresh_disk(); cx.notify(); })))
            .child(icon_button("performance-close", Icon::Close, true, cx.listener(|_, _, _, cx| {
                cx.stop_propagation();
                super::close();
                cx.notify();
            }))))
        .child(div().id("performance-metrics").relative().h(px(38.)).flex_none().px(px(20.))
            .bg(theme.sidebar).border_b_1().border_color(theme.hairline)
            .flex().items_center().justify_between().gap(px(12.))
            .children(smoke_overlay::probe("performance-metrics"))
            .child(div().flex().items_center().gap(px(16.)).children(stats))
            .child(scanned_at))
        .when(loading, |card| card.child(loading_screen()))
        .when(!loading, |card| card.child(div().w_full().px(px(20.)).py(px(12.)).flex().flex_col().gap(px(8.))
            .when(!error.is_empty(), |body| body.child(div().id("performance-disk-error").text_size(px(12.)).text_color(theme.text_secondary)
                .tooltip(tooltip::tooltip(error)).child(tr("部分文件未能读取", "Some files could not be read"))))
            .when(!process_error.is_empty(), |body| body.child(div().id("performance-process-error").text_size(px(12.)).text_color(theme.text_secondary)
                .tooltip(tooltip::tooltip(process_error)).child(tr("部分进程采样受限", "Some process metrics unavailable"))))
            .child(treemap("performance-map", &disk, 3, selected.as_deref(), select.clone()))
            .child(treemap("performance-process-map", &processes, 2, selected.as_deref(), select))));
    let overlay = div()
        .id("performance-overlay")
        .absolute()
        .top_0()
        .left_0()
        .size_full()
        .occlude()
        .bg(rgba(0x20242c2e))
        .flex()
        .items_center()
        .justify_center()
        .p(px(20.))
        .on_mouse_move(cx.listener(move |_, event: &MouseMoveEvent, _, cx| {
            if event.pressed_button == Some(MouseButton::Left) {
                if super::update_drag((f32::from(event.position.x), f32::from(event.position.y)), drag_limit) {
                    cx.notify();
                }
            } else if super::end_drag() {
                cx.notify();
            }
        }))
        .on_mouse_up(MouseButton::Left, cx.listener(|_, _: &MouseUpEvent, _, cx| {
            if super::end_drag() { cx.notify(); }
        }))
        .on_mouse_up_out(MouseButton::Left, cx.listener(|_, _: &MouseUpEvent, _, cx| {
            if super::end_drag() { cx.notify(); }
        }))
        .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| { super::close(); cx.notify(); }))
        .child(modal_enter(card, "modal-card-enter"));
    Some(gpui::deferred(overlay)
        .with_priority(7).into_any_element())
}

fn metrics(state: &State, disk: &[TreemapItem]) -> (Vec<AnyElement>, AnyElement) {
    let file_size = if matches!(state.disk, Some(Ok(_))) {
        format_bytes(disk.iter().fold(0u64, |sum, item| sum.saturating_add(item.bytes)))
    } else { "—".into() };
    let snapshot = state.process.as_ref().and_then(|result| result.as_ref().ok());
    let cpu = snapshot.and_then(|snapshot| snapshot.processes.iter().try_fold(0., |sum, row| row.cpu_percent.map(|value| sum + value)));
    let rss = snapshot.and_then(|snapshot| snapshot.processes.iter().try_fold(0u64, |sum, row| row.rss_bytes.and_then(|value| sum.checked_add(value))));
    let threads = snapshot.and_then(|snapshot| snapshot.processes.iter().try_fold(0u32, |sum, row| row.thread_count.and_then(|value| sum.checked_add(value))));
    let time = state.disk_at.and_then(|time| time.duration_since(UNIX_EPOCH).ok())
        .map(|duration| crate::page_history::time::format_datetime_ymdhms(duration.as_secs()))
        .unwrap_or_else(|| "—".into());
    let left = vec![
        metric(tr("文件体积", "Storage"), file_size),
        metric(tr("运行内存", "Memory"), rss.map(format_bytes).unwrap_or_else(|| "—".into())),
        metric("CPU", cpu.map(|value| format!("{value:.1}%")).unwrap_or_else(|| "—".into())),
        metric(tr("线程", "Threads"), threads.map(|value| value.to_string()).unwrap_or_else(|| "—".into())),
    ];
    (left, metric(tr("统计时间", "Scanned"), time))
}

fn disk_items(state: &State) -> Vec<TreemapItem> {
    let Some(Ok(snapshot)) = &state.disk else { return Vec::new() };
    snapshot.categories.iter().enumerate().map(|(index, category)| {
        let (label, detail) = category_text(category.id);
        TreemapItem { id: category.id.into(), label: label.into(), bytes: category.bytes,
            color: super::colors::category(index), detail: detail.into() }
    }).collect()
}

fn process_items(state: &State) -> Vec<TreemapItem> {
    let Some(Ok(snapshot)) = &state.process else { return Vec::new() };
    [true, false].into_iter().enumerate().filter_map(|(index, main)| {
        let rows: Vec<_> = snapshot.processes.iter().filter(|row| row.is_main == main).collect();
        if rows.is_empty() { return None; }
        let bytes = rows.iter().try_fold(0u64, |sum, row| row.rss_bytes.and_then(|value| sum.checked_add(value)))?;
        let cpu = rows.iter().try_fold(0., |sum, row| row.cpu_percent.map(|value| sum + value));
        let threads = rows.iter().try_fold(0u32, |sum, row| row.thread_count.and_then(|value| sum.checked_add(value)));
        let first = rows[0];
        let uptime = first.uptime.or_else(|| first.started_at.and_then(|t| t.elapsed().ok()))
            .map(|d| format!(" · {}", format_uptime(d.as_secs()))).unwrap_or_default();
        let who = if main || rows.len() == 1 {
            format!("PID {}", first.pid)
        } else {
            format!("{} {}", rows.len(), tr("个子进程", "processes"))
        };
        let detail = format!("{who} · CPU {} · {} {}{}",
            cpu.map(|value| format!("{value:.1}%")).unwrap_or_else(|| "—".into()),
            tr("线程", "Threads"), threads.map(|value| value.to_string()).unwrap_or_else(|| "—".into()), uptime);
        Some(TreemapItem { id: if main { "process-main" } else { "process-children" }.into(),
            label: if main { tr("主进程", "Main Process") } else { tr("子进程", "Child Processes") }.into(),
            bytes, color: super::colors::category(index), detail })
    }).collect()
}

fn format_uptime(secs: u64) -> String {
    if secs < 60 {
        format!("{secs}s")
    } else if secs < 3600 {
        format!("{}m {:02}s", secs / 60, secs % 60)
    } else {
        format!("{}h {:02}m", secs / 3600, (secs % 3600) / 60)
    }
}

fn category_text(id: &str) -> (&'static str, &'static str) {
    match id {
        "app" => (tr("应用", "App"), tr("程序本体", "Application bundle")),
        "notebook_markdown" => (tr("笔记", "Notes"), tr("Markdown 文档", "Markdown documents")),
        "notebook_other" => (tr("附件", "Attachments"), tr("图片与资源文件", "Images & assets")),
        "history" => (tr("历史版本", "History"), tr("本地历史快照", "Version snapshots")),
        "ai_sessions" => (tr("AI 会话", "AI Sessions"), tr("对话记录与草稿", "Chats & drafts")),
        "data_other" => (tr("配置与数据", "App Data"), tr("设置与运行日志", "Settings & logs")),
        "cache" => (tr("缓存", "Cache"), tr("字体与图片缓存", "Fonts & images")),
        _ => (tr("其他", "Other"), ""),
    }
}
