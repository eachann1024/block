//! "About Block" dialog: an in-window modal opened from the app menu.
//! Native About panels cannot hold clickable links, so this one is drawn here.
use std::sync::{Arc, LazyLock, atomic::{AtomicBool, Ordering}};
use gpui::{
    AnyElement, App, Context, CursorStyle, Image, ImageFormat, MouseButton, SharedString, div, img,
    prelude::*, px,
};
use crate::{app::Workspace, icons::Icon, theme::{Theme, tr}, ui::{icon_button::icon_button, modal}};

const PROFILE_URL: &str = "https://github.com/eachann1024";
const REPO_URL: &str = "https://github.com/eachann1024/block";

static OPEN: AtomicBool = AtomicBool::new(false);
static ICON: LazyLock<Arc<Image>> = LazyLock::new(|| {
    Arc::new(Image::from_bytes(ImageFormat::Png, include_bytes!("../assets/app-icon/apple-touch-icon.png").to_vec()))
});

pub fn open(cx: &mut App) {
    crate::search::window::activate_main_window(cx);
    OPEN.store(true, Ordering::Relaxed);
    if let Some((Some(handle), workspace)) = crate::search::window::main_window(cx) {
        let _ = handle.update(cx, |_, window, cx| {
            workspace.update(cx, |w, cx| { w.focus_root(window, cx); cx.notify(); });
        });
    }
}

/// Closes the dialog; returns whether it was open.
pub fn close() -> bool { OPEN.swap(false, Ordering::Relaxed) }

fn row(label: &'static str, value: impl IntoElement) -> impl IntoElement {
    let theme = Theme::ocean();
    div().min_h(px(40.)).px(px(4.)).flex().items_center()
        .child(div().w(px(96.)).flex_none().text_color(theme.text_secondary).child(label))
        .child(div().flex_1().min_w_0().child(value))
}

fn link(id: &'static str, text: &'static str, url: &'static str) -> impl IntoElement {
    let theme = Theme::ocean();
    div().id(id).text_color(theme.accent).cursor(CursorStyle::PointingHand)
        .hover(|s| s.underline())
        .on_mouse_down(MouseButton::Left, move |_, _, cx| { cx.stop_propagation(); cx.open_url(url); })
        .child(text)
}

pub fn view(cx: &mut Context<Workspace>) -> Option<AnyElement> {
    if !OPEN.load(Ordering::Relaxed) { return None; }
    let theme = Theme::ocean();
    let close_btn = icon_button("about-close", Icon::Close, true, cx.listener(|this, _, window, cx| {
        close();
        this.restore_focus(window, cx);
        cx.notify();
    }));
    let header = div().flex().items_center().gap(px(12.))
        .child(img(ICON.clone()).size(px(40.)).flex_none())
        .child(div().flex_1().text_size(px(18.)).line_height(px(26.)).font_weight(gpui::FontWeight(560.)).child("Block"))
        .child(close_btn);
    let version: SharedString = env!("CARGO_PKG_VERSION").into();
    let list = div().flex().flex_col().pt(px(8.)).border_t_1().border_color(theme.hairline)
        .child(row(tr("版本", "Version"), div().font_family("JetBrains Mono").child(version)))
        .child(row(tr("作者", "Author"), "eachann"))
        .child(row("GitHub", link("about-profile", "github.com/eachann1024", PROFILE_URL)))
        .child(row(tr("源码", "Source"), link("about-repo", "eachann1024/block", REPO_URL)))
        .child(row(tr("许可证", "License"), "Apache-2.0"));
    let card = modal::card("about-dialog", px(400.))
        .p(px(24.)).gap(px(16.)).text_size(px(14.)).line_height(px(20.))
        .child(header)
        .child(list);
    Some(gpui::deferred(modal::backdrop("about-overlay", card)
        .on_mouse_down(MouseButton::Left, cx.listener(|this, _, window, cx| {
            close();
            this.restore_focus(window, cx);
            cx.notify();
        })))
        .with_priority(6)
        .into_any_element())
}
