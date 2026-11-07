use std::{cell::RefCell, collections::HashMap, rc::Rc, time::Duration};
use gpui::{Animation, AnimationExt, AnyElement, App, CursorStyle, ElementId, Entity, FontWeight,
    IntoElement, MouseButton, SharedString, Subscription, Window, div, prelude::*, px};
use gpui_component::input::{Input, InputEvent, InputState};
use serde::{Deserialize, Serialize};
use crate::{editor::ai::backend::ApprovalResponse, icons::{Icon, icon}, theme::tr, ui::{ai_theme, motion, smoke_overlay}};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum QuestionType { Radio, Check }

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct ApprovalQuestion {
    pub q: String,
    #[serde(default = "default_radio")]
    pub kind: QuestionType,
    pub options: Vec<String>,
}
fn default_radio() -> QuestionType { QuestionType::Radio }

pub fn parse_questions(args: &serde_json::Value) -> Vec<ApprovalQuestion> {
    let list = args.get("questions").and_then(|v| v.as_array()).cloned().unwrap_or_else(|| {
        if args.get("q").is_some() || args.get("question").is_some() { vec![args.clone()] } else { Vec::new() }
    });
    list.into_iter().filter_map(|item| {
        let q = item.get("q").or_else(|| item.get("question")).and_then(|v| v.as_str())?.trim().to_owned();
        if q.is_empty() { return None; }
        let multi = item.get("multiSelect").or_else(|| item.get("is_multi_select")).and_then(|v| v.as_bool()).unwrap_or(false)
            || matches!(item.get("type").or_else(|| item.get("kind")).and_then(|v| v.as_str()), Some("check" | "checkbox" | "multi"));
        let options = item.get("options").and_then(|v| v.as_array()).into_iter().flatten().filter_map(|opt| {
            opt.as_str().or_else(|| opt.get("label").or_else(|| opt.get("value")).and_then(|v| v.as_str())).map(str::trim).filter(|s| !s.is_empty()).map(str::to_owned)
        }).collect();
        Some(ApprovalQuestion { q, kind: if multi { QuestionType::Check } else { QuestionType::Radio }, options })
    }).collect()
}

pub fn extract_markdown_questions(text: &str) -> Vec<ApprovalQuestion> {
    let mut out = Vec::new();
    for line in text.lines() {
        let trimmed = line.trim();
        let rest = trimmed.strip_prefix(|c: char| c.is_ascii_digit()).and_then(|s| s.trim_start_matches(|c: char| c.is_ascii_digit()).strip_prefix('.').or_else(|| s.strip_prefix('、')))
            .or_else(|| trimmed.strip_prefix("- ").or_else(|| trimmed.strip_prefix("* ")));
        let Some(body) = rest.map(str::trim).filter(|s| s.contains('？') || s.contains('?')) else { continue };
        let end = body.find('？').map(|i| i + '？'.len_utf8()).or_else(|| body.find('?').map(|i| i + 1)).unwrap_or(body.len());
        let q = body[..end].replace("**", "").replace('`', "").trim().to_owned();
        if q.chars().count() < 4 || q.chars().count() > 90 { continue; }
        let options = if q.contains("是否") || q.to_lowercase().starts_with("should ") || q.to_lowercase().starts_with("do you ") {
            vec![tr("是，确认", "Yes").into(), tr("否，不需要", "No").into()]
        } else {
            vec![tr("确认", "Confirm").into(), tr("跳过此项", "Skip this").into()]
        };
        out.push(ApprovalQuestion { q, kind: QuestionType::Radio, options });
        if out.len() >= 4 { break; }
    }
    out
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Choice { Allow, Deny }

#[derive(Default)]
pub(crate) struct ApprovalState {
    choice: Option<Choice>,
    submitted: bool,
    pub qi: usize,
    pub prev_qi: usize,
    pub nav_seq: usize,
    pub answers: HashMap<usize, Vec<usize>>,
    pub custom_inputs: HashMap<usize, (Entity<InputState>, Subscription)>,
    pub dismissed: bool,
    pub advance_seq: usize,
}

fn step_counter(state: &ApprovalState, total: usize) -> AnyElement {
    let cur = format!("{} / {total}", state.qi + 1);
    let prev = format!("{} / {total}", state.prev_qi + 1);
    if state.nav_seq == 0 || cur == prev {
        return div().child(cur).into_any_element();
    }
    let down = state.qi < state.prev_qi;
    let seq = state.nav_seq;
    let old_chars: Vec<char> = prev.chars().collect();
    let new_chars: Vec<char> = cur.chars().collect();
    let len = old_chars.len().max(new_chars.len());
    let mut row = div().flex().items_center();
    for i in 0..len {
        let o = old_chars.get(i).copied().unwrap_or(' ');
        let n = new_chars.get(i).copied().unwrap_or(' ');
        if o == n {
            row = row.child(n.to_string());
        } else {
            let (top, bottom) = if down { (n, o) } else { (o, n) };
            row = row.child(div().h(px(14.)).overflow_hidden().child(
                div().flex().flex_col().child(div().h(px(14.)).line_height(px(14.)).child(top.to_string()))
                    .child(div().h(px(14.)).line_height(px(14.)).child(bottom.to_string()))
                    .with_animation(SharedString::from(format!("ai-approval-roll-{seq}-{i}")),
                        Animation::new(Duration::from_millis(350)).with_easing(motion::ease_out_quart()),
                        move |el, d| el.relative().top(px(if down { -14. * (1. - d) } else { -14. * d })))));
        }
    }
    row.into_any_element()
}

fn option(id: &'static str, label: &'static str, choice: Choice, state: Rc<RefCell<ApprovalState>>) -> impl IntoElement {
    let theme = ai_theme::theme();
    let selected = state.borrow().choice == Some(choice);
    let selected_probe = match choice { Choice::Allow => "ai-approval-selected-allow", Choice::Deny => "ai-approval-selected-deny" };
    div().id(id).relative().children(smoke_overlay::probe(id)).when(selected, |r| r.children(smoke_overlay::probe(selected_probe)))
        .w_full().min_h(px(28.)).pl(px(4.)).pr(px(8.)).py(px(4.)).flex().items_center().gap(px(6.)).rounded(px(6.)).cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
        .child(div().size(px(16.)).flex_none().rounded_full().border_1().border_color(if selected { theme.text } else { theme.btn_border })
            .when(selected, |r| r.bg(theme.text)).flex().items_center().justify_center()
            .when(selected, |r| r.child(div().size(px(6.)).rounded_full().bg(theme.paper))))
        .child(div().text_size(px(13.)).text_color(if selected { theme.text } else { theme.text_secondary }).child(label))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let mut s = state.borrow_mut();
            if !s.submitted { s.choice = Some(choice); window.refresh(); }
        })
}

pub(crate) fn approval_card(state: Rc<RefCell<ApprovalState>>, title: String, detail: String,
    on_response: impl Fn(ApprovalResponse, &mut Window, &mut App) -> bool + 'static) -> impl IntoElement {
    let theme = ai_theme::theme();
    let enabled = state.borrow().choice.is_some() && !state.borrow().submitted;
    let response = Rc::new(on_response);
    let (skip_response, skip_state, continue_state) = (response.clone(), state.clone(), state.clone());
    let continue_button = div().id("ai-approval-continue").relative().children(smoke_overlay::probe("ai-approval-continue"))
        .when(!enabled, |b| b.children(smoke_overlay::probe("ai-approval-continue-disabled")))
        .h(px(27.)).px(px(12.)).rounded_full().flex().items_center().justify_center().text_size(px(13.)).font_weight(FontWeight(500.))
        .bg(theme.accent).text_color(gpui::rgb(0xffffff)).when(!enabled, |b| b.opacity(0.5))
        .when(enabled, |b| b.cursor(CursorStyle::PointingHand).hover(|s| s.opacity(0.85))).child(tr("继续", "Continue"))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let choice = { let mut s = continue_state.borrow_mut(); if s.submitted { None } else { if s.choice.is_some() { s.submitted = true; } s.choice } };
            if let Some(c) = choice && !response(match c { Choice::Allow => ApprovalResponse::AllowOnce, Choice::Deny => ApprovalResponse::Deny }, window, cx) {
                continue_state.borrow_mut().submitted = false; window.refresh();
            }
        });
    let skip = div().id("ai-approval-skip").relative().children(smoke_overlay::probe("ai-approval-skip"))
        .h(px(27.)).px(px(12.)).rounded_full().flex().items_center().justify_center().text_size(px(13.)).text_color(theme.text).bg(theme.selected)
        .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.btn_border)).child(tr("跳过", "Skip"))
        .on_mouse_down(MouseButton::Left, move |_, window, cx| {
            cx.stop_propagation();
            let submit = { let mut s = skip_state.borrow_mut(); if s.submitted { false } else { s.submitted = true; true } };
            if submit && !skip_response(ApprovalResponse::Deny, window, cx) { skip_state.borrow_mut().submitted = false; window.refresh(); }
        });
    let card = div().relative().children(smoke_overlay::probe("ai-approval-card")).w_full().max_w(px(320.)).rounded(px(12.)).border_1().border_color(theme.hairline).bg(theme.paper)
        .text_color(theme.text).flex().flex_col()
        .child(div().p(px(14.)).flex().flex_col().child(div().text_size(px(14.)).font_weight(FontWeight(500.)).child(title))
            .when(!detail.is_empty(), |b| b.child(div().mt(px(4.)).text_size(px(13.)).text_color(theme.text_secondary).child(detail)))
            .child(div().mt(px(10.)).flex().flex_col().gap(px(4.)).child(option("ai-approval-option-allow", tr("允许一次", "Allow once"), Choice::Allow, state.clone()))
                .child(option("ai-approval-option-deny", tr("拒绝", "Deny"), Choice::Deny, state))))
        .child(div().px(px(14.)).pb(px(12.)).pt(px(4.)).flex().items_center().justify_between()
            .child(div().flex().items_center().gap(px(4.)).text_size(px(12.)).text_color(theme.text_secondary)
                .child(div().opacity(0.3).child(icon(Icon::ChevronUp, px(14.), theme.mono_icon_colors(theme.text_secondary)))).child("1 / 1")
                .child(div().opacity(0.3).child(icon(Icon::ChevronDown, px(14.), theme.mono_icon_colors(theme.text_secondary)))))
            .child(div().flex().items_center().gap(px(6.)).child(skip).child(continue_button)));
    motion::fade_up(card, "ai-approval-enter")
}

pub(crate) fn question_card(state: Rc<RefCell<ApprovalState>>, questions: Vec<ApprovalQuestion>, window: &mut Window, cx: &mut App,
    on_submit: impl Fn(serde_json::Value, &mut Window, &mut App) -> bool + 'static) -> AnyElement {
    let total = questions.len().max(1);
    let submit_fn = Rc::new(on_submit);
    let qi = { let mut s = state.borrow_mut(); s.qi = s.qi.min(total - 1); s.qi };
    let input = {
        let mut s = state.borrow_mut();
        if let Some((entity, _)) = s.custom_inputs.get(&qi) { entity.clone() } else {
            let (st, qs_in, sub_in) = (state.clone(), questions.clone(), submit_fn.clone());
            let entity = cx.new(|cx| InputState::new(window, cx).placeholder(tr("其他补充…", "Something else…")));
            let kind = questions.get(qi).map(|q| q.kind).unwrap_or(QuestionType::Radio);
            let sub = window.subscribe(&entity, cx, move |input_state, ev: &InputEvent, window, cx| match ev {
                InputEvent::Change => {
                    let val = input_state.read(cx).value().to_string();
                    let mut s = st.borrow_mut();
                    s.advance_seq += 1;
                    if kind == QuestionType::Radio && !val.trim().is_empty() { s.answers.insert(qi, Vec::new()); }
                    window.refresh();
                }
                InputEvent::PressEnter { secondary: false, .. } => {
                    let val = input_state.read(cx).value().trim().to_string();
                    let has = !val.is_empty() || st.borrow().answers.get(&qi).is_some_and(|a| !a.is_empty());
                    if has { advance_or_submit(&st, &qs_in, &sub_in, window, cx); }
                }
                _ => {}
            });
            s.custom_inputs.insert(qi, (entity.clone(), sub));
            entity
        }
    };
    let theme = ai_theme::theme();
    if state.borrow().dismissed {
        let st = state.clone();
        return div().id("ai-question-reopen").relative().children(smoke_overlay::probe("ai-question-reopen"))
            .h(px(30.)).px(px(12.)).rounded(px(8.)).border_1().border_color(theme.btn_border).bg(theme.paper)
            .flex().items_center().gap(px(6.)).text_size(px(12.5)).font_weight(FontWeight(500.)).text_color(theme.text)
            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)).child(tr("展开待确认问题", "Open approval"))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); st.borrow_mut().dismissed = false; window.refresh(); }).into_any_element();
    }
    let q = questions.get(qi).cloned().unwrap_or(ApprovalQuestion { q: String::new(), kind: QuestionType::Radio, options: Vec::new() });
    let picked = state.borrow().answers.get(&qi).cloned().unwrap_or_default();
    let custom_text = input.read(cx).value().trim().to_string();
    let has_answer = !picked.is_empty() || !custom_text.is_empty();
    let last = qi + 1 >= total;
    let mut opts = div().mt(px(10.)).flex().flex_col().gap(px(4.));
    for (idx, opt) in q.options.iter().enumerate() {
        let on = picked.contains(&idx);
        let (st, qs, sub_fn, is_radio) = (state.clone(), questions.clone(), submit_fn.clone(), q.kind == QuestionType::Radio);
        let inp = input.clone();
        let row_id = ElementId::from(SharedString::from(format!("ai-question-opt-{qi}-{idx}")));
        opts = opts.child(div().id(row_id.clone()).relative().children(smoke_overlay::probe_for(&row_id))
            .w_full().min_h(px(28.)).pl(px(4.)).pr(px(8.)).py(px(4.)).flex().items_center().gap(px(8.)).rounded(px(6.))
            .cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
            .child(div().size(px(16.)).flex_none().rounded(if is_radio { px(99.) } else { px(5.) }).border_1()
                .border_color(if on { theme.text } else { theme.btn_border }).when(on, |b| b.bg(theme.text)).flex().items_center().justify_center()
                .when(on, |b| if is_radio { b.child(div().size(px(6.)).rounded_full().bg(theme.paper)) }
                    else { b.child(icon(Icon::Check, px(11.), theme.mono_icon_colors(theme.paper))) }))
            .child(div().flex_1().min_w_0().text_size(px(13.)).line_height(px(18.)).text_color(if on { theme.text } else { theme.text_secondary }).child(opt.clone()))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                cx.stop_propagation();
                let seq = {
                    let mut s = st.borrow_mut();
                    if s.submitted { return; }
                    s.advance_seq += 1;
                    let list = s.answers.entry(qi).or_default();
                    if is_radio { *list = vec![idx]; } else if let Some(p) = list.iter().position(|&x| x == idx) { list.remove(p); } else { list.push(idx); }
                    s.advance_seq
                };
                if is_radio {
                    inp.update(cx, |i, cx| i.set_value("", window, cx));
                    let (st2, qs2, sub2) = (st.clone(), qs.clone(), sub_fn.clone());
                    window.spawn(cx, async move |cx| {
                        cx.background_executor().timer(Duration::from_millis(380)).await;
                        let _ = cx.update(|window, cx| {
                            if st2.borrow().advance_seq == seq && !st2.borrow().submitted && st2.borrow().qi == qi {
                                advance_or_submit(&st2, &qs2, &sub2, window, cx);
                            }
                        });
                    }).detach();
                }
                window.refresh();
            }));
    }
    opts = opts.child(div().id("ai-question-custom").relative().children(smoke_overlay::probe("ai-question-custom"))
        .w_full().min_h(px(28.)).pl(px(4.)).pr(px(8.)).py(px(2.)).flex().items_center().rounded(px(6.)).hover(move |s| s.bg(theme.hover))
        .child(Input::new(&input).appearance(false).bordered(false).focus_bordered(false).w_full().h(px(24.)).px(px(2.)).text_size(px(13.))));
    let (st_dismiss, st_prev, st_next, st_skip, st_cont) = (state.clone(), state.clone(), state.clone(), state.clone(), state.clone());
    let (qs_skip, sub_skip, qs_cont, sub_cont) = (questions.clone(), submit_fn.clone(), questions.clone(), submit_fn.clone());
    let seq = state.borrow().nav_seq;
    let slide_up = state.borrow().qi >= state.borrow().prev_qi;
    let body = div().p(px(14.)).flex().flex_col().overflow_hidden().child(
        div().flex().flex_col().child(div().pr(px(24.)).text_size(px(14.)).line_height(px(20.)).font_weight(FontWeight(500.)).child(q.q)).child(opts)
            .with_animation(SharedString::from(format!("ai-question-slide-{seq}")),
                Animation::new(Duration::from_millis(320)).with_easing(motion::ease_out_quart()),
                move |el, d| if seq == 0 { el } else { el.opacity(d).relative().top(px((if slide_up { 12. } else { -12. }) * (1. - d))) }));
    let footer = div().px(px(14.)).pb(px(12.)).pt(px(4.)).flex().items_center().justify_between().gap(px(12.))
        .child(div().flex().items_center().gap(px(4.)).text_size(px(12.)).font_weight(FontWeight(500.)).text_color(theme.text_secondary)
            .child(div().id("ai-question-prev").relative().children(smoke_overlay::probe("ai-question-prev")).size(px(18.)).rounded(px(5.)).flex().items_center().justify_center()
                .when(qi == 0, |b| b.opacity(0.3)).when(qi > 0, |b| b.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)))
                .child(icon(Icon::ChevronUp, px(14.), theme.mono_icon_colors(theme.text_secondary)))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); if qi > 0 { step_to(&st_prev, qi - 1); window.refresh(); } }))
            .child(step_counter(&state.borrow(), total))
            .child(div().id("ai-question-next").relative().children(smoke_overlay::probe("ai-question-next")).size(px(18.)).rounded(px(5.)).flex().items_center().justify_center()
                .when(last, |b| b.opacity(0.3)).when(!last, |b| b.cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover)))
                .child(icon(Icon::ChevronDown, px(14.), theme.mono_icon_colors(theme.text_secondary)))
                .on_mouse_down(MouseButton::Left, move |_, window, cx| { cx.stop_propagation(); if !last { step_to(&st_next, qi + 1); window.refresh(); } })))
        .child(div().flex().items_center().gap(px(6.))
            .child(div().id("ai-question-skip").relative().children(smoke_overlay::probe("ai-question-skip")).h(px(27.)).px(px(12.)).rounded_full().flex().items_center().justify_center()
                .text_size(px(13.)).font_weight(FontWeight(500.)).text_color(theme.text).bg(theme.selected).cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.btn_border))
                .child(tr("跳过", "Skip")).on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    cx.stop_propagation();
                    st_skip.borrow_mut().answers.remove(&qi);
                    if !last { step_to(&st_skip, qi + 1); window.refresh(); } else { finish_submit(&st_skip, &qs_skip, &sub_skip, true, window, cx); }
                }))
            .child(div().id("ai-question-continue").relative().children(smoke_overlay::probe("ai-question-continue")).h(px(27.)).px(px(12.)).rounded_full().flex().items_center().justify_center()
                .text_size(px(13.)).font_weight(FontWeight(500.)).bg(theme.accent).text_color(gpui::rgb(0xffffff)).when(!has_answer, |b| b.opacity(0.5))
                .when(has_answer, |b| b.cursor(CursorStyle::PointingHand).hover(|s| s.opacity(0.85)))
                .child(if last { tr("发送", "Send") } else { tr("继续", "Continue") })
                .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                    cx.stop_propagation();
                    if has_answer { advance_or_submit(&st_cont, &qs_cont, &sub_cont, window, cx); }
                })));
    let card = div().relative().children(smoke_overlay::probe("ai-question-card")).w_full().rounded(px(12.)).border_1().border_color(theme.btn_border).bg(theme.paper)
        .shadow(crate::ui::dropdown::shadows()).text_color(theme.text).flex().flex_col()
        .child(div().id("ai-question-dismiss").relative().children(smoke_overlay::probe("ai-question-dismiss")).absolute().right(px(10.)).top(px(10.)).size(px(22.)).rounded(px(6.))
            .flex().items_center().justify_center().cursor(CursorStyle::PointingHand).hover(move |s| s.bg(theme.hover))
            .child(icon(Icon::Close, px(13.), theme.mono_icon_colors(theme.text_secondary)))
            .on_mouse_down(MouseButton::Left, move |_, window, cx| {
                cx.stop_propagation();
                finish_submit(&st_dismiss, &questions, &submit_fn, true, window, cx);
            }))
        .child(body).child(footer);
    motion::fade_up(card, "ai-question-enter").into_any_element()
}

fn step_to(state: &Rc<RefCell<ApprovalState>>, next: usize) {
    let mut s = state.borrow_mut();
    if s.qi != next { s.prev_qi = s.qi; s.qi = next; s.nav_seq += 1; s.advance_seq += 1; }
}

fn advance_or_submit(state: &Rc<RefCell<ApprovalState>>, questions: &[ApprovalQuestion],
    on_submit: &Rc<impl Fn(serde_json::Value, &mut Window, &mut App) -> bool + 'static>, window: &mut Window, cx: &mut App) {
    let qi = state.borrow().qi;
    if qi + 1 < questions.len() { step_to(state, qi + 1); window.refresh(); }
    else { finish_submit(state, questions, on_submit, false, window, cx); }
}

fn finish_submit(state: &Rc<RefCell<ApprovalState>>, questions: &[ApprovalQuestion],
    on_submit: &Rc<impl Fn(serde_json::Value, &mut Window, &mut App) -> bool + 'static>, skipped: bool, window: &mut Window, cx: &mut App) {
    let payload = {
        let mut s = state.borrow_mut();
        if s.submitted { return; }
        s.submitted = true;
        s.advance_seq += 1;
        let mut list = Vec::new();
        let mut map = serde_json::Map::new();
        for (i, q) in questions.iter().enumerate() {
            let mut chosen: Vec<String> = s.answers.get(&i).into_iter().flatten()
                .filter_map(|&idx| q.options.get(idx).cloned()).collect();
            if let Some((inp, _)) = s.custom_inputs.get(&i) {
                let custom = inp.read(cx).value().trim().to_string();
                if !custom.is_empty() { chosen.push(custom); }
            }
            let joined = chosen.join(", ");
            if !joined.is_empty() { map.insert(q.q.clone(), serde_json::Value::String(joined.clone())); }
            list.push(serde_json::json!({ "question": q.q, "selected": chosen, "answer": joined }));
        }
        serde_json::json!({ "status": if skipped && map.is_empty() { "skipped" } else { "answered" }, "answers": map, "items": list })
    };
    if !on_submit(payload, window, cx) { state.borrow_mut().submitted = false; window.refresh(); }
}

