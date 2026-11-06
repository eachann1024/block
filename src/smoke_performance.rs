use std::{fs, path::PathBuf};
use gpui::{ElementId, Modifiers, MouseMoveEvent, PlatformInput, ScrollDelta, ScrollEvent, TouchPhase, point, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

const CATEGORIES: [(&str, u64); 7] = [
    ("app", 1024), ("notebook_markdown", 2048), ("notebook_other", 3072),
    ("history", 4096), ("ai_sessions", 5120), ("data_other", 6144), ("cache", 7168),
];

fn fixture() -> Result<PathBuf, String> {
    let root = super::sandbox_path().join("performance-fixture");
    match fs::remove_dir_all(&root) {
        Ok(()) => (),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
        Err(error) => return Err(error.to_string()),
    }
    for (name, size) in [
        ("app/block", 1024), ("notebook/note.md", 2048), ("notebook/image.bin", 3072),
        ("data/History/revision", 4096), ("data/ai-chats.json", 5120),
        ("data/settings.json", 6144), ("cache/cache.bin", 7168),
    ] {
        let path = root.join(name);
        fs::create_dir_all(path.parent().unwrap()).map_err(|error| error.to_string())?;
        fs::write(path, vec![0; size]).map_err(|error| error.to_string())?;
    }
    Ok(root)
}

fn tab_target(index: usize) -> String {
    ElementId::from((ElementId::from("performance-tabs"), index.to_string())).to_string()
}

impl Run {
    async fn performance_scroll(&self, cx: &AsyncApp, delta: f32) -> Result<(), String> {
        self.redraw(cx);
        let position = smoke_overlay::target_center("performance-scroll").ok_or("找不到性能滚动区域")?;
        self.win(cx, |window, cx| window.dispatch_event(PlatformInput::Scroll(ScrollEvent {
            position, delta: ScrollDelta::Pixels(point(px(0.), px(delta))),
            modifiers: Modifiers::default(), touch_phase: TouchPhase::Moved, source: Default::default(),
        }), cx))?;
        sleep(cx, 180).await;
        self.redraw(cx);
        Ok(())
    }

    async fn performance_reveal_row(&self, cx: &AsyncApp, target: &str) -> Result<(), String> {
        for _ in 0..12 {
            self.redraw(cx);
            let scroll = smoke_overlay::target_bounds("performance-scroll").ok_or("找不到性能滚动区域")?;
            let card = smoke_overlay::target_bounds("performance-card").ok_or("找不到性能面板")?;
            if let Some(row) = smoke_overlay::target_bounds(target) {
                if row.top() >= scroll.top() && row.bottom() <= scroll.bottom()
                    && row.top() >= card.top() && row.bottom() <= card.bottom()
                    && row.left() >= scroll.left() && row.right() <= scroll.right() {
                    return Ok(());
                }
            }
            self.performance_scroll(cx, -100.).await?;
        }
        Err(format!("滚动后明细仍未完整显示：{target}；视口={:?}，面板={:?}，明细={:?}",
            smoke_overlay::target_bounds("performance-scroll"),
            smoke_overlay::target_bounds("performance-card"),
            smoke_overlay::target_bounds(target)))
    }

    async fn performance_hover(&self, cx: &AsyncApp, id: &str) -> Result<(), String> {
        self.redraw(cx);
        let position = smoke_overlay::target_center(id).ok_or_else(|| format!("找不到悬停目标 {id}"))?;
        self.win(cx, |window, cx| window.dispatch_event(PlatformInput::MouseMove(MouseMoveEvent {
            position, modifiers: Modifiers::default(), pressed_button: None,
        }), cx))?;
        sleep(cx, 150).await;
        Ok(())
    }

    pub(super) async fn performance(&self, cx: &AsyncApp) -> Result<(), String> {
        let root = fixture()?;
        cx.update(|cx| {
            crate::about::open_state();
            self.workspace.update(cx, |_, cx| cx.notify());
        });
        self.redraw(cx);
        self.click_target(cx, "about-performance", "性能 · 打开监控").await?;
        self.performance_checks(cx, root).await
    }

    async fn performance_checks(&self, cx: &AsyncApp, root: PathBuf) -> Result<(), String> {
        let ready = self.wait_until(cx, 15000, |_, _| {
            let state = crate::performance::smoke_state();
            state.open && state.process_samples >= 2 && state.process_ok && state.cpu_ready
                && !state.disk_loading && state.disk_categories.len() == 7
        }).await;
        self.check(14, ready, "性能监控完成真实进程采样和磁盘扫描")?;
        let state = crate::performance::smoke_state();
        self.check(14, state.disk_error.is_none() && CATEGORIES.iter().all(|(id, bytes)|
            state.disk_categories.iter().any(|(actual, value)| actual == id && value == bytes)), "七类磁盘字节与隔离夹具一致")?;
        self.redraw(cx);
        let map = smoke_overlay::target_bounds("performance-map").ok_or("找不到性能矩阵")?;
        let map_area = f32::from(map.size.width) * f32::from(map.size.height);
        let total: u64 = CATEGORIES.iter().map(|(_, bytes)| bytes).sum();
        for (id, bytes) in CATEGORIES {
            let tile = format!("performance-map-tile-{id}");
            let bounds = smoke_overlay::target_bounds(&tile).ok_or_else(|| format!("找不到矩阵色块 {id}"))?;
            let area = f32::from(bounds.size.width) * f32::from(bounds.size.height);
            let actual = area / map_area;
            let expected = bytes as f32 / total as f32;
            self.check(14, (actual - expected).abs() < 0.02, &format!("{id} 色块面积与字节比例一致"))?;
            self.performance_hover(cx, &tile).await?;
            self.check(14, crate::performance::smoke_state().selected.as_deref() == Some(id), &format!("悬停 {id} 选中对应明细"))?;
        }
        for (id, _) in CATEGORIES {
            let row = format!("performance-map-row-{id}");
            self.performance_reveal_row(cx, &row).await?;
            self.click_target(cx, &row, "性能 · 点击分类明细").await?;
            self.check(14, crate::performance::smoke_state().selected.as_deref() == Some(id), &format!("点击 {id} 明细保持选中"))?;
        }
        self.performance_scroll(cx, 2000.).await?;
        self.click_target(cx, &tab_target(1), "性能 · 切换运行内存").await?;
        self.check(14, crate::performance::smoke_state().tab == "memory", "已切换运行内存")?;
        self.click_target(cx, &tab_target(0), "性能 · 切换文件体积").await?;
        self.check(14, crate::performance::smoke_state().tab == "storage", "已切换文件体积")?;
        fs::write(root.join("notebook/new.md"), vec![0; 8192]).map_err(|error| error.to_string())?;
        self.click_target(cx, "performance-refresh", "性能 · 重新统计新增文件").await?;
        let refreshed = self.wait_until(cx, 15000, |_, _| {
            let state = crate::performance::smoke_state();
            !state.disk_loading && state.disk_categories.iter().any(|(id, bytes)| id == "notebook_markdown" && *bytes == 10240)
        }).await;
        self.check(14, refreshed, "重新统计反映新增文件真实字节")?;
        self.click_target(cx, "performance-close", "性能 · 关闭监控").await?;
        let closed = crate::performance::smoke_state();
        self.check(14, !closed.open, "关闭监控")?;
        sleep(cx, 1300).await;
        self.check(14, crate::performance::smoke_state().process_samples == closed.process_samples, "关闭后采样停止")?;
        self.click_target(cx, "about-performance", "性能 · 重新打开监控").await?;
        let reopened = self.wait_until(cx, 5000, |_, _| {
            let state = crate::performance::smoke_state();
            state.open && state.generation != closed.generation && state.process_samples >= 1
        }).await;
        self.check(14, reopened, "重新打开后启动新的采样")?;
        self.press(cx, "escape").await?;
        let escaped = crate::performance::smoke_state();
        self.check(14, !escaped.open, "Escape 关闭监控")?;
        sleep(cx, 1300).await;
        self.check(14, crate::performance::smoke_state().process_samples == escaped.process_samples, "Escape 后采样停止")?;
        cx.update(|cx| { crate::about::close(); self.workspace.update(cx, |_, cx| cx.notify()); });
        self.redraw(cx);
        Ok(())
    }
}
