//! "About Block" dialog (Scheme A · Centered Card): an in-window modal opened from
//! the app menu. Reads `crate::version::VERSION` (shared with Settings) so the
//! displayed version is never hardcoded in UI code.
use std::sync::{
    Arc, LazyLock,
    atomic::{AtomicBool, AtomicU8, AtomicU64, Ordering},
};
use gpui::{
    AnyElement, App, Context, FontWeight, Image, ImageFormat, MouseButton, SharedString, div, img,
    prelude::*, px,
};
use crate::{
    app::Workspace,
    icons::Icon,
    theme::{Theme, tr},
    ui::{icon_button::icon_button, modal},
    version::VERSION,
};

const PROFILE_URL: &str = "https://github.com/eachann1024";
const REPO_URL: &str = "https://github.com/eachann1024/block";

const HOVER_NONE: u8 = 0;
const HOVER_PROFILE: u8 = 1;
const HOVER_REPO: u8 = 2;

static OPEN: AtomicBool = AtomicBool::new(false);
static HOVERED: AtomicU8 = AtomicU8::new(HOVER_NONE);
static PROFILE_BURST: AtomicU64 = AtomicU64::new(0);
static REPO_BURST: AtomicU64 = AtomicU64::new(0);
static ICON: LazyLock<Arc<Image>> = LazyLock::new(|| {
    Arc::new(Image::from_bytes(
        ImageFormat::Png,
        include_bytes!("../assets/app-icon/apple-touch-icon.png").to_vec(),
    ))
});

pub fn open(cx: &mut App) {
    // Menu dispatch borrows the window; defer until it is available again.
    cx.defer(|cx| {
        crate::search::window::activate_main_window(cx);
        open_state();
        if let Some((Some(handle), workspace)) = crate::search::window::main_window(cx) {
            let _ = handle.update(cx, |_, window, cx| {
                workspace.update(cx, |w, cx| {
                    w.focus_root(window, cx);
                    cx.notify();
                });
            });
        }
    });
}

pub(crate) fn open_state() {
    HOVERED.store(HOVER_NONE, Ordering::Relaxed);
    PROFILE_BURST.store(0, Ordering::Relaxed);
    REPO_BURST.store(0, Ordering::Relaxed);
    OPEN.store(true, Ordering::Relaxed);
}

/// Closes the dialog; returns whether it was open.
pub fn close() -> bool {
    crate::performance::close();
    HOVERED.store(HOVER_NONE, Ordering::Relaxed);
    OPEN.swap(false, Ordering::Relaxed)
}

fn set_hover(slot: u8, active: bool, cx: &mut Context<Workspace>) {
    let prev = HOVERED.load(Ordering::Relaxed);
    let next = if active {
        slot
    } else if prev == slot {
        HOVER_NONE
    } else {
        prev
    };
    if prev != next {
        if active {
            match slot {
                HOVER_PROFILE => {
                    PROFILE_BURST.fetch_add(1, Ordering::Relaxed);
                }
                HOVER_REPO => {
                    REPO_BURST.fetch_add(1, Ordering::Relaxed);
                }
                _ => {}
            }
        }
        HOVERED.store(next, Ordering::Relaxed);
        cx.notify();
    }
}

pub fn view(cx: &mut Context<Workspace>) -> Option<AnyElement> {
    if !OPEN.load(Ordering::Relaxed) {
        return None;
    }
    let theme = Theme::ocean();
    let hovered = HOVERED.load(Ordering::Relaxed);
    let profile_seq = PROFILE_BURST.load(Ordering::Relaxed);
    let repo_seq = REPO_BURST.load(Ordering::Relaxed);
    let version_label: SharedString = format!("{} {VERSION}", tr("版本", "Version")).into();

    let close_top = div()
        .absolute()
        .top(px(14.))
        .right(px(14.))
        .child(icon_button(
            "about-close",
            Icon::Close,
            true,
            cx.listener(|this, _, window, cx| {
                close();
                this.restore_focus(window, cx);
                cx.notify();
            }),
        ));

    let hero = div()
        .w_full()
        .flex()
        .flex_col()
        .items_center()
        .gap(px(4.))
        .child(img(ICON.clone()).size(px(84.)).mb(px(6.)).flex_none())
        .child(
            div()
                .text_size(px(22.))
                .line_height(px(30.))
                .font_weight(FontWeight(560.))
                .text_color(theme.text)
                .child("Block"),
        )
        .child(
            div()
                .font_family("JetBrains Mono")
                .text_size(px(14.))
                .line_height(px(20.))
                .text_color(theme.text_secondary)
                .child(version_label),
        )
        .child(modal::author_puff_link(
            "about-profile",
            tr("作者 eachann", "Made by eachann"),
            PROFILE_URL,
            hovered == HOVER_PROFILE,
            profile_seq,
            cx.listener(|_, active: &bool, _, cx| set_hover(HOVER_PROFILE, *active, cx)),
        ));

    let links = div()
        .w_full()
        .flex()
        .flex_col()
        .gap(px(12.))
        .pt(px(16.))
        .pb(px(4.))
        .border_t_1()
        .border_color(theme.hairline)
        .child(modal::bubble_link_pill(
            "about-repo",
            Icon::Code,
            tr("项目源码", "Project Source"),
            "eachann1024/block",
            REPO_URL,
            hovered == HOVER_REPO,
            repo_seq,
            cx.listener(|_, active: &bool, _, cx| set_hover(HOVER_REPO, *active, cx)),
        ))
        .child(modal::action_button(
            "about-performance",
            tr("性能与占用", "Performance & Usage"),
            cx.listener(|this, _, _, cx| {
                crate::performance::open(this.performance_folders(), cx);
            }),
        ));

    let close_btn = modal::action_button(
        "about-done",
        tr("关闭", "Close"),
        cx.listener(|this, _, window, cx| {
            close();
            this.restore_focus(window, cx);
            cx.notify();
        }),
    );

    let card = modal::card("about-dialog", px(368.))
        .relative()
        .pt(px(28.))
        .px(px(24.))
        .pb(px(22.))
        .gap(px(18.))
        .items_center()
        .text_size(px(14.))
        .line_height(px(20.))
        .child(close_top)
        .child(hero)
        .child(links)
        .child(close_btn);

    Some(
        gpui::deferred(
            modal::backdrop("about-overlay", card).on_mouse_down(
                MouseButton::Left,
                cx.listener(|this, _, window, cx| {
                    close();
                    this.restore_focus(window, cx);
                    cx.notify();
                }),
            ),
        )
        .with_priority(6)
        .into_any_element(),
    )
}
