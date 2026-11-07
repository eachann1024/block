use std::{cell::RefCell, rc::Rc};
use gpui::{AnyWindowHandle, AppContext, Bounds, Context, IntoElement, Render, Window, WindowBounds, WindowOptions, div, point, prelude::*, px, size};
use super::{AsyncApp, Run, sleep, smoke_overlay};
use crate::editor::ai::backend::ApprovalResponse;

struct ApprovalFixture {
    state: Rc<RefCell<crate::ui::ai_approval::ApprovalState>>,
    responses: Rc<RefCell<Vec<ApprovalResponse>>>,
    fail_once: Rc<std::cell::Cell<bool>>,
}

impl Render for ApprovalFixture {
    fn render(&mut self, _window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        let responses = self.responses.clone();
        let fail_once = self.fail_once.clone();
        div().size_full().p(px(20.)).child(crate::ui::ai_approval::approval_card(
            self.state.clone(), "Smoke permission".into(), "Read sandbox note".into(),
            move |response, _, _| {
                responses.borrow_mut().push(response);
                !fail_once.replace(false)
            },
        ))
    }
}

impl Run {
    #[cfg(feature = "smoke-capture")]
    pub(super) fn capture_loader_bounds(&self, cx: &AsyncApp) -> Result<(), String> {
        let bounds = smoke_overlay::target_bounds("ai-process-stage-badge").ok_or("Missing loader bounds")?;
        let scale = self.win(cx, |window, _| window.scale_factor())?;
        let data = serde_json::json!({
            "x": f32::from(bounds.left()) * scale,
            "y": f32::from(bounds.top()) * scale,
            "width": f32::from(bounds.size.width) * scale,
            "height": f32::from(bounds.size.height) * scale,
            "scale": scale,
        });
        std::fs::write("/tmp/block-beautifului-smoke/loader-bounds.json", data.to_string()).map_err(|error| error.to_string())
    }

    #[cfg(not(feature = "smoke-capture"))]
    pub(super) fn capture_loader_bounds(&self, _cx: &AsyncApp) -> Result<(), String> { Ok(()) }

    #[cfg(feature = "smoke-capture")]
    pub(super) fn capture_ai_theme(&self, cx: &AsyncApp, name: &str, mode: crate::files::state::ThemeMode) -> Result<(), String> {
        let previous = crate::theme::theme_mode();
        cx.update(|cx| {
            crate::theme::set_theme_mode(mode);
            crate::theme::sync_gpui_theme(cx);
        });
        self.redraw(cx);
        let result = self.capture_ai(cx, name);
        cx.update(|cx| {
            crate::theme::set_theme_mode(previous);
            crate::theme::sync_gpui_theme(cx);
        });
        self.redraw(cx);
        result
    }

    #[cfg(not(feature = "smoke-capture"))]
    pub(super) fn capture_ai_theme(&self, _cx: &AsyncApp, _name: &str, _mode: crate::files::state::ThemeMode) -> Result<(), String> { Ok(()) }

    pub(super) async fn beautiful_approval(&self, cx: &AsyncApp) -> Result<(), String> {
        let responses = Rc::new(RefCell::new(Vec::new()));
        let fixture_responses = responses.clone();
        let fail_once = Rc::new(std::cell::Cell::new(false));
        let fixture_fail_once = fail_once.clone();
        let handle = cx.update(|cx| cx.open_window(WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(Bounds::new(point(px(0.), px(0.)), size(px(420.), px(360.))))),
            show: false, focus: false, ..Default::default()
        }, |_, cx| cx.new(|_| ApprovalFixture { state: Rc::default(), responses: fixture_responses, fail_once: fixture_fail_once })))
            .map_err(|error| error.to_string())?;
        let run = Run { window: AnyWindowHandle::from(handle), workspace: self.workspace.clone() };
        let result = run.exercise_approval(cx, &responses, &fail_once).await;
        cx.update(|cx| run.window.update(cx, |_, window, _| window.remove_window())).map_err(|error| error.to_string())?;
        self.redraw(cx);
        result
    }

    async fn exercise_approval(&self, cx: &AsyncApp, responses: &Rc<RefCell<Vec<ApprovalResponse>>>, fail_once: &std::cell::Cell<bool>) -> Result<(), String> {
        for (index, option) in ["allow", "deny", "skip"].into_iter().enumerate() {
            if index > 0 {
                cx.update(|cx| self.window.update(cx, |view, _, cx| {
                    view.downcast::<ApprovalFixture>().unwrap().update(cx, |fixture, cx| { fixture.state = Rc::default(); cx.notify(); });
                })).map_err(|error| error.to_string())?;
            }
            self.redraw(cx);
            sleep(cx, 200).await;
            self.redraw(cx);
            self.check(14, smoke_overlay::target_bounds("ai-approval-continue-disabled").is_some()
                && smoke_overlay::target_bounds("ai-approval-selected-allow").is_none()
                && smoke_overlay::target_bounds("ai-approval-selected-deny").is_none(), "新审批没有继承选择")?;
            self.click_target(cx, "ai-approval-continue", "未选择时继续不应生效").await?;
            self.check(14, responses.borrow().len() == index, "未选择时不得发送审批结果")?;
            if option == "skip" {
                self.click_target(cx, "ai-approval-skip", "跳过审批").await?;
            } else {
                self.click_target(cx, &format!("ai-approval-option-{option}"), "选择审批选项").await?;
                self.redraw(cx);
                self.check(14, responses.borrow().len() == index
                    && smoke_overlay::target_bounds(&format!("ai-approval-selected-{option}")).is_some(), "选择后等待确认")?;
                if index == 0 { self.capture_ai(cx, "approval")?; }
                self.click_target(cx, "ai-approval-continue", "确认审批选项").await?;
            }
            let responses = responses.borrow();
            let valid = match (option, responses.last()) {
                ("allow", Some(ApprovalResponse::AllowOnce)) | ("deny" | "skip", Some(ApprovalResponse::Deny)) => true,
                _ => false,
            };
            self.check(14, responses.len() == index + 1 && valid, "审批返回对应决定且只返回一次")?;
        }
        cx.update(|cx| self.window.update(cx, |view, _, cx| {
            view.downcast::<ApprovalFixture>().unwrap().update(cx, |fixture, cx| { fixture.state = Rc::default(); cx.notify(); });
        })).map_err(|error| error.to_string())?;
        self.redraw(cx);
        self.click_target(cx, "ai-approval-option-allow", "选择后模拟回调失败").await?;
        fail_once.set(true);
        self.click_target(cx, "ai-approval-continue", "提交失败后允许重试").await?;
        self.redraw(cx);
        self.check(14, responses.borrow().len() == 4 && smoke_overlay::target_bounds("ai-approval-continue-disabled").is_none(), "失败后审批仍可重试")?;
        self.click_target(cx, "ai-approval-continue", "重试审批").await?;
        self.check(14, responses.borrow().len() == 5 && matches!(responses.borrow().last(), Some(ApprovalResponse::AllowOnce)), "重试返回允许一次")?;
        Ok(())
    }

    #[cfg(feature = "smoke-capture")]
    pub(super) fn capture_ai(&self, cx: &AsyncApp, name: &str) -> Result<(), String> {
        let directory = std::path::Path::new("/tmp/block-beautifului-smoke");
        std::fs::create_dir_all(directory).map_err(|error| format!("创建截图目录失败: {error}"))?;
        cx.update(|_| smoke_overlay::set_step(None, String::new()));
        self.win(cx, |window, cx| {
            window.refresh();
            window.draw(cx).clear(cx);
            window.render_to_image()
                .map_err(|error| format!("离屏渲染 {name} 失败: {error}"))?
                .save(directory.join(format!("{name}.png")))
                .map_err(|error| format!("保存截图 {name} 失败: {error}"))
        })?
    }

    #[cfg(not(feature = "smoke-capture"))]
    pub(super) fn capture_ai(&self, _cx: &AsyncApp, _name: &str) -> Result<(), String> { Ok(()) }
}
