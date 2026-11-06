//! `--smoke`: queued scripted checks in a background resident worker.
//! `--smoke-once` joins the same queue on Unix; other platforms use a single-run window.
//! This file is the runner; the scripted steps live in `smoke_steps.rs`.
use std::{path::PathBuf, time::Duration};
use gpui::{
    AnyWindowHandle, App, AppContext, AsyncApp, Entity, Keystroke, Modifiers, MouseButton, MouseDownEvent,
    MouseMoveEvent, MouseUpEvent, PlatformInput, Pixels, Point, Window,
};
use crate::{app::Workspace, editor::Editor, ui::smoke_overlay};

#[path = "smoke_annotations.rs"] mod annotations;
#[path = "smoke_steps.rs"]
mod steps;
#[path = "smoke_chat_selection.rs"] mod chat_selection;
#[cfg(unix)]
#[path = "smoke_ipc.rs"] mod ipc;
#[path = "smoke_client.rs"] mod client;
#[path = "smoke_worker.rs"] mod worker;

pub fn handle_cli() -> bool { client::handle() }
pub fn is_worker() -> bool { std::env::args().any(|a| a == "--smoke-worker") }

const STEPS: usize = 14;

pub fn is_requested() -> bool { std::env::args().any(|a| matches!(a.as_str(), "--smoke" | "--smoke-test" | "--smoke-worker" | "--smoke-once")) }
pub fn keep_open() -> bool { std::env::args().any(|a| a == "--keep-open") }

/// Creates the sandbox notebook and redirects all preferences into it.
pub fn init_sandbox() -> Option<PathBuf> {
    if !is_requested() { return None; }
    smoke_overlay::enable_smoke_mode();
    if let Err(error) = reset_sandbox() {
        eprintln!("[FAIL] 无法创建冒烟沙箱: {error}");
        std::process::exit(1);
    }
    Some(sandbox_path())
}

fn sandbox_path() -> PathBuf { PathBuf::from("/tmp").join(format!("block-smoke-{}", std::process::id())) }
fn reset_sandbox() -> std::io::Result<()> {
    let sandbox = sandbox_path();
    let notebook = sandbox.join("notebook");
    // Detached history writers may still use sandbox/data after the editor closes.
    match std::fs::remove_dir_all(&notebook) {
        Ok(()) => (), Err(e) if e.kind() == std::io::ErrorKind::NotFound => (), Err(e) => return Err(e),
    }
    std::fs::create_dir_all(&notebook)?;
    let welcome = notebook.join("Welcome.md");
    std::fs::write(&welcome, "# Block\n\n点击验证初始段落。\n")?;
    crate::files::state::set_state_path_override(sandbox.join("state.json"));
    crate::editor::ai::provider::save(&Default::default()).map_err(std::io::Error::other)?;
    std::fs::write(notebook.join("notes.txt"), "plain text\n")?;
    crate::files::update_state(|s| {
        *s = crate::files::state::State::default();
        s.folder = Some(notebook.clone());
        s.notebooks = vec![notebook];
        s.open_tabs = vec![welcome.clone()];
        s.active_tab = Some(welcome);
    }).map(|_| ())
}

pub fn spawn(window: AnyWindowHandle, workspace: Entity<Workspace>, sandbox: Option<PathBuf>, cx: &mut App) {
    let run = Run { window, workspace };
    #[cfg(unix)]
    if is_worker() { worker::spawn(run, cx); return; }
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
        if is_worker() { worker::draw(self, cx); }
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
        if is_worker() { worker::draw(self, cx); }
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

#[path = "smoke_chat_composer.rs"] mod chat_composer;
