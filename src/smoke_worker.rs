//! One serial GPUI consumer and an active-job frame pump for the hidden window.
use super::*;
#[cfg(unix)]
use super::ipc::{self, Request};

pub(super) fn draw(run: &Run, cx: &AsyncApp) {
    let _ = run.win(cx, |window, cx| { window.refresh(); window.draw(cx).clear(cx); });
}

#[cfg(unix)]
pub(super) fn spawn(run: Run, cx: &mut App) {
    let receiver = ipc::receiver();
    cx.spawn(async move |cx| {
        loop {
            let request = match receiver.try_recv() {
                Ok(request) => request,
                Err(std::sync::mpsc::TryRecvError::Empty) => { sleep(cx, 50).await; continue; }
                Err(_) => break,
            };
            let Request::Run(job) = request else { cx.update(|cx| cx.quit()); break };
            ipc::active(true);
            let pumping = std::rc::Rc::new(std::cell::Cell::new(true));
            let pump_flag = pumping.clone();
            let pump_run = Run { window: run.window, workspace: run.workspace.clone() };
            let pump = cx.spawn(async move |cx| {
                while pump_flag.get() { draw(&pump_run, cx); sleep(cx, 25).await; }
            });
            let outcome = match reset(&run, cx).await {
                Ok(()) => run.all_steps(cx).await,
                Err(error) => Err(error),
            };
            cx.update(|_| smoke_overlay::set_finished(outcome.is_ok(), match &outcome {
                Ok(()) => "Smoke passed".to_owned(), Err(error) => format!("Smoke failed: {error}"),
            }));
            draw(&run, cx);
            pumping.set(false);
            drop(pump);
            ipc::reply(job, &outcome);
            ipc::active(false);
        }
    }).detach();
}

#[cfg(unix)]
async fn reset(run: &Run, cx: &AsyncApp) -> Result<(), String> {
    if !run.wait_until(cx, 10000, |w, cx| {
        let editor = w.editor.read(cx);
        !w.busy() && !editor.is_busy() && !editor.is_loading()
    }).await {
        return Err("previous workspace did not become idle".into());
    }
    run.win(cx, |window, cx| {
        run.workspace.update(cx, |workspace, cx| workspace.smoke_close_workspace(window, cx));
    })?;
    if !run.wait_until(cx, 10000, |w, cx| {
        let editor = w.editor.read(cx);
        !w.busy() && !editor.is_busy() && !editor.is_loading() && editor.current_path().is_none()
    }).await {
        return Err("previous editor did not close".into());
    }
    reset_sandbox().map_err(|e| format!("sandbox reset failed: {e}"))?;
    let language = crate::theme::language_mode();
    let state = crate::files::state::State { language, ..Default::default() };
    crate::theme::apply_state(&state);
    smoke_overlay::reset();
    run.win(cx, |_, cx| {
        crate::theme::sync_gpui_theme(cx);
        run.workspace.update(cx, |workspace, cx| {
            workspace.smoke_open_notebook(sandbox_path().join("notebook"), cx);
        });
    })?;
    draw(run, cx);
    Ok(())
}
