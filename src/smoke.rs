//! `--smoke`: a visible, scripted click and keyboard pass over the real window.
//! It runs against a throw-away sandbox, never the user's notes or `state.json`.
use std::{path::PathBuf, time::Duration};
use gpui::{
    AnyWindowHandle, App, AppContext, AsyncApp, Entity, Keystroke, Modifiers, MouseButton, MouseDownEvent,
    MouseMoveEvent, MouseUpEvent, PlatformInput, Pixels, Point, Window, point, px,
};
use crate::{app::Workspace, ui::smoke_overlay};

const STEPS: usize = 7;

pub fn is_requested() -> bool { std::env::args().any(|a| a == "--smoke" || a == "--smoke-test") }
pub fn keep_open() -> bool { std::env::args().any(|a| a == "--keep-open") }

/// Creates the sandbox notebook and redirects all preferences into it.
pub fn init_sandbox() -> Option<PathBuf> {
    if !is_requested() { return None; }
    smoke_overlay::enable_smoke_mode();
    let sandbox = PathBuf::from("/tmp").join(format!("thinkly-smoke-{}", std::process::id()));
    let notebook = sandbox.join("notebook");
    let setup = || -> std::io::Result<()> {
        std::fs::create_dir_all(&notebook)?;
        let welcome = notebook.join("Welcome.md");
        std::fs::write(&welcome, "# Thinkly Fast\n\n点击验证初始段落。\n")?;
        crate::files::state::set_state_path_override(sandbox.join("state.json"));
        crate::files::save_folder(&notebook)?;
        crate::files::update_state(|s| { s.open_tabs = vec![welcome.clone()]; s.active_tab = Some(welcome); })?;
        Ok(())
    };
    if let Err(error) = setup() {
        eprintln!("[FAIL] 无法创建冒烟沙箱: {error}");
        std::process::exit(1);
    }
    Some(sandbox)
}

pub fn spawn(window: AnyWindowHandle, workspace: Entity<Workspace>, sandbox: Option<PathBuf>, cx: &mut App) {
    let run = Run { window, workspace };
    cx.spawn(async move |cx| {
        let outcome = run.all_steps(cx).await;
        run.finish(outcome, sandbox, cx).await;
    }).detach();
}

struct Run { window: AnyWindowHandle, workspace: Entity<Workspace> }

fn sleep(cx: &AsyncApp, ms: u64) -> gpui::Task<()> { cx.background_executor().timer(Duration::from_millis(ms)) }

impl Run {
    fn win<R>(&self, cx: &AsyncApp, f: impl FnOnce(&mut Window, &mut App) -> R) -> Result<R, String> {
        cx.update(|cx| cx.update_window(self.window, |_, window, cx| f(window, cx)))
            .map_err(|e| format!("窗口不可用: {e}"))
    }

    fn query<R>(&self, cx: &AsyncApp, f: impl FnOnce(&Workspace, &App) -> R) -> R {
        cx.update(|cx| f(self.workspace.read(cx), cx))
    }

    fn redraw(&self, cx: &AsyncApp) {
        cx.update(|cx| self.workspace.update(cx, |_, cx| cx.notify()));
    }

    async fn wait_until(&self, cx: &AsyncApp, max_ms: u64, cond: impl Fn(&Workspace, &App) -> bool) -> bool {
        for _ in 0..(max_ms / 50).max(1) {
            if self.query(cx, &cond) { return true; }
            sleep(cx, 50).await;
        }
        self.query(cx, &cond)
    }

    async fn click_at(&self, cx: &AsyncApp, position: Point<Pixels>, label: &str) -> Result<(), String> {
        cx.update(|_| smoke_overlay::set_step(Some(position), label.to_owned()));
        self.redraw(cx);
        self.win(cx, |window, cx| {
            window.dispatch_event(PlatformInput::MouseMove(MouseMoveEvent {
                position, modifiers: Modifiers::default(), pressed_button: None,
            }), cx);
        })?;
        sleep(cx, 140).await;
        self.win(cx, |window, cx| {
            window.dispatch_event(PlatformInput::MouseDown(MouseDownEvent {
                position, button: MouseButton::Left, modifiers: Modifiers::default(), click_count: 1, first_mouse: false,
            }), cx);
            window.dispatch_event(PlatformInput::MouseUp(MouseUpEvent {
                position, button: MouseButton::Left, modifiers: Modifiers::default(), click_count: 1,
            }), cx);
        })?;
        sleep(cx, 280).await;
        Ok(())
    }

    async fn click_target(&self, cx: &AsyncApp, id: &str, label: &str) -> Result<(), String> {
        let center = smoke_overlay::target_center(id).ok_or_else(|| format!("找不到点击目标 {id}"))?;
        self.click_at(cx, center, label).await
    }

    fn press(&self, cx: &AsyncApp, key: &str) -> Result<(), String> {
        let stroke = Keystroke { modifiers: Modifiers::default(), key: key.to_owned(), key_char: None };
        self.win(cx, |window, cx| { window.dispatch_keystroke(stroke, cx); })
    }

    async fn type_text(&self, cx: &AsyncApp, text: &str) -> Result<(), String> {
        for ch in text.chars() {
            let shift = ch.is_uppercase();
            let stroke = Keystroke {
                modifiers: Modifiers { shift, ..Modifiers::default() },
                key: ch.to_lowercase().to_string(),
                key_char: None,
            };
            self.win(cx, |window, cx| { window.dispatch_keystroke(stroke, cx); })?;
            sleep(cx, 35).await;
        }
        Ok(())
    }

    fn check(&self, n: usize, ok: bool, title: &str) -> Result<(), String> {
        if !ok { return Err(format!("Step {n}/{STEPS}: {title}")); }
        println!("[PASS] Step {n}/{STEPS}: {title}");
        Ok(())
    }

    async fn all_steps(&self, cx: &mut AsyncApp) -> Result<(), String> {
        let cx = &*cx;
        let ready = self.wait_until(cx, 4000, |w, _| !w.loading && !w.busy() && w.folder.is_some()).await;
        if !ready { return Err("工作区未能在 4 秒内就绪".into()); }
        sleep(cx, 350).await;
        // Step 1/2: collapse and expand the sidebar.
        self.click_target(cx, "sidebar-toggle", "Step 1/7 · 点击收起侧边栏").await?;
        self.check(1, self.query(cx, |w, _| w.sidebar_collapsed), "收起侧边栏")?;
        self.click_target(cx, "sidebar-toggle", "Step 2/7 · 点击展开侧边栏").await?;
        self.check(2, self.query(cx, |w, _| !w.sidebar_collapsed), "展开侧边栏")?;
        // Step 3: new note opens a second tab.
        sleep(cx, 120).await;
        self.click_target(cx, "new-note", "Step 3/7 · 点击新建笔记").await?;
        let opened = self.wait_until(cx, 2000, |w, _| !w.busy() && w.tab_count() >= 2).await;
        self.check(3, opened, "新建笔记并打开标签页")?;
        sleep(cx, 250).await;
        self.editor_step(cx).await?;
        self.overlay_steps(cx).await
    }

    async fn editor_step(&self, cx: &AsyncApp) -> Result<(), String> {
        let bounds = smoke_overlay::target_bounds("editor").ok_or("找不到点击目标 editor")?;
        let spot = point(bounds.center().x - px(120.), bounds.top() + px(96.));
        self.click_at(cx, spot, "Step 4/7 · 点击编辑区，输入 / 打开斜杠菜单").await?;
        self.press(cx, "/")?;
        sleep(cx, 300).await;
        let editor = self.query(cx, |w, _| w.editor.clone());
        let menu = cx.update(|cx| editor.read(cx).slash_menu_open());
        self.check(4, menu, "斜杠菜单已显示")?;
        self.press(cx, "down")?;
        sleep(cx, 120).await;
        self.press(cx, "up")?;
        sleep(cx, 120).await;
        self.press(cx, "enter")?;
        sleep(cx, 180).await;
        cx.update(|_| smoke_overlay::set_step(None, "Step 4/7 · 输入 SmokeClickOK"));
        self.redraw(cx);
        self.type_text(cx, "SmokeClickOK").await?;
        sleep(cx, 200).await;
        let text = cx.update(|cx| editor.read(cx).markdown_text());
        self.check(4, text.contains("SmokeClickOK"), "编辑区输入内容已写入文档")
    }

    async fn overlay_steps(&self, cx: &AsyncApp) -> Result<(), String> {
        self.click_target(cx, "search-button", "Step 5/7 · 点击打开搜索弹窗").await?;
        self.check(5, self.query(cx, |w, _| w.search_is_open()), "搜索弹窗已打开")?;
        sleep(cx, 260).await;
        self.press(cx, "escape")?;
        sleep(cx, 200).await;
        self.check(5, self.query(cx, |w, _| !w.search_is_open()), "Esc 关闭搜索弹窗")?;

        self.click_target(cx, "notebook-switcher", "Step 6/7 · 点击切换笔记本菜单").await?;
        self.check(6, self.query(cx, |w, _| w.switcher_is_open()), "笔记本菜单已打开")?;
        sleep(cx, 260).await;
        // Clicking the switcher again would close and instantly re-toggle it; click outside instead.
        let editor = smoke_overlay::target_bounds("editor").ok_or("找不到点击目标 editor")?;
        let outside = point(editor.right() - px(120.), editor.center().y);
        self.click_at(cx, outside, "Step 6/7 · 点击空白处收起笔记本菜单").await?;
        self.check(6, self.query(cx, |w, _| !w.switcher_is_open()), "笔记本菜单已关闭")?;

        self.click_target(cx, "settings-button", "Step 7/7 · 点击打开设置页").await?;
        self.check(7, self.query(cx, |w, _| w.settings_is_open()), "设置页已打开")?;
        sleep(cx, 300).await;
        self.click_target(cx, "settings-back", "Step 7/7 · 点击返回编辑").await?;
        self.check(7, self.query(cx, |w, _| !w.settings_is_open()), "返回编辑，设置页已关闭")
    }

    async fn finish(&self, outcome: Result<(), String>, sandbox: Option<PathBuf>, cx: &mut AsyncApp) {
        let cx = &*cx;
        let cleanup = |sandbox: &Option<PathBuf>| if let Some(dir) = sandbox { let _ = std::fs::remove_dir_all(dir); };
        match outcome {
            Ok(()) => {
                cx.update(|_| smoke_overlay::set_finished(true, format!("✓ 自动点击验收全部通过 ({STEPS}/{STEPS})")));
                self.redraw(cx);
                println!("[PASS] 自动点击验收全部通过 ({STEPS}/{STEPS})");
                sleep(cx, 750).await;
                if keep_open() { return; }
                // Flush the open note into the sandbox before deleting it, so quit has nothing left to write.
                let editor = self.query(cx, |w, _| w.editor.clone());
                cx.update(|cx| editor.update(cx, |e, _| e.prepare_to_quit()));
                cleanup(&sandbox);
                cx.update(|cx| cx.quit());
            }
            Err(reason) => {
                cx.update(|_| smoke_overlay::set_finished(false, format!("✗ 验收失败：{reason}")));
                self.redraw(cx);
                eprintln!("[FAIL] {reason}");
                sleep(cx, 1000).await;
                cleanup(&sandbox);
                std::process::exit(1);
            }
        }
    }
}
