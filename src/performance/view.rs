use std::{rc::Rc, time::UNIX_EPOCH};
use gpui::{AnyElement, Context, FontWeight, MouseButton, Window, div, prelude::*, px};
use crate::{app::Workspace, icons::Icon, theme::{Theme, tr}, ui::{
    icon_button::icon_button, modal,
    performance_rows::{detail_row, metric},
    performance_treemap::{PerformanceSelect, TreemapItem, format_bytes, treemap},
    segmented_control::{SegmentSpec, segmented_control}, smoke_overlay, tooltip,
}};
use super::{STATE, State, Tab};

pub fn view(window: &mut Window, cx: &mut Context<Workspace>) -> Option<AnyElement> {
    let state = STATE.lock().unwrap_or_else(|error| error.into_inner());
    if !state.open { return None; }
    let tab = state.tab;
    let selected = state.selected.clone();
    let (items, message) = items(&state);
    let stats = metrics(&state);
    let ready = match tab { Tab::Storage => matches!(state.disk, Some(Ok(_))), Tab::Memory => matches!(state.process, Some(Ok(_))) };
    let partial = match tab {
        Tab::Storage => matches!(&state.disk, Some(Ok(snapshot)) if snapshot.partial),
        Tab::Memory => matches!(&state.process, Some(Ok(snapshot)) if !snapshot.errors.is_empty() || snapshot.processes.iter().any(|row| row.rss_bytes.is_none())),
    };
    let error_detail = match tab {
        Tab::Storage => match &state.disk { Some(Ok(snapshot)) => snapshot.errors.join("\n"), Some(Err(error)) => error.clone(), _ => String::new() },
        Tab::Memory => match &state.process { Some(Ok(snapshot)) => snapshot.errors.join("\n"), Some(Err(error)) => error.clone(), _ => String::new() },
    };
    let disk_time = state.disk_at.and_then(|time| time.duration_since(UNIX_EPOCH).ok())
        .map(|duration| crate::page_history::time::format_version_time(duration.as_secs()));
    let loading = state.disk_loading;
    drop(state);
    let theme = Theme::ocean();
    let owner = cx.entity().downgrade();
    let select: PerformanceSelect = Rc::new(move |id, _, cx| {
        let mut state = STATE.lock().unwrap_or_else(|error| error.into_inner());
        state.selected = Some(id.to_owned());
        drop(state);
        let _ = owner.update(cx, |_, cx| cx.notify());
    });
    let segments = [(Tab::Storage, tr("文件体积", "File size")), (Tab::Memory, tr("运行内存", "Memory"))]
        .into_iter().map(|(value, label)| SegmentSpec {
            label: label.into(), selected: tab == value,
            on_select: Box::new(cx.listener(move |_, _, _, cx| {
                let mut state = STATE.lock().unwrap_or_else(|error| error.into_inner());
                state.tab = value; state.selected = None; drop(state); cx.notify();
            })),
        }).collect();
    let own: Vec<_> = items.iter().filter(|item| !item.id.starts_with("notebook_")).cloned().collect();
    let linked: Vec<_> = items.iter().filter(|item| item.id.starts_with("notebook_")).cloned().collect();
    let max_h = (window.viewport_size().height - px(48.)).max(px(1.)).min(px(500.));
    let group_count = if tab == Tab::Storage { 2 } else { 1 };
    let content_h = px((group_count * 40 + items.len() * 32 + if message.is_empty() { 0 } else { 24 }) as f32);
    let fixed_h = if tab == Tab::Storage { px(172.) } else { px(132.) };
    let scroll_h = content_h.min((max_h - fixed_h).max(px(0.)));
    let scope = match tab {
        Tab::Storage => tr("应用及其数据、缓存的文件逻辑大小；占比以本组为准，不代表物理占盘", "Logical sizes of the application, its data and caches; percentages are within this group, not physical disk usage"),
        Tab::Memory => tr("主进程及可识别的后代进程 RSS；共享内存可能重复计算", "RSS of main and identifiable descendant processes; shared memory may be counted twice"),
    };
    let body = div().id("performance-scroll-content").w_full().h(scroll_h).overflow_y_scroll()
        .flex().flex_col().gap(px(4.))
        .when(!message.is_empty(), |body| body.child(div().id("performance-error").h(px(20.)).flex_none()
            .overflow_hidden().text_color(theme.text_secondary).tooltip(tooltip::tooltip(error_detail)).child(message)))
        .child(group("performance-map", if tab == Tab::Storage { tr("应用自身", "Application") } else { tr("进程驻留内存", "Process resident memory") },
            if tab == Tab::Storage { &own } else { &items }, ready, partial, selected.as_deref(), scope, select.clone()))
        .when(tab == Tab::Storage, |body| body.child(group("performance-notebook-map", tr("关联笔记本", "Linked notebooks"),
            &linked, ready, partial, selected.as_deref(),
            tr("已添加笔记本的文件，不计入应用自身；占比以本组为准，大小为文件逻辑大小", "Files in added notebooks, separate from the application; percentages are within this group and sizes are logical file sizes"), select.clone())));
    let card = modal::card("performance-card", px(680.)).relative().max_h(max_h)
        .children(smoke_overlay::probe("performance-card"))
        .child(div().h(px(44.)).flex_none().flex().items_center().px(px(20.))
            .child(div().flex_1().text_size(px(16.)).font_weight(FontWeight(560.)).child(tr("性能与占用", "Performance & Usage")))
            .child(icon_button("performance-close", Icon::Close, true, cx.listener(|_, _, _, cx| { super::close(); cx.notify(); }))))
        .child(div().id("performance-metrics").relative().flex_none().px(px(20.)).min_h(px(40.))
            .children(smoke_overlay::probe("performance-metrics"))
            .child(div().flex().flex_wrap().gap_x(px(16.)).gap_y(px(4.)).children(stats)))
        .child(div().px(px(20.)).pb(px(4.)).flex_none().child(segmented_control("performance-tabs", segments)))
        .child(div().relative().w_full().px(px(20.))
            .children(smoke_overlay::probe("performance-scroll"))
            .child(body))
        .when(tab == Tab::Storage, |card| card.child(div().h(px(40.)).flex_none().px(px(20.)).flex().items_center().gap(px(12.))
            .child(div().flex_1().text_color(theme.text_secondary).child(disk_time.map(|time|
                format!("{} {time}", tr("统计时间", "Scanned"))).unwrap_or_else(|| tr("正在统计…", "Scanning…").into())))
            .child(modal::action_button("performance-refresh", if loading { tr("统计中…", "Scanning…") } else { tr("重新统计", "Rescan") },
                cx.listener(|_, _, _, cx| { super::refresh_disk(); cx.notify(); })).w(px(110.)).h(px(28.)))));
    Some(gpui::deferred(modal::backdrop("performance-overlay", card).pt(px(20.))
        .on_mouse_down(MouseButton::Left, cx.listener(|_, _, _, cx| { super::close(); cx.notify(); })))
        .with_priority(7).into_any_element())
}

fn group(id: &str, title: &str, items: &[TreemapItem], ready: bool, partial: bool,
    selected: Option<&str>, scope: &str, select: PerformanceSelect) -> AnyElement {
    let total = items.iter().fold(0u64, |sum, item| sum.saturating_add(item.bytes));
    let value = if ready { format_bytes(total) } else { "—".into() };
    div().w_full().flex_none().flex().flex_col()
        .child(div().id(gpui::SharedString::from(format!("{id}-scope"))).h(px(20.)).flex_none().flex().items_center().font_weight(FontWeight(560.))
            .tooltip(tooltip::tooltip(scope.to_owned()))
            .child(div().flex_1().child(title.to_owned()))
            .child(format!("{}{value}", if partial { tr("已统计 ", "Counted ") } else { "" })))
        .child(treemap(id, items, selected, select.clone()))
        .children(items.iter().map(|item| detail_row("performance-map", item, total,
            selected == Some(item.id.as_str()), select.clone())))
        .into_any_element()
}

fn metrics(state: &State) -> Vec<AnyElement> {
    let Some(Ok(snapshot)) = &state.process else {
        let status = match &state.process { Some(Err(_)) => tr("采样失败", "Sampling failed"), _ => tr("采样中…", "Sampling…") };
        return vec![metric(tr("处理器", "CPU"), status), metric(tr("运行内存", "Memory"), status)];
    };
    let main = snapshot.processes.iter().find(|row| row.is_main);
    let cpu = snapshot.processes.iter().try_fold(0., |sum, row| row.cpu_percent.map(|value| sum + value));
    let rss = snapshot.processes.iter().try_fold(0u64, |sum, row| row.rss_bytes.and_then(|value| sum.checked_add(value)));
    let threads = snapshot.processes.iter().try_fold(0u32, |sum, row| row.thread_count.and_then(|value| sum.checked_add(value)));
    vec![div().id("performance-cpu").tooltip(tooltip::tooltip(tr("主进程及可识别后代进程的 CPU 总和；单核满载为 100%，多核可超过 100%", "CPU sum of the main and identifiable descendants; 100% is one fully used core, and multicore usage may exceed 100%")))
            .child(metric(tr("处理器", "CPU"), cpu.map(|value| format!("{value:.1}%")).unwrap_or_else(|| tr("采样中", "Sampling").into()))).into_any_element(),
        div().id("performance-rss").tooltip(tooltip::tooltip(tr("主进程及可识别后代进程的驻留内存总和，共享内存可能重复计算", "Resident memory of the main and identifiable descendant processes; shared pages may be counted twice")))
            .child(metric(tr("运行内存", "Memory"), rss.map(format_bytes).unwrap_or_else(|| "—".into()))).into_any_element(),
        div().id("performance-threads").tooltip(tooltip::tooltip(tr("应用同时执行任务的工作线程数，不等于 CPU 核心数", "Application worker threads executing tasks; not the number of CPU cores")))
            .child(metric(tr("线程", "Threads"), threads.map(|value| value.to_string()).unwrap_or_else(|| "—".into()))).into_any_element(),
        metric(tr("主进程运行", "Main process uptime"), main.and_then(|row| row.uptime)
            .map(|value| format!("{}{} {}{}", value.as_secs() / 60, tr("分钟", "m"), value.as_secs() % 60, tr("秒", "s"))).unwrap_or_else(|| "—".into()))]
}

fn items(state: &State) -> (Vec<TreemapItem>, String) {
    if state.tab == Tab::Storage {
        let Some(result) = &state.disk else { return (Vec::new(), String::new()) };
        let snapshot = match result { Ok(snapshot) => snapshot, Err(_) => return (Vec::new(), tr("统计失败", "Scan failed").into()) };
        let items = snapshot.categories.iter().enumerate().map(|(index, category)| {
            let (label, detail) = category_text(category.id);
            TreemapItem { id: category.id.into(), label: label.into(), bytes: category.bytes,
                color: super::colors::category(index), detail: detail.into() }
        }).collect();
        let message = if snapshot.partial { format!("{} {} {}", tr("有", ""), snapshot.error_count, tr("项未能读取", "items could not be read")) } else { String::new() };
        (items, message)
    } else {
        let Some(result) = &state.process else { return (Vec::new(), String::new()) };
        let snapshot = match result { Ok(snapshot) => snapshot, Err(_) => return (Vec::new(), tr("采样失败", "Sampling failed").into()) };
        let items = snapshot.processes.iter().enumerate().filter_map(|(index, row)| row.rss_bytes.map(|bytes| TreemapItem {
            id: row.pid.to_string(), label: if row.is_main { tr("主进程", "Main process").into() } else { row.name.clone() },
            bytes, color: super::colors::category(index), detail: format!("{} · PID {}", row.name, row.pid),
        })).collect();
        let mut errors = snapshot.errors.clone();
        if snapshot.processes.iter().any(|row| row.rss_bytes.is_none()) { errors.push(tr("部分进程 RSS 不可用", "Some process RSS values are unavailable").into()); }
        let message = if errors.is_empty() { String::new() } else { format!("{} {} {}", tr("有", ""), errors.len(), tr("项采样不完整", "sampling issues")) };
        (items, message)
    }
}

fn category_text(id: &str) -> (&'static str, &'static str) {
    match id {
        "app" => (tr("应用", "Application"), tr("应用程序文件", "Application files")),
        "notebook_markdown" => (tr("笔记", "Notes"), tr("所有已添加笔记本中的 Markdown 文件", "Markdown files in all added notebooks")),
        "notebook_other" => (tr("其他目录文件", "Other directory files"), tr("笔记本目录中的非 Markdown 文件，可能包含工程依赖或构建产物，不等于 Block 生成的附件", "Non-Markdown files in notebook directories, potentially including project dependencies or build artifacts; not necessarily attachments generated by Block")),
        "history" => (tr("历史版本", "History"), tr("用于恢复笔记的历史文件", "Files used to restore note history")),
        "ai_sessions" => (tr("AI 会话", "AI sessions"), tr("本地会话与草稿文件", "Local sessions and draft files")),
        "data_other" => (tr("配置及其他", "Settings & other"), tr("应用数据中的其他文件", "Other application data")),
        "cache" => (tr("缓存", "Cache"), tr("字体与远程图片等缓存", "Font, remote image, and other caches")),
        _ => (tr("其他", "Other"), ""),
    }
}
