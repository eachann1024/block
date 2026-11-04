//! `--smoke`: a visible, scripted click and keyboard pass over the real window.
//! It runs against a throw-away sandbox, never the user's notes or `state.json`.
//! This file is the runner; the scripted steps live in `smoke_steps.rs`.
use std::{path::PathBuf, time::Duration};
use gpui::{
    AnyWindowHandle, App, AppContext, AsyncApp, Entity, Keystroke, Modifiers, MouseButton, MouseDownEvent,
    MouseMoveEvent, MouseUpEvent, PlatformInput, Pixels, Point, Window,
};
use crate::{app::Workspace, editor::Editor, ui::smoke_overlay};

#[path = "smoke_steps.rs"]
mod steps;

const STEPS: usize = 12;

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
        std::fs::write(&welcome, "# Yu\n\n点击验证初始段落。\n")?;
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

    fn editor<R>(&self, cx: &AsyncApp, f: impl FnOnce(&Editor) -> R) -> R {
        cx.update(|cx| f(self.workspace.read(cx).editor.read(cx)))
    }

    fn redraw(&self, cx: &AsyncApp) {
        cx.update(|cx| self.workspace.update(cx, |_, cx| cx.notify()));
    }

    /// Shows `label` in the HUD without a click ripple.
    fn hud(&self, cx: &AsyncApp, label: &str) {
        cx.update(|_| smoke_overlay::set_step(None, label.to_owned()));
        self.redraw(cx);
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

    /// One key press, e.g. "enter" or "cmd-z"; a short pause lets the UI settle.
    async fn press(&self, cx: &AsyncApp, key: &'static str) -> Result<(), String> {
        let stroke = Keystroke::parse(key).map_err(|e| format!("无效按键 {key}: {e}"))?;
        self.win(cx, |window, cx| { window.dispatch_keystroke(stroke, cx); })?;
        sleep(cx, 110).await;
        Ok(())
    }

    async fn type_text(&self, cx: &AsyncApp, text: &str) -> Result<(), String> {
        for ch in text.chars() {
            let key = if ch == ' ' { "space".to_owned() } else { ch.to_lowercase().to_string() };
            let stroke = Keystroke { modifiers: Modifiers { shift: ch.is_uppercase(), ..Modifiers::default() }, key, key_char: None };
            self.win(cx, |window, cx| { window.dispatch_keystroke(stroke, cx); })?;
            sleep(cx, 35).await;
        }
        sleep(cx, 120).await;
        Ok(())
    }

    fn check(&self, n: usize, ok: bool, title: &str) -> Result<(), String> {
        if !ok { return Err(format!("Step {n}/{STEPS}: {title}")); }
        println!("[PASS] Step {n}/{STEPS}: {title}");
        Ok(())
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
