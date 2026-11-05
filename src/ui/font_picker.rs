//! Universal font picker dropdown: trigger + search + grouped fonts list + import button.
use std::cell::RefCell;
use std::collections::{HashMap, HashSet};
use gpui::{
    Bounds, CursorStyle, ElementId, MouseButton, Pixels, SharedString, Size,
    Stateful, anchored, deferred, div, point, prelude::*, px,
};
use gpui_component::input::Input;
use crate::app::Workspace;
use crate::icons::{Icon, IconColors, icon};
use crate::theme::{Theme, tr};
use crate::ui::dropdown::{self, group_label, palette};

pub const FONT_MENU_WIDTH: f32 = 300.;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum FontPickerRole {
    Body,
    Code,
}

thread_local! {
    static TRIGGER_BOUNDS: RefCell<HashMap<FontPickerRole, (Bounds<Pixels>, Size<Pixels>)>> = RefCell::new(HashMap::new());
    static PICKER_ERROR: RefCell<Option<String>> = const { RefCell::new(None) };
}

pub fn record_trigger_bounds(role: FontPickerRole, bounds: Bounds<Pixels>, vp: Size<Pixels>) {
    TRIGGER_BOUNDS.with(|slot| { slot.borrow_mut().insert(role, (bounds, vp)); });
}

pub fn get_trigger_bounds(role: FontPickerRole) -> Option<(Bounds<Pixels>, Size<Pixels>)> {
    TRIGGER_BOUNDS.with(|slot| slot.borrow().get(&role).copied())
}

pub fn set_font_picker_error(err: Option<String>) {
    PICKER_ERROR.with(|slot| *slot.borrow_mut() = err);
}

pub fn font_picker_error() -> Option<String> {
    PICKER_ERROR.with(|slot| slot.borrow().clone())
}

pub fn font_picker_trigger(
    id: impl Into<ElementId>,
    role: FontPickerRole,
    open: bool,
    display_name: SharedString,
    display_family: SharedString,
    cx: &mut gpui::Context<Workspace>,
) -> Stateful<gpui::Div> {
    let theme = Theme::ocean();
    let border_color = if open { theme.accent } else { theme.btn_border };
    let chevron_icon = if open { Icon::ChevronUp } else { Icon::ChevronDown };
    div()
        .id(id).w(px(248.)).h(px(36.)).px(px(10.)).pl(px(12.))
        .rounded(crate::ui::radius::CONTROL).bg(theme.shell)
        .border_1().border_color(border_color)
        .flex().items_center().justify_between().cursor(CursorStyle::PointingHand)
        .hover(move |s| s.bg(theme.hover))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.toggle_font_picker(role, window, cx);
        }))
        .child(
            div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                .text_size(px(15.)).font_family(display_family).text_color(theme.text).child(display_name),
        )
        .child(div().flex_none().ml(px(8.)).child(icon(chevron_icon, px(14.), theme.mono_icon_colors(theme.text_secondary))))
        .child(
            gpui::canvas(
                move |bounds, window, _| record_trigger_bounds(role, bounds, window.viewport_size()),
                |_, _, _, _| {},
            )
            .absolute().top_0().left_0().size_full(),
        )
}

struct ItemSpec {
    id: Option<String>,
    name: SharedString,
    family: SharedString,
    selected: bool,
}

struct GroupSpec {
    title: SharedString,
    items: Vec<ItemSpec>,
}

fn collect_groups(role: FontPickerRole, query: &str) -> Vec<GroupSpec> {
    let q = query.trim().to_lowercase();
    let imported = crate::theme::imported_fonts();
    let installed = crate::theme::installed_fonts();
    let imported_set: HashSet<String> = imported.iter().map(|f| f.to_lowercase()).collect();
    let installed_deduped: Vec<String> = installed.into_iter().filter(|f| !imported_set.contains(&f.to_lowercase())).collect();

    let make_items = |list: Vec<String>, cur: Option<&str>| -> Vec<ItemSpec> {
        list.into_iter().map(|f| ItemSpec {
            id: Some(f.clone()),
            name: f.clone().into(),
            family: f.clone().into(),
            selected: cur == Some(&f),
        }).filter(|it| q.is_empty() || it.name.to_lowercase().contains(&q)).collect()
    };

    let mut groups = Vec::new();
    match role {
        FontPickerRole::Body => {
            let cur = crate::theme::custom_editor_font();
            let mut sys = vec![
                ItemSpec { id: None, name: tr("系统无衬线", "System Sans").into(), family: ".SystemUIFont".into(), selected: cur.is_none() },
                ItemSpec { id: Some("system-serif".into()), name: tr("系统衬线", "System Serif").into(), family: crate::theme::resolved_serif_font().into(), selected: cur.as_deref() == Some("system-serif") },
            ];
            sys.retain(|it| q.is_empty() || it.name.to_lowercase().contains(&q));
            if !sys.is_empty() { groups.push(GroupSpec { title: tr("系统", "System").into(), items: sys }); }

            let imp = make_items(imported, cur.as_deref());
            if !imp.is_empty() { groups.push(GroupSpec { title: tr("已导入", "Imported").into(), items: imp }); }

            let inst = make_items(installed_deduped, cur.as_deref());
            if !inst.is_empty() { groups.push(GroupSpec { title: tr("已安装", "Installed").into(), items: inst }); }
        }
        FontPickerRole::Code => {
            let cur = crate::theme::custom_code_font();
            let mut sys = vec![ItemSpec {
                id: None,
                name: format!("{} ({})", tr("自动", "Auto"), crate::theme::resolved_mono_font()).into(),
                family: crate::theme::resolved_mono_font().into(),
                selected: cur.is_none(),
            }];
            sys.retain(|it| q.is_empty() || it.name.to_lowercase().contains(&q));
            if !sys.is_empty() { groups.push(GroupSpec { title: tr("系统", "System").into(), items: sys }); }

            let imp = make_items(imported, cur.as_deref());
            if !imp.is_empty() { groups.push(GroupSpec { title: tr("已导入", "Imported").into(), items: imp }); }

            let mono_src: Vec<String> = installed_deduped.into_iter().filter(|f| crate::theme::is_monospace_name(f)).collect();
            let mono = make_items(mono_src, cur.as_deref());
            if !mono.is_empty() { groups.push(GroupSpec { title: tr("等宽", "Monospace").into(), items: mono }); }
        }
    }
    groups
}

pub fn font_picker_popover(
    role: FontPickerRole,
    search_input: &gpui::Entity<gpui_component::input::InputState>,
    query: &str,
    cx: &mut gpui::Context<Workspace>,
) -> gpui::Div {
    let theme = Theme::ocean();
    let pal = palette();
    let measured = get_trigger_bounds(role);
    let max_h = measured.map(|(_, vp)| (f32::from(vp.height) - 16.).clamp(80., dropdown::MAX_HEIGHT)).unwrap_or(dropdown::MAX_HEIGHT);
    let anchor = measured.map(|(b, vp)| dropdown::anchor_below_right(b, FONT_MENU_WIDTH, max_h, Some(vp))).unwrap_or(point(px(100.), px(100.)));
    let groups = collect_groups(role, query);

    let search_bar = div()
        .flex_none().flex().items_center().gap(px(8.)).px(px(8.)).pb(px(4.)).mb(px(4.))
        .border_b_1().border_color(pal.sep)
        .child(icon(Icon::FindSearch, px(14.), theme.mono_icon_colors(theme.text_secondary)))
        .child(div().flex_1().min_w_0().child(Input::new(search_input).appearance(false).bordered(false).focus_bordered(false).h(px(28.)).text_size(px(14.))));

    let error_banner = font_picker_error().map(|msg| {
        div().flex_none().mx(px(4.)).mb(px(4.)).px(px(8.)).py(px(4.))
            .rounded(crate::ui::radius::CONTROL).bg(pal.danger_fill)
            .text_size(px(14.)).line_height(px(20.)).text_color(pal.danger_fg).child(msg)
    });

    let mut list = div().id("font-picker-list").flex_1().min_h_0().overflow_y_scroll().flex().flex_col().gap(px(2.));
    let has_any = groups.iter().any(|g| !g.items.is_empty());
    if !has_any {
        list = list.child(div().flex_none().px(px(10.)).py(px(16.)).text_color(pal.muted).child(tr("没有匹配的字体", "No matching fonts")));
    } else {
        for group in groups {
            list = list.child(group_label(group.title));
            for item in group.items {
                let id = item.id;
                let active = item.selected;
                let check_colors = IconColors { line: theme.accent, fill: gpui::transparent_black(), accent: theme.accent };
                let row = div()
                    .flex_none().w_full().h(px(34.)).px(px(10.)).rounded(crate::ui::radius::CONTROL)
                    .flex().items_center().justify_between().cursor(CursorStyle::PointingHand)
                    .hover(move |s| s.bg(pal.hover_bg))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
                        cx.stop_propagation();
                        match role {
                            FontPickerRole::Body => this.select_editor_font(id.clone(), window, cx),
                            FontPickerRole::Code => this.select_code_font(id.clone(), window, cx),
                        }
                    }))
                    .child(
                        div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis()
                            .text_size(px(15.)).font_family(item.family).text_color(pal.text).child(item.name),
                    )
                    .when(active, |r| r.child(div().ml(px(8.)).flex_none().child(icon(Icon::Check, px(16.), check_colors))));
                list = list.child(row);
            }
        }
    }

    let import_btn = div()
        .flex_none().w_full().h(px(34.)).px(px(10.)).rounded(crate::ui::radius::CONTROL)
        .flex().items_center().gap(px(8.)).cursor(CursorStyle::PointingHand)
        .text_color(theme.accent)
        .hover(move |s| s.bg(pal.hover_bg))
        .on_mouse_down(MouseButton::Left, cx.listener(move |this, _, window, cx| {
            cx.stop_propagation();
            this.import_font_dialog(role, window, cx);
        }))
        .child(icon(Icon::Plus, px(16.), IconColors { line: theme.accent, fill: gpui::transparent_black(), accent: theme.accent }))
        .child(tr("导入字体文件…", "Import Font File…"));

    let bottom_bar = div().flex_none().pt(px(4.)).mt(px(4.)).border_t_1().border_color(pal.sep).child(import_btn);

    let menu = div()
        .id(("font-picker-menu", role as usize))
        .w(px(FONT_MENU_WIDTH)).max_h(px(max_h)).p(px(6.))
        .rounded(crate::ui::radius::SURFACE).bg(pal.bg).shadow(dropdown::shadows())
        .occlude().flex().flex_col()
        .on_mouse_down(MouseButton::Left, |_, _, cx| cx.stop_propagation())
        .on_mouse_down(MouseButton::Right, |_, _, cx| cx.stop_propagation())
        .on_scroll(|_, _, cx| cx.stop_propagation())
        .child(search_bar).children(error_banner).child(list).child(bottom_bar);

    let backdrop = deferred(
        anchored().position(point(px(0.), px(0.))).child(
            div().id("font-picker-backdrop").w(px(10000.)).h(px(10000.)).bg(gpui::transparent_black()).occlude()
                .on_mouse_down(MouseButton::Left, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.close_font_picker(cx); }))
                .on_mouse_down(MouseButton::Right, cx.listener(|this, _, _, cx| { cx.stop_propagation(); this.close_font_picker(cx); })),
        )
    ).with_priority(2);

    div().child(backdrop).child(dropdown::deferred_popover(anchor, menu, 3))
}
