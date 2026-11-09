//! WebAssembly runtime CDN font fetcher (`window.fetch` -> `ArrayBuffer` -> `Uint8Array`).

use std::{
    cell::RefCell,
    future::poll_fn,
    rc::Rc,
    task::{Poll, Waker},
};
use gpui::{App, Entity};
use wasm_bindgen::{JsCast, JsValue, closure::Closure};
use crate::app::Workspace;
use super::{
    FontSpec, IBM_PLEX_SANS_400, IBM_PLEX_SANS_600,
    JETBRAINS_MONO_400, JETBRAINS_MONO_500, STIX_TWO_MATH_400,
    is_valid_sfnt, mark_math_failed, register_export_font, set_math_font,
};

async fn await_promise(promise: js_sys::Promise) -> Result<JsValue, JsValue> {
    let state: Rc<RefCell<(Option<Result<JsValue, JsValue>>, Option<Waker>)>> =
        Rc::new(RefCell::new((None, None)));
    let s_ok = state.clone();
    let on_ok = Closure::once(move |val: JsValue| {
        let mut guard = s_ok.borrow_mut();
        guard.0 = Some(Ok(val));
        if let Some(waker) = guard.1.take() {
            waker.wake();
        }
    });
    let s_err = state.clone();
    let on_err = Closure::once(move |err: JsValue| {
        let mut guard = s_err.borrow_mut();
        guard.0 = Some(Err(err));
        if let Some(waker) = guard.1.take() {
            waker.wake();
        }
    });
    let _ = promise.then2(&on_ok, &on_err);
    on_ok.forget();
    on_err.forget();
    poll_fn(move |cx| {
        let mut guard = state.borrow_mut();
        if let Some(res) = guard.0.take() {
            Poll::Ready(res)
        } else {
            guard.1 = Some(cx.waker().clone());
            Poll::Pending
        }
    })
    .await
}

async fn fetch_url(url: &str) -> Result<Vec<u8>, String> {
    let win = web_sys::window().ok_or_else(|| String::from("no window"))?;
    let fetch_fn = js_sys::Reflect::get(&win, &JsValue::from_str("fetch"))
        .ok()
        .and_then(|v| v.dyn_into::<js_sys::Function>().ok())
        .ok_or_else(|| String::from("window.fetch unavailable"))?;
    let p = fetch_fn
        .call1(&win, &JsValue::from_str(url))
        .ok()
        .and_then(|v| v.dyn_into::<js_sys::Promise>().ok())
        .ok_or_else(|| format!("fetch call failed: {url}"))?;
    let resp = await_promise(p).await.map_err(|e| format!("fetch error {url}: {e:?}"))?;
    let ok = js_sys::Reflect::get(&resp, &JsValue::from_str("ok"))
        .ok()
        .and_then(|v| v.as_bool())
        .unwrap_or(false);
    if !ok {
        return Err(format!("HTTP error for {url}"));
    }
    let ab_fn = js_sys::Reflect::get(&resp, &JsValue::from_str("arrayBuffer"))
        .ok()
        .and_then(|v| v.dyn_into::<js_sys::Function>().ok())
        .ok_or_else(|| String::from("Response.arrayBuffer unavailable"))?;
    let ab_p = ab_fn
        .call0(&resp)
        .ok()
        .and_then(|v| v.dyn_into::<js_sys::Promise>().ok())
        .ok_or_else(|| String::from("arrayBuffer call failed"))?;
    let buf = await_promise(ab_p).await.map_err(|e| format!("arrayBuffer error: {e:?}"))?;
    let bytes = js_sys::Uint8Array::new(&buf).to_vec();
    if !is_valid_sfnt(&bytes) {
        return Err(format!("invalid sfnt header from {url}"));
    }
    Ok(bytes)
}

fn local_font_urls(filename: &str) -> Vec<String> {
    let mut names = vec![filename];
    match filename {
        "ibm-plex-sans-latin-400-normal.ttf" => names.push("IBMPlexSans-Regular.ttf"),
        "ibm-plex-sans-latin-600-normal.ttf" => names.push("IBMPlexSans-SemiBold.ttf"),
        "jetbrains-mono-latin-400-normal.ttf" => names.push("JetBrainsMono-Regular.ttf"),
        "jetbrains-mono-latin-500-normal.ttf" => names.push("JetBrainsMono-Medium.ttf"),
        _ => {}
    }
    let mut urls = Vec::with_capacity(names.len() * 2);
    for name in names {
        urls.push(format!("./editor/fonts/{name}"));
        urls.push(format!("./fonts/{name}"));
    }
    urls
}

async fn fetch_spec(spec: FontSpec, errors: &mut Vec<String>) -> Option<Vec<u8>> {
    for url in local_font_urls(spec.filename) {
        if let Ok(bytes) = fetch_url(&url).await {
            return Some(bytes);
        }
    }
    let mut last_err = String::new();
    for url in spec.urls() {
        if !url.ends_with(".ttf") {
            continue;
        }
        match fetch_url(&url).await {
            Ok(bytes) => return Some(bytes),
            Err(err) => last_err = err,
        }
    }
    if !last_err.is_empty() {
        errors.push(format!("{}: {last_err}", spec.filename));
    }
    None
}

/// Fetches Latin UI & monospace fonts before resolving `.SystemUIFont` on web.
pub async fn load_ui_fonts() -> Vec<&'static [u8]> {
    let mut errors = Vec::new();
    let mut out = Vec::new();
    for spec in [
        IBM_PLEX_SANS_400,
        IBM_PLEX_SANS_600,
        JETBRAINS_MONO_400,
        JETBRAINS_MONO_500,
    ] {
        if let Some(bytes) = fetch_spec(spec, &mut errors).await {
            out.push(register_export_font(bytes));
        }
    }
    if !out.is_empty() {
        crate::export::reset_font_system();
    }
    if !errors.is_empty() {
        crate::log::warn(&format!("CDN UI font fetch incomplete: {}", errors.join("; ")));
    }
    out
}

/// Fetches STIX Two Math in the background after the web workspace opens.
pub fn spawn(workspace: Entity<Workspace>, cx: &mut App) {
    let weak = workspace.downgrade();
    cx.spawn(async move |cx| {
        let mut errors = Vec::new();
        let math_ok = match fetch_spec(STIX_TWO_MATH_400, &mut errors).await {
            Some(bytes) => set_math_font(bytes),
            None => {
                mark_math_failed();
                false
            }
        };
        if !errors.is_empty() {
            crate::log::warn(&format!("CDN math font fetch incomplete: {}", errors.join("; ")));
        }
        if math_ok || super::math_font_failed() {
            cx.update(|cx| {
                if let Some(ws) = weak.upgrade() {
                    ws.update(cx, |w, cx| {
                        w.editor.update(cx, |_, cx| cx.notify());
                        cx.notify();
                    });
                }
            });
        }
    })
    .detach();
}
