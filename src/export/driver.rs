//! Editor-facing orchestration. Heavy work runs exclusively on the background executor.
use super::view::Editor;
use crate::export::{ExportError, ExportJob, export_to_path, preflight};
use gpui::{Context, Window};

pub fn export_document(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    export(editor, false, window, cx);
}

pub fn export_selected(editor: &mut Editor, window: &mut Window, cx: &mut Context<Editor>) {
    export(editor, true, window, cx);
}

fn export(
    editor: &mut Editor,
    selected_only: bool,
    _window: &mut Window,
    cx: &mut Context<Editor>,
) {
    // Count first: a million-block document is rejected without walking or cloning it.
    let count = editor.export_block_count(selected_only);
    if let Err(error) = preflight(count, 0, 0) {
        editor.report_status(error.to_string(), cx);
        return;
    }
    let (bytes, spans) = editor.export_budget(selected_only);
    if let Err(error) = preflight(count, bytes, spans) {
        editor.report_status(error.to_string(), cx);
        return;
    }
    let job = match ExportJob::begin() {
        Ok(job) => job,
        Err(error) => {
            editor.report_status(error.to_string(), cx);
            return;
        }
    };
    // Only this bounded snapshot occurs on the UI thread. Keep the exact selection
    // at invocation, even if the user changes documents while the dialog is open.
    let blocks = editor.export_blocks(selected_only);
    let directory = std::env::var_os("HOME")
        .map(std::path::PathBuf::from)
        .unwrap_or_default();
    let prompt = cx.prompt_for_new_path(&directory, Some("note.png"));
    editor.report_status("Choose where to save the PNG image".into(), cx);
    cx.spawn(async move |editor, cx| {
        let path = match prompt.await {
            Ok(Ok(Some(path))) => path,
            Ok(Ok(None)) => {
                let _ = editor.update(cx, |editor, cx| {
                    editor.report_status(ExportError::Cancelled.to_string(), cx)
                });
                return;
            }
            Ok(Err(error)) => {
                let _ = editor.update(cx, |editor, cx| {
                    editor.report_status(ExportError::Dialog(error.to_string()).to_string(), cx)
                });
                return;
            }
            Err(error) => {
                let _ = editor.update(cx, |editor, cx| {
                    editor.report_status(ExportError::Dialog(error.to_string()).to_string(), cx)
                });
                return;
            }
        };
        if editor
            .update(cx, |editor, cx| {
                editor.report_status("Rendering and saving PNG…".into(), cx)
            })
            .is_err()
        {
            job.cancellation.cancel();
            return;
        }
        let result = cx
            .background_executor()
            .spawn(async move {
                // Holding job also prevents a second dialog/snapshot until completion.
                let result = export_to_path(&blocks, &path, &job.cancellation);
                drop(job);
                result
            })
            .await;
        let message = match result {
            Ok(report) => format!("Exported PNG to {}", report.path.display()),
            Err(error) => error.to_string(),
        };
        let _ = editor.update(cx, |editor, cx| editor.report_status(message, cx));
    })
    .detach();
}
