//! Geometry assertions on menus opened by the sandbox's actual GPUI mouse events.
use gpui::{Bounds, Pixels, px};
use super::{AsyncApp, Run, smoke_overlay};

fn bounds(id: &str) -> Result<Bounds<Pixels>, String> {
    smoke_overlay::target_bounds(id).ok_or_else(|| format!("菜单定位缺少实际布局：{id}"))
}

fn aligned_left(button: Bounds<Pixels>, popup: Bounds<Pixels>, viewport: Bounds<Pixels>) -> Pixels {
    let min = viewport.left() + px(8.);
    let max = viewport.right() - px(8.);
    if button.left() >= min && button.left() + popup.size.width <= max { button.left() }
    else if button.right() - popup.size.width >= min && button.right() <= max { button.right() - popup.size.width }
    else { button.left().min(max - popup.size.width).max(min) }
}

impl Run {
    pub(super) async fn menu_positions(&self, cx: &AsyncApp) -> Result<(), String> {
        use crate::theme::tr;
        use crate::ui::placement::{AlignX, PlacementSpec, resolve_origin};
        use gpui::{point, size};
        let viewport = Bounds::new(point(px(100.), px(40.)), size(px(600.), px(500.)));
        for (x, width, align, expected) in [
            (620., 206., AlignX::Left, 446.),
            (120., 206., AlignX::Right, 120.),
            (380., 500., AlignX::Left, 192.),
            (200., 206., AlignX::Left, 200.),
        ] {
            let trigger = Bounds::new(point(px(x), px(500.)), size(px(32.), px(28.)));
            let origin = resolve_origin(&PlacementSpec::below(trigger).viewport(viewport).align_x(align), size(px(width), px(126.)), size(px(800.), px(600.)));
            self.check(6, origin == point(px(expected), px(370.)), "菜单左右翻转、双侧不足贴边及底部向上展开")?;
        }
        for (trigger, menu) in [
            (tr("文字类型", "Text type"), "toolbar-kind-menu"),
            (tr("列表", "Lists"), "toolbar-kind-menu"),
            (tr("文本与高亮颜色", "Text and highlight color"), "palette-panel"),
        ] {
            self.click_target(cx, trigger, "菜单定位 · 打开选区工具栏下拉菜单").await?;
            self.redraw(cx);
            self.check(6, self.editor(cx, |e| e.menu_format_open()), "选区菜单已实际打开")?;
            let button = bounds(trigger)?;
            let popup = bounds(menu)?;
            let viewport = bounds("editor")?;
            let expected_left = aligned_left(button, popup, viewport);
            self.check(6, (popup.left() - expected_left).abs() <= px(2.), "选区菜单空间不足时切换按钮边缘对齐")?;
            self.check(6, popup.top() >= button.bottom() + px(3.) || popup.bottom() <= button.top() - px(3.), "选区菜单不遮挡触发按钮")?;
            self.click_target(cx, trigger, "菜单定位 · 收起选区菜单").await?;
        }
        let link = tr("链接 ⌘⇧K", "Link ⌘⇧K");
        let button = bounds(link)?;
        self.click_target(cx, link, "菜单定位 · 打开链接输入框").await?;
        self.redraw(cx);
        let popup = bounds("format-link-card")?;
        self.check(6, popup.top() >= button.bottom() + px(3.) || popup.bottom() <= button.top() - px(3.), "链接输入框避开工具栏按钮")?;
        self.press(cx, "escape").await?;
        self.click_target(cx, "titlebar-more", "菜单定位 · 打开标题栏更多菜单").await?;
        self.redraw(cx);
        let button = bounds("titlebar-more")?;
        let popup = bounds("page-more-menu")?;
        self.check(6, (popup.right() - button.right()).abs() <= px(2.) && (popup.top() - button.bottom() - px(4.)).abs() <= px(2.), "标题栏菜单按按钮实际右下角定位")?;
        self.click_target(cx, "titlebar-more", "菜单定位 · 关闭标题栏菜单").await?;
        self.click_target(cx, "notebook-switcher", "菜单定位 · 打开笔记本切换菜单").await?;
        self.redraw(cx);
        let button = bounds("notebook-switcher")?;
        let popup = bounds("workspace-menu")?;
        self.check(6, (popup.left() - button.left()).abs() <= px(2.) && (button.top() - popup.bottom() - px(4.)).abs() <= px(2.), "笔记本菜单按真实高度向上展开")?;
        self.click_target(cx, "notebook-switcher", "菜单定位 · 关闭笔记本菜单").await?;
        for right in [false, true] {
            self.win(cx, |window, cx| self.workspace.update(cx, |w, cx| w.prepare_menu_position_smoke(right, window, cx)))?;
            self.redraw(cx);
            self.click_target(cx, "tree-move-row", "菜单定位 · 打开靠近窗口边缘的移动子菜单").await?;
            self.redraw(cx);
            let row = bounds("tree-move-row")?;
            let popup = bounds("move-list")?;
            self.check(6, (popup.left() - row.right() - px(8.)).abs() <= px(2.) || (row.left() - popup.right() - px(8.)).abs() <= px(2.), "移动子菜单跟随已避让的父菜单，空间不足时向左展开")?;
            let size = self.win(cx, |w, _| w.viewport_size())?;
            self.check(6, (popup.top() - row.top().min(size.height - px(8.) - popup.size.height).max(px(8.))).abs() <= px(2.) && popup.bottom() <= size.height - px(7.), "级联菜单沿触发行对齐并保持在窗口内")?;
            self.click_target(cx, "titlebar-more", "菜单定位 · 点击外侧关闭移动菜单").await?;
        }
        Ok(())
    }
}

impl Run {
    pub(super) async fn image_menu_positions(&self, cx: &AsyncApp) -> Result<(), String> {
        let editor = self.query(cx, |w, _| w.editor.clone());
        let id = self.win(cx, |w, cx| editor.update(cx, |e, cx| e.prepare_menu_image_smoke(w, cx)))?;
        super::sleep(cx, 350).await;
        for (trigger, menu) in [(crate::theme::tr("更多图片操作", "More image options"), "img-more-menu"), (crate::theme::tr("裁剪图片", "Crop image"), "image-crop-popover")] {
            self.redraw(cx);
            let before = bounds(trigger)?;
            self.click_target(cx, trigger, "菜单定位 · 打开图片工具栏子菜单").await?;
            self.redraw(cx);
            let button = bounds(trigger)?;
            self.check(13, self.editor(cx, |e| e.menu_image_open()) && button == before, "图片子菜单已打开且工具栏位置保持不变")?;
            let name = if menu == "img-more-menu" { gpui::ElementId::from((menu, id as usize)).to_string() } else { menu.to_owned() };
            let popup = bounds(&name)?;
            let viewport = bounds("editor")?;
            let expected_left = aligned_left(button, popup, viewport);
            self.check(13, (popup.left() - expected_left).abs() <= px(2.) && (popup.top() >= button.bottom() + px(3.) || popup.bottom() <= button.top() - px(3.)), "图片菜单对齐按钮且不挤动工具栏")?;
            self.click_target(cx, trigger, "菜单定位 · 关闭图片子菜单").await?;
        }
        self.win(cx, |w, cx| editor.update(cx, |e, cx| e.prepare_menu_edge_smoke(w, cx)))?;
        self.press(cx, "cmd-down").await?;
        self.press(cx, "shift-left").await?;
        self.press(cx, "shift-left").await?;
        self.click_target(cx, crate::theme::tr("列表", "Lists"), "菜单定位 · 右侧空间不足时打开列表菜单").await?;
        self.redraw(cx);
        let button = bounds(crate::theme::tr("列表", "Lists"))?;
        let popup = bounds("toolbar-kind-menu")?;
        let viewport = bounds("editor")?;
        self.check(13, self.editor(cx, |e| e.menu_format_open()) && button.left() + popup.size.width > viewport.right() - px(8.), "真实选区复现右侧空间不足")?;
        self.check(13, (popup.right() - button.right()).abs() <= px(2.) && popup.left() >= viewport.left() + px(7.), "空间不足的列表菜单改为与按钮右边缘对齐")?;
        self.click_target(cx, crate::theme::tr("列表", "Lists"), "菜单定位 · 关闭边缘列表菜单").await?;
        Ok(())
    }
}
