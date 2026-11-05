//! Composer reference selection and request preflight.
use gpui::{App, Context, Window};
use super::{chat::{AiChat, Msg, Pick, follow_placeholder}, provider};
use crate::theme::tr;

impl AiChat {
    pub(super) fn picker_token(&self, cx: &App) -> Option<String> {
        if self.picker_dismissed { return None; }
        let input = self.input.read(cx);
        let value = input.value();
        if !input.selected_range().is_empty() || input.cursor() != value.len() { return None; }
        let token = value.rsplit(char::is_whitespace).next().unwrap_or("");
        (token.starts_with('@') || token.starts_with('/')).then(|| token.to_owned())
    }

    pub(super) fn picker_items(&self, token: &str, cx: &App) -> Vec<(Pick, String, String)> {
        let value = self.input.read(cx).value();
        if let Some(q) = token.strip_prefix('@') {
            let q = q.to_lowercase();
            self.agent_ctx(cx).pages().into_iter().filter(|p| p.to_lowercase().contains(&q) && !self.refs.contains(p))
                .take(8).map(|p| (Pick::Page(p.clone()), p, tr("笔记", "Note").into())).collect()
        } else if let Some(q) = token.strip_prefix('/').filter(|_| value.trim_start().starts_with('/') || value.contains(" /")) {
            let q = q.to_lowercase();
            let new_cmd = tr("/新会话", "/new");
            let mut v: Vec<(Pick, String, String)> = vec![(Pick::NewConv, new_cmd.into(), tr("开启空白对话，当前会话会留在历史里", "Start a new chat; current chat stays in history").into())];
            v.extend(self.agent_ctx(cx).local_skills().into_iter().map(|(n, d, _)| (Pick::Skill(n.clone()), format!("/{n}"), d)));
            v.into_iter().filter(|(_, n, d)| q.is_empty() || n.to_lowercase().contains(&q) || d.to_lowercase().contains(&q) || (q == "new" && n == new_cmd)).collect()
        } else { Vec::new() }
    }

    pub(super) fn move_pick(&mut self, direction: isize, cx: &mut Context<Self>) -> bool {
        let Some(token) = self.picker_token(cx) else { return false };
        let count = self.picker_items(&token, cx).len();
        if count > 0 {
            self.pick_index = (self.pick_index.min(count - 1) as isize + direction).rem_euclid(count as isize) as usize;
            cx.notify();
        }
        true
    }

    pub(super) fn accept_pick(&mut self, pick: Pick, window: &mut Window, cx: &mut Context<Self>) {
        let Some(token) = self.picker_token(cx) else { return };
        let value = self.input.read(cx).value().to_string();
        let rest = value[..value.len() - token.len()].to_owned();
        match pick {
            Pick::Page(page) => { if !self.refs.contains(&page) { self.refs.push(page); } }
            Pick::Skill(skill) => { if !self.skills.contains(&skill) { self.skills.push(skill); } }
            Pick::NewConv => { self.new_conv(window, cx); return; }
        }
        self.pick_index = 0;
        self.input.update(cx, |i, cx| { i.set_value(rest, window, cx); i.focus(window, cx); });
        cx.notify();
    }

    pub(super) fn send(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        if self.streaming.is_some() { return; }
        if let Some(token) = self.picker_token(cx) {
            let picks = self.picker_items(&token, cx);
            if let Some((pick, _, _)) = picks.get(self.pick_index.min(picks.len().saturating_sub(1))) {
                self.accept_pick(pick.clone(), window, cx);
            } else {
                cx.defer_in(window, |this, window, cx| this.input.update(cx, |i, cx| i.focus(window, cx)));
            }
            return;
        }
        let question = self.input.read(cx).value().trim().to_owned();
        if question.is_empty() || matches!(question.as_str(), "@" | "/") { return; }
        if let Err((message, settings)) = provider::validate_request(&provider::config()) {
            self.error = Some(message); self.error_settings = settings; cx.notify(); return;
        }
        let ctx = self.agent_ctx(cx);
        let (mut context, mut shown) = (String::new(), String::new());
        for r in self.refs.drain(..) {
            shown.push_str(&format!("@{r} "));
            let md = ctx.read(&r).unwrap_or_default();
            context.push_str(&format!("\n\n# 引用笔记：{r}（pageId: {r}）\n\n{}", md.chars().take(12000).collect::<String>()));
        }
        let local = ctx.local_skills();
        for name in self.skills.drain(..) {
            shown.push_str(&format!("/{name} "));
            if let Some((_, _, body)) = local.iter().find(|s| s.0 == name) { context.push_str(&format!("\n\n# 用户调用的 Skill：{name}\n\n{body}")); }
        }
        let content = if shown.is_empty() { question } else { format!("{shown}{question}") };
        self.push_msg(Msg { role: "user".into(), content, context: (!context.is_empty()).then_some(context), ..Default::default() });
        self.error = None; self.error_settings = false;
        self.input.update(cx, |i, cx| { i.set_value("", window, cx); i.set_placeholder(follow_placeholder(), window, cx); });
        self.run_turn(0, cx);
    }

}
