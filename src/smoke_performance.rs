use std::{fs, path::PathBuf};
use gpui::{Focusable, Modifiers, MouseMoveEvent, PlatformInput, px};
use super::{AsyncApp, Run, sleep, smoke_overlay};

const CATEGORIES: [(&str, u64); 7] = [
    ("app", 1024), ("notebook_markdown", 2048), ("notebook_other", 3072),
    ("history", 4096), ("ai_sessions", 5120), ("data_other", 6144), ("cache", 7168),
];

struct SparseFile(fs::File, u64);
impl Drop for SparseFile { fn drop(&mut self) { let _ = self.0.set_len(self.1); } }

fn fixture() -> Result<PathBuf, String> {
    let root = super::sandbox_path().join("performance-fixture");
    match fs::remove_dir_all(&root) {
        Ok(()) => (), Err(error) if error.kind() == std::io::ErrorKind::NotFound => (),
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

impl Run {
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
        self.win(cx, |window, cx| {
            let focus = self.workspace.read(cx).editor.focus_handle(cx);
            window.focus(&focus, cx);
            window.dispatch_action(Box::new(crate::menu_bar::ShowAbout), cx);
        })?;
        sleep(cx, 180).await;
        let editor_focused = self.win(cx, |window, cx| self.workspace.read(cx).editor.focus_handle(cx).is_focused(window))?;
        self.check(14, !editor_focused, "关于菜单正确移交焦点")?;
        self.redraw(cx);
        self.click_target(cx, "about-performance", "性能 · 打开监控").await?;
        let ready = self.wait_until(cx, 15000, |_, _| {
            let state = crate::performance::smoke_state();
            state.open && state.process_samples >= 2 && state.process_ok && state.cpu_ready
                && !state.disk_loading && state.disk_categories.len() == 7
        }).await;
        self.check(14, ready, "性能监控完成真实进程采样和磁盘扫描")?;
        let state = crate::performance::smoke_state();
        self.check(14, state.disk_error.is_none() && CATEGORIES.iter().all(|(id, bytes)|
            state.disk_categories.iter().any(|(actual, value)| actual == id && value == bytes)), "七类磁盘字节与隔离夹具一致")?;
        self.performance_geometry(cx)?;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("performance-process-map-tile-process-main").is_some(), "主进程和文件分类同屏")?;
        self.performance_select(cx, "app").await?;
        self.performance_select(cx, "history").await?;
        fs::write(root.join("notebook/new.md"), vec![0; 8192]).map_err(|error| error.to_string())?;
        self.click_target(cx, "performance-refresh", "性能 · 重新统计新增文件").await?;
        let refreshed = self.wait_until(cx, 15000, |_, _| {
            let state = crate::performance::smoke_state();
            !state.disk_loading && state.disk_categories.iter().any(|(id, bytes)| id == "notebook_markdown" && *bytes == 10240)
        }).await;
        self.check(14, refreshed, "重新统计反映新增文件真实字节")?;
        self.performance_skewed(cx, &root).await?;
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
        self.check(14, !crate::performance::smoke_state().open, "Escape 关闭监控")?;
        self.click_target(cx, "about-done", "关于 · 关闭").await?;
        self.win(cx, |window, cx| window.dispatch_action(Box::new(crate::menu_bar::ShowAbout), cx))?;
        sleep(cx, 180).await;
        self.redraw(cx);
        self.check(14, smoke_overlay::target_bounds("about-performance").is_some(), "再次打开关于 Block")?;
        cx.update(|cx| { crate::about::close(); self.workspace.update(cx, |_, cx| cx.notify()); });
        Ok(())
    }

    async fn performance_select(&self, cx: &AsyncApp, id: &str) -> Result<(), String> {
        let tile = format!("performance-map-tile-{id}");
        self.performance_hover(cx, &tile).await?;
        self.redraw(cx);
        let state = crate::performance::smoke_state();
        let bytes = state.disk_categories.iter().find(|(category, _)| category == id)
            .map(|(_, bytes)| *bytes).ok_or("找不到分类字节数")?;
        let total = state.disk_categories.iter().map(|(_, bytes)| *bytes as u128).sum();
        let detail = crate::ui::performance_rows::smoke_detail();
        self.check(14, state.selected.as_deref() == Some(id)
            && smoke_overlay::target_bounds(&format!("performance-detail-{id}")).is_some()
            && detail.contains(&crate::ui::performance_treemap::format_bytes(bytes))
            && detail.contains(&crate::ui::performance_treemap::format_percent(bytes, total)), "悬停更新选中详情和真实数值")?;
        self.click_target(cx, &tile, "性能 · 点击分类方块").await?;
        self.redraw(cx);
        self.check(14, crate::performance::smoke_state().selected.as_deref() == Some(id)
            && crate::ui::performance_rows::smoke_detail() == detail, "点击更新选中状态和详情")
    }

    fn performance_geometry(&self, cx: &AsyncApp) -> Result<(), String> {
        self.redraw(cx);
        let card = smoke_overlay::target_bounds("performance-card").ok_or("找不到性能面板")?;
        let viewport = self.win(cx, |window, _| window.viewport_size())?;
        self.check(14, card.size.height <= px(440.) && card.top() >= px(0.) && card.bottom() <= viewport.height, "面板高度不超过 440px")?;
        let mut tiles = Vec::new();
        for (id, _) in CATEGORIES {
            let bounds = smoke_overlay::target_bounds(&format!("performance-map-tile-{id}")).ok_or_else(|| format!("找不到方块 {id}"))?;
            self.check(14, bounds.size.width >= px(100.) && bounds.size.height >= px(50.)
                && bounds.left() >= card.left() && bounds.right() <= card.right()
                && bounds.top() >= card.top() && bounds.bottom() <= card.bottom(), "分类方块可读且完整可见")?;
            tiles.push(bounds);
        }
        for (index, tile) in tiles.iter().enumerate() {
            for other in tiles.iter().skip(index + 1) {
                let separated = tile.right() + px(5.) <= other.left() || other.right() + px(5.) <= tile.left()
                    || tile.bottom() + px(5.) <= other.top() || other.bottom() + px(5.) <= tile.top();
                self.check(14, separated, "方块之间存在明显空隙")?;
            }
        }
        let areas: Vec<f32> = tiles.iter().map(|bounds| f32::from(bounds.size.width) * f32::from(bounds.size.height)).collect();
        let min = areas.iter().copied().fold(f32::INFINITY, f32::min);
        let max = areas.iter().copied().fold(0., f32::max);
        self.check(14, max <= min * 3.2, "分类方块面积比例受控")
    }

    async fn performance_skewed(&self, cx: &AsyncApp, root: &std::path::Path) -> Result<(), String> {
        let large_bytes = 60 * 1024 * 1024 * 1024u64;
        let file = fs::OpenOptions::new().write(true).open(root.join("notebook/image.bin")).map_err(|error| error.to_string())?;
        let large = SparseFile(file, 3072);
        large.0.set_len(large_bytes).map_err(|error| error.to_string())?;
        let file = fs::OpenOptions::new().write(true).open(root.join("cache/cache.bin")).map_err(|error| error.to_string())?;
        let zero = SparseFile(file, 7168);
        zero.0.set_len(0).map_err(|error| error.to_string())?;
        self.click_target(cx, "performance-refresh", "性能 · 统计极端比例文件").await?;
        let refreshed = self.wait_until(cx, 15000, |_, _| {
            let state = crate::performance::smoke_state();
            !state.disk_loading && state.disk_categories.iter().any(|(id, bytes)| id == "notebook_other" && *bytes == large_bytes)
                && state.disk_categories.iter().any(|(id, bytes)| id == "cache" && *bytes == 0)
        }).await;
        self.check(14, refreshed, "稀疏大文件及零字节分类按真实字节统计")?;
        self.performance_geometry(cx)?;
        for (id, _) in CATEGORIES { self.performance_select(cx, id).await?; }
        Ok(())
    }
}
