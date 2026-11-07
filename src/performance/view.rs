use std::{rc::Rc, time::UNIX_EPOCH};
use gpui::{AnyElement, Context, FontWeight, MouseButton, Window, div, prelude::*, px};
use crate::{app::Workspace, icons::Icon, theme::{Theme, tr}, ui::{
    icon_button::icon_button, modal, performance_rows::{metric, selection_detail},
    performance_treemap::{PerformanceSelect, TreemapItem, format_bytes, treemap}, smoke_overlay, tooltip,
}};
use super::{STATE, State};

pub fn view(window: &mut Window, cx: &mut Context<Workspace>) -> Option<AnyElement> {
    let state = STATE.lock().unwrap_or_else(|error| error.into_inner());
    if !state.open { return None; }
    let selected = state.selected.clone();
    let disk = disk_items(&state);
    let processes = process_items(&state);
    let stats = metrics(&state, &disk);
    let loading = state.disk_loading;
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
    let (selected_items, total) = if processes.iter().any(|item| Some(item.id.as_str()) == selected.as_deref()) {
        (&processes, processes.iter().map(|item| item.bytes as u128).sum())
    } else { (&disk, disk.iter().map(|item| item.bytes as u128).sum()) };
    let max_h = (window.viewport_size().height - px(40.)).max(px(1.)).min(px(440.));
    let card = modal::card("performance-card", px(720.)).relative().max_h(max_h)
        .children(smoke_overlay::probe("performance-card"))
        .child(div().h(px(44.)).flex_none().flex().items_center().px(px(20.)).gap(px(8.))
            .child(div().flex_1().text_size(px(16.)).font_weight(FontWeight(560.)).child(tr("性能与占用", "Performance & Usage")))
            .child(modal::action_button("performance-refresh", if loading { tr("统计中…", "Scanning…") } else { tr("重新统计", "Rescan") },
                cx.listener(|_, _, _, cx| { super::refresh_disk(); cx.notify(); })).w(px(100.)).h(px(28.)))
            .child(icon_button("performance-close", Icon::Close, true, cx.listener(|_, _, _, cx| { super::close(); cx.notify(); }))))
        .child(div().id("performance-metrics").relative().h(px(32.)).flex_none().px(px(20.)).flex().items_center().gap(px(14.))
            .children(smoke_overlay::probe("performance-metrics")).children(stats))
        .child(div().w_full().px(px(20.)).pb(px(12.)).flex().flex_col().gap(px(6.))
            .when(!error.is_empty(), |body| body.child(div().id("performance-disk-error").text_size(px(12.)).text_color(theme.text_secondary)
                .tooltip(tooltip::tooltip(error)).child(tr("文件统计不完整", "File scan incomplete"))))
            .when(!process_error.is_empty(), |body| body.child(div().id("performance-process-error").text_size(px(12.)).text_color(theme.text_secondary)
                .tooltip(tooltip::tooltip(process_error)).child(tr("进程采样不完整", "Process sampling incomplete"))))
            .child(treemap("performance-map", &disk, 3, selected.as_deref(), select.clone()))
            .child(treemap("performance-process-map", &processes, 2, selected.as_deref(), select))
            .child(selection_detail(selected_items, selected.as_deref(), total)));
    Some(gpui::deferred(modal::backdrop("performance-overlay", card).pt(px(20.))
        .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| { super::close(); cx.notify(); })))
        .with_priority(7).into_any_element())
}

fn metrics(state: &State, disk: &[TreemapItem]) -> Vec<AnyElement> {
    let file_size = if matches!(state.disk, Some(Ok(_))) {
        format_bytes(disk.iter().fold(0u64, |sum, item| sum.saturating_add(item.bytes)))
    } else { "—".into() };
    let snapshot = state.process.as_ref().and_then(|result| result.as_ref().ok());
    let cpu = snapshot.and_then(|snapshot| snapshot.processes.iter().try_fold(0., |sum, row| row.cpu_percent.map(|value| sum + value)));
    let rss = snapshot.and_then(|snapshot| snapshot.processes.iter().try_fold(0u64, |sum, row| row.rss_bytes.and_then(|value| sum.checked_add(value))));
    let threads = snapshot.and_then(|snapshot| snapshot.processes.iter().try_fold(0u32, |sum, row| row.thread_count.and_then(|value| sum.checked_add(value))));
    let time = state.disk_at.and_then(|time| time.duration_since(UNIX_EPOCH).ok())
        .map(|duration| crate::page_history::time::format_version_time(duration.as_secs()))
        .and_then(|value| value.split_whitespace().last().map(str::to_owned)).unwrap_or_else(|| "—".into());
    vec![metric(tr("文件体积", "Files"), file_size), metric(tr("运行内存", "Memory"), rss.map(format_bytes).unwrap_or_else(|| "—".into())),
        metric("CPU", cpu.map(|value| format!("{value:.1}%")).unwrap_or_else(|| "—".into())),
        metric(tr("线程", "Threads"), threads.map(|value| value.to_string()).unwrap_or_else(|| "—".into())),
        metric(tr("统计时间", "Scanned"), time)]
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
        let uptime = first.uptime.or_else(|| first.started_at.and_then(|t| t.elapsed().ok())).map(|d| format!(" · {}s", d.as_secs())).unwrap_or_default();
        let detail = format!("{} ({}->{}) · CPU {} · {} {}{}", first.name, first.parent_pid, first.pid,
            cpu.map(|value| format!("{value:.1}%")).unwrap_or_else(|| "—".into()),
            tr("线程", "Threads"), threads.map(|value| value.to_string()).unwrap_or_else(|| "—".into()), uptime);
        Some(TreemapItem { id: if main { "process-main" } else { "process-children" }.into(),
            label: if main { tr("主进程", "Main process") } else { tr("子进程", "Child processes") }.into(),
            bytes, color: super::colors::category(index), detail })
    }).collect()
}

fn category_text(id: &str) -> (&'static str, &'static str) {
    match id {
        "app" => (tr("应用", "Application"), tr("应用程序文件", "Application files")),
        "notebook_markdown" => (tr("笔记", "Notes"), tr("笔记本中的 Markdown 文件", "Markdown files in notebooks")),
        "notebook_other" => (tr("附件及其他", "Attachments & other"), tr("笔记本中的其他文件，可能包含工程依赖或构建产物", "Other notebook files, potentially including dependencies or build artifacts")),
        "history" => (tr("历史版本", "History"), tr("用于恢复笔记的历史文件", "Files used to restore note history")),
        "ai_sessions" => (tr("AI 会话", "AI sessions"), tr("本地会话与草稿文件", "Local sessions and draft files")),
        "data_other" => (tr("配置及其他", "Settings & other"), tr("应用数据中的其他文件", "Other application data")),
        "cache" => (tr("缓存", "Cache"), tr("字体与远程图片等缓存", "Font, remote image, and other caches")),
        _ => (tr("其他", "Other"), ""),
    }
}
