use gpui::{AppContext, AnyWindowHandle, Bounds, WindowBounds, WindowOptions, PlatformInput,
    ScrollEvent, ScrollDelta, Pixels, Point, point, px, size};
use crate::editor::Editor;
use super::{AsyncApp, Run, sleep};

fn draw(handle: AnyWindowHandle, cx: &AsyncApp, event: Option<ScrollEvent>) -> Result<(), String> {
    cx.update(|cx| handle.update(cx, |_, window, cx| {
        if let Some(event) = event { window.dispatch_event(PlatformInput::Scroll(event), cx); }
        window.refresh(); window.draw(cx).clear(cx);
    })).map_err(|e| e.to_string())
}
fn geometry(handle: AnyWindowHandle, cx: &AsyncApp) -> Result<(Bounds<Pixels>, Option<Bounds<Pixels>>), String> {
    cx.update(|cx| handle.read::<Editor, _, _>(cx, |editor, cx| editor.read(cx).scroll_smoke_geometry())).map_err(|e| e.to_string())
}
async fn wheel(handle: AnyWindowHandle, cx: &AsyncApp, position: Point<Pixels>, delta: f32) -> Result<(), String> {
    draw(handle, cx, Some(ScrollEvent {
        position, delta: ScrollDelta::Pixels(point(px(0.), px(delta))), ..Default::default()
    }))?;
    sleep(cx, 16).await;
    draw(handle, cx, None)
}
async fn verify(handle: AnyWindowHandle, cx: &AsyncApp) -> Result<(), String> {
    draw(handle, cx, None)?;
    sleep(cx, 100).await;
    draw(handle, cx, None)?;
    let (viewport, _) = geometry(handle, cx)?;
    let body = point(viewport.right() - px(18.), viewport.bottom() - px(18.));
    for _ in 0..128 {
        wheel(handle, cx, body, -2000.).await?;
        if geometry(handle, cx)?.1.is_some() { break; }
    }
    for _ in 0..4 { wheel(handle, cx, body, -2000.).await?; }
    cx.update(|cx| handle.update(cx, |view, _, cx| {
        view.downcast::<Editor>().unwrap().update(cx, |e, cx| e.scroll_smoke_menu(cx));
    })).map_err(|e| e.to_string())?;
    draw(handle, cx, None)?; sleep(cx, 250).await; draw(handle, cx, None)?;
    let menu_bounds = || super::smoke_overlay::target_bounds("block-menu").ok_or("菜单不可见".to_owned());
    let initial = menu_bounds()?;
    let position = initial.center();
    for _ in 0..8 { wheel(handle, cx, position, -1000.).await?; }
    if menu_bounds()? == initial { return Err("菜单未产生内部滚动".into()); }
    let document = geometry(handle, cx)?.1;
    for down in [true, false] {
        if !down { for _ in 0..4 { wheel(handle, cx, position, 1000.).await?; } }
        let baseline = menu_bounds()?;
        for i in 0..60 {
            let delta = if i % 2 == 0 { 17.25 } else { 0.75 } * if down { -1. } else { 1. };
            let (offset, limit) = cx.update(|cx| handle.update(cx, |view, window, cx| {
                window.dispatch_event(PlatformInput::Scroll(ScrollEvent { position,
                    delta: ScrollDelta::Pixels(point(px(0.), px(delta))), ..Default::default() }), cx);
                view.downcast::<Editor>().unwrap().read(cx).menu_scroll_smoke_offset()
            })).map_err(|e| e.to_string())?;
            if offset.y < -limit.y || offset.y > px(0.) {
                return Err(format!("菜单边界事件后偏移越界: {:.2}, 下限 {:.2}", f32::from(offset.y), f32::from(-limit.y)));
            }
            draw(handle, cx, None)?;
            let current = menu_bounds()?;
            if current != baseline {
                return Err(format!("菜单边界抖动 frame={i}: y={:.3} -> {:.3}", f32::from(baseline.top()), f32::from(current.top())));
            }
            sleep(cx, 16).await; draw(handle, cx, None)?;
            if menu_bounds()? != baseline { return Err("菜单触边后重绘改变位置".into()); }
            if geometry(handle, cx)?.1 != document { return Err("菜单滚动传递到正文".into()); }
        }
        wheel(handle, cx, position, if down { 40. } else { -40. }).await?;
        if menu_bounds()? == baseline { return Err("菜单触边后无法反向滚动".into()); }
    }
    Ok(())
}
async fn compact_case(cx: &AsyncApp) -> Result<(), String> {
    let handle = cx.update(|cx| cx.open_window(WindowOptions {
        window_bounds: Some(WindowBounds::Windowed(Bounds::new(point(px(0.), px(0.)), size(px(380.), px(371.))))),
        show: false, focus: false, ..Default::default()
    }, |window, cx| cx.new(|cx| {
        let mut editor = Editor::new(cx);
        editor.prepare_scroll_smoke(super::sandbox_path().join("notebook/scroll.md"), window, cx);
        editor
    }))).map_err(|e| e.to_string())?;
    let handle: AnyWindowHandle = handle.into();
    let result = verify(handle, cx).await;
    cx.update(|cx| handle.update(cx, |_, window, _| window.remove_window())).map_err(|e| e.to_string())?;
    result
}
impl Run {
    pub(super) async fn scroll_edges(&self, cx: &AsyncApp) -> Result<(), String> {
        let main_zoom = crate::editor::zoom::zoom_factor(false);
        let quick_zoom = crate::editor::zoom::zoom_factor(true);
        let mut result = Ok(());
        for zoom in [0.9, 1.0] {
            crate::editor::init_zoom(Some(main_zoom), Some(zoom));
            result = compact_case(cx).await;
            if result.is_err() { break; }
        }
        crate::editor::init_zoom(Some(main_zoom), Some(quick_zoom));
        self.redraw(cx);
        result?;
        self.check(9, true, "小窗菜单上下边界连续滚动稳定，反向滚动正常且正文不移动（90%/100%）")
    }
}
