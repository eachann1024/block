use serde_json::Value;
use super::super::{agent, chat::{AiChat, Msg}};
use crate::{
    theme::{is_en, tr},
    ui::ai_process_card::{DetailTone, ProcessDetailLine, ProcessDiffChip, ProcessStage, ProcessStep, ProcessStepIcon, ProcessStepMeta},
};

pub(super) fn now_ms() -> u64 {
    std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_millis() as u64
}

pub(in crate::editor::ai) fn record_activity(chat: &mut AiChat, name: String) {
    let name: String = name.chars().filter(|c| c.is_alphanumeric() || matches!(c, '_' | '-' | '.')).take(80).collect();
    if name.is_empty() { return; }
    if let Some(conv) = chat.current.and_then(|id| chat.convs.iter_mut().find(|c| c.id == id)) {
        if conv.messages.last().is_some_and(|m| m.role == "activity" && m.content == name) { return; }
        conv.messages.push(Msg { role: "activity".into(), content: name, ..Default::default() });
    }
}

pub(in crate::editor::ai) fn record_summary(chat: &mut AiChat, text: String) {
    if let Some(conv) = chat.current.and_then(|id| chat.convs.iter_mut().find(|c| c.id == id)) {
        if !conv.messages.last().is_some_and(|m| m.role == "public_summary") {
            conv.messages.push(Msg { role: "public_summary".into(), ..Default::default() });
        }
        if let Some(m) = conv.messages.last_mut() {
            m.content.extend(text.chars().take(4096usize.saturating_sub(m.content.chars().count())));
        }
    }
}

pub(in crate::editor::ai) fn record_outcome(chat: &mut AiChat, outcome: &str) {
    if let Some(m) = chat.current.and_then(|id| chat.convs.iter_mut().find(|c| c.id == id)).and_then(|c| c.messages.iter_mut().rev().find(|m| m.role == "user")) {
        if let Some((_, end)) = &mut m.timing { *end = Some(now_ms()); }
    }
    chat.push_msg(Msg { role: "turn_outcome".into(), content: outcome.into(), ..Default::default() });
}

pub(in crate::editor::ai) fn start_turn(chat: &mut AiChat) {
    if let Some(m) = chat.current.and_then(|id| chat.convs.iter_mut().find(|c| c.id == id)).and_then(|c| c.messages.iter_mut().rev().find(|m| m.role == "user")) {
        m.timing = Some((now_ms(), None));
        chat.persist();
    }
}

fn add_note(notes: &mut Vec<String>, value: Option<&str>) {
    if let Some(v) = value.filter(|v| !v.is_empty()) {
        if !notes.iter().any(|n| n == v) { notes.push(v.to_owned()); }
    }
}

pub(super) fn note_file(raw: &str) -> String {
    let t = raw.trim();
    if t.is_empty() { tr("当前笔记.md", "current.md").into() } else if t.contains('.') { t.to_owned() } else { format!("{t}.md") }
}

fn clean_line(raw: &str) -> String {
    raw.trim().trim_start_matches(['#', '*', '-', '>']).trim().chars().take(96).collect()
}

pub(super) fn summary_step(content: &str) -> (ProcessStep, ProcessStepMeta, String) {
    let lines: Vec<ProcessDetailLine> = content.lines().map(clean_line).filter(|l| !l.is_empty()).take(4)
        .map(|text| ProcessDetailLine { text, tone: DetailTone::Neutral }).collect();
    let chip = lines.first().map(|l| l.text.clone()).unwrap_or_else(|| tr("正在规划执行步骤…", "Planning next steps…").into());
    let step = ProcessStep { label: tr("思考中", "Thinking").into(), detail: chip, merged_count: 1, running: false };
    (step, ProcessStepMeta { icon: ProcessStepIcon::Think, mono: false, detail_mono: false, lines }, content.to_owned())
}

pub(super) fn activity_step(name: &str) -> (ProcessStep, ProcessStepMeta, String) {
    let text = format!("✓ {}", if is_en() { format!("Executed {name}") } else { format!("已执行 {name}") });
    let step = ProcessStep { label: tr("执行工具", "Run tool").into(), detail: name.to_owned(), merged_count: 1, running: false };
    let meta = ProcessStepMeta { icon: ProcessStepIcon::Run, mono: true, detail_mono: true, lines: vec![ProcessDetailLine { text: text.clone(), tone: DetailTone::Neutral }] };
    (step, meta, text)
}

pub(super) fn tool_step(call: &Value, result_opt: Option<&Value>, notes: &mut Vec<String>) -> (ProcessStage, ProcessStep, ProcessStepMeta, String) {
    let result = result_opt.unwrap_or(&Value::Null);
    let name = call.pointer("/function/name").and_then(Value::as_str).unwrap_or("");
    let args = call.pointer("/function/arguments")
        .and_then(|v| v.as_str().and_then(|s| serde_json::from_str::<Value>(s).ok()).or_else(|| v.as_object().map(|_| v.clone())))
        .unwrap_or_default();
    let items = match name {
        "listPages" => result.as_array().or_else(|| result.get("pages").and_then(Value::as_array)),
        "searchNotes" => result.get("results").and_then(Value::as_array),
        "listNotebooks" => result.get("notebooks").and_then(Value::as_array),
        _ => None,
    };
    if matches!(name, "listPages" | "searchNotes") {
        for item in items.into_iter().flatten() { add_note(notes, item.get("title").or_else(|| item.get("pageId")).and_then(Value::as_str)); }
    }
    let ops = args.get("ops").or_else(|| args.get("operations")).and_then(Value::as_array);
    if name == "executeBatchPlan" {
        for item in ops.into_iter().flatten() { add_note(notes, item.get("title").or_else(|| item.get("pageId")).and_then(Value::as_str)); }
    }
    let title = result.get("title").or_else(|| result.get("pageId")).or_else(|| args.get("pageId")).and_then(Value::as_str).unwrap_or("");
    if name == "readPage" { add_note(notes, Some(title)); }
    let failed = result.get("error").is_some();
    let count_str = if failed { tr("失败", "Failed").to_owned() } else if result_opt.is_none() { tr("状态未知", "Status unknown").to_owned() } else if let Some(items) = items {
        match name {
            "listNotebooks" => if is_en() { format!("{} workspaces", items.len()) } else { format!("{} 个工作区", items.len()) },
            "listPages" => if is_en() { format!("{} notes", items.len()) } else { format!("{} 篇笔记", items.len()) },
            _ => if is_en() { format!("{} matches", items.len()) } else { format!("{} 条命中", items.len()) },
        }
    } else { tr("完成", "Done").to_owned() };

    let (stage, icon, label, chip, mono, detail_mono, mut lines) = match name {
        "loadSkill" | "readSkillResource" => {
            let target = args.get("skill").or_else(|| args.get("name")).or_else(|| args.get("path")).or_else(|| args.get("skillId")).and_then(Value::as_str).unwrap_or("");
            let chip = if target.is_empty() { count_str.clone() } else { target.to_owned() };
            let l1 = if is_en() { format!("✓ Loaded {chip}") } else { format!("✓ 已加载 {chip}") };
            let l2 = if is_en() { "✓ Workspace tools unlocked".to_owned() } else { "✓ 工作区工具已就绪".to_owned() };
            (ProcessStage::Skill, ProcessStepIcon::Run, tr("加载工作区技能", "Load workspace skill").to_owned(), chip, true, true,
                vec![ProcessDetailLine { text: l1, tone: DetailTone::Neutral }, ProcessDetailLine { text: l2, tone: DetailTone::Neutral }])
        }
        "listNotebooks" => {
            let names: Vec<&str> = items.into_iter().flatten().filter_map(Value::as_str).take(4).collect();
            let l1 = if is_en() { format!("✓ {count_str} available") } else { format!("✓ 已挂载 {count_str}") };
            let mut lines = vec![ProcessDetailLine { text: l1, tone: DetailTone::Neutral }];
            if !names.is_empty() { lines.push(ProcessDetailLine { text: format!("✓ {}", names.join(" · ")), tone: DetailTone::Neutral }); }
            (ProcessStage::Scan, ProcessStepIcon::Run, tr("加载工作区目录", "Load workspace roots").to_owned(), count_str.clone(), true, true, lines)
        }
        "listPages" => {
            let titles: Vec<&str> = items.into_iter().flatten().filter_map(|i| i.get("title").or_else(|| i.get("pageId")).and_then(Value::as_str)).take(4).collect();
            let l1 = if is_en() { format!("✓ Scanned {count_str}") } else { format!("✓ 共扫描 {count_str}") };
            let mut lines = vec![ProcessDetailLine { text: l1, tone: DetailTone::Neutral }];
            if !titles.is_empty() { lines.push(ProcessDetailLine { text: titles.join(" · "), tone: DetailTone::Neutral }); }
            (ProcessStage::Scan, ProcessStepIcon::Read, tr("扫描笔记本目录", "Scan notebook pages").to_owned(), count_str.clone(), true, false, lines)
        }
        "searchNotes" => {
            let query = args.get("query").and_then(Value::as_str).unwrap_or("");
            let chip = if query.is_empty() { count_str.clone() } else { format!("{query} · {count_str}") };
            let l1 = if is_en() { format!("✓ {count_str}") } else { format!("✓ 检索完成 · {count_str}") };
            let mut lines = vec![ProcessDetailLine { text: l1, tone: DetailTone::Neutral }];
            for hit in items.into_iter().flatten().take(2) {
                let t = hit.get("title").and_then(Value::as_str).unwrap_or("");
                let s = hit.get("snippet").and_then(Value::as_str).unwrap_or("");
                if !t.is_empty() { lines.push(ProcessDetailLine { text: if s.is_empty() { t.to_owned() } else { format!("{t} · {}", clean_line(s)) }, tone: DetailTone::Neutral }); }
            }
            (ProcessStage::Scan, ProcessStepIcon::Run, tr("搜索相关笔记", "Search notes").to_owned(), chip, true, false, lines)
        }
        "readPage" => {
            let file = note_file(title);
            let md = result.get("markdown").and_then(Value::as_str).unwrap_or("");
            let mut lines = Vec::new();
            if !md.is_empty() {
                let lc = md.lines().count().max(1);
                let cc = md.chars().count();
                lines.push(ProcessDetailLine {
                    text: if is_en() { format!("{lc} lines · {cc} chars") } else { format!("共 {lc} 行 · {cc} 字") },
                    tone: DetailTone::Neutral,
                });
                if let Some(excerpt) = md.lines().map(clean_line).find(|l| !l.is_empty()) {
                    lines.push(ProcessDetailLine { text: excerpt, tone: DetailTone::Neutral });
                }
            } else {
                lines.push(ProcessDetailLine {
                    text: if is_en() { format!("Read note {file}") } else { format!("已读取笔记 {file}") },
                    tone: DetailTone::Neutral,
                });
            }
            (ProcessStage::Read, ProcessStepIcon::Read, tr("读取笔记", "Read note").to_owned(), file, true, false, lines)
        }
        "askUserQuestion" => {
            let prompt = args.pointer("/questions/0/question").and_then(Value::as_str).unwrap_or_else(|| tr("等待确认…", "Awaiting confirmation…"));
            (ProcessStage::Synth, ProcessStepIcon::Think, tr("向用户提问确认", "Ask user for confirmation").to_owned(),
                clean_line(prompt), false, false, vec![ProcessDetailLine { text: clean_line(prompt), tone: DetailTone::Neutral }])
        }
        "executeBatchPlan" => {
            let first_page = ops.into_iter().flatten().find_map(|o| o.get("pageId").or_else(|| o.get("title")).and_then(Value::as_str)).unwrap_or("");
            let file = note_file(first_page);
            let mut added_lines = Vec::new();
            let mut line_count = 0usize;
            for op in ops.into_iter().flatten() {
                let body = op.get("newString").or_else(|| op.get("markdown")).and_then(Value::as_str).unwrap_or("");
                line_count += body.lines().filter(|l| !l.trim().is_empty()).count();
                for line in body.lines().map(str::trim).filter(|l| !l.is_empty()).take(3) {
                    if added_lines.len() < 3 {
                        added_lines.push(ProcessDetailLine { text: format!("+ {}", line.chars().take(80).collect::<String>()), tone: DetailTone::Add });
                    }
                }
            }
            if added_lines.is_empty() {
                let summary = args.get("summary").or_else(|| args.get("title")).and_then(Value::as_str).unwrap_or(tr("更新笔记内容", "Update note"));
                added_lines.push(ProcessDetailLine { text: format!("+ {summary}"), tone: DetailTone::Add });
            }
            let write_label = if line_count > 0 {
                if is_en() { format!("Write {line_count} lines") } else { format!("写入 {line_count} 行") }
            } else {
                tr("生成笔记变更计划", "Write note changes").to_owned()
            };
            (ProcessStage::Write, ProcessStepIcon::Write, write_label, file, true, true, added_lines)
        }
        _ => (ProcessStage::Scan, ProcessStepIcon::Run, tr("调用工具", "Call tool").to_owned(), count_str.clone(), true, true,
            vec![ProcessDetailLine { text: format!("{name} · {count_str}"), tone: DetailTone::Neutral }]),
    };
    if failed {
        if let Some(err) = result.get("error").and_then(Value::as_str) {
            lines = vec![ProcessDetailLine { text: format!("− {err}"), tone: DetailTone::Del }];
        }
    }
    let chip = if failed { tr("失败", "Failed").to_owned() } else if result_opt.is_none() { tr("状态未知", "Status unknown").to_owned() } else { chip };
    let plain = lines.iter().map(|l| l.text.as_str()).collect::<Vec<_>>().join("\n");
    (stage, ProcessStep { label, detail: chip, merged_count: 1, running: false }, ProcessStepMeta { icon, mono, detail_mono, lines }, plain)
}

pub(super) fn build_diff_chips(turn: &[Msg], notes: &[String]) -> Vec<ProcessDiffChip> {
    let mut chips: Vec<ProcessDiffChip> = Vec::new();
    for message in turn {
        if let Some(plan) = &message.plan {
            for op in &plan.ops {
                let file = note_file(&op.page);
                let (del_body, add_body) = match &op.kind {
                    agent::OpKind::Replace { old, new, .. } => (old.as_str(), new.as_str()),
                    agent::OpKind::Edit { markdown } | agent::OpKind::Create { markdown } => ("", markdown.as_str()),
                };
                let del = del_body.lines().filter(|l| !l.trim().is_empty()).count();
                let add = add_body.lines().filter(|l| !l.trim().is_empty()).count().max(1);
                let mut lines = Vec::new();
                for l in del_body.lines().map(str::trim).filter(|l| !l.is_empty()).take(2) {
                    lines.push(ProcessDetailLine { text: l.chars().take(72).collect(), tone: DetailTone::Del });
                }
                for l in add_body.lines().map(str::trim).filter(|l| !l.is_empty()).take(3) {
                    lines.push(ProcessDetailLine { text: l.chars().take(72).collect(), tone: DetailTone::Add });
                }
                if let Some(existing) = chips.iter_mut().find(|c| c.file == file) {
                    existing.add += add; existing.del += del;
                    if existing.lines.len() < 5 { existing.lines.extend(lines.into_iter().take(5 - existing.lines.len())); }
                } else {
                    chips.push(ProcessDiffChip { file, add, del, lines });
                }
            }
        }
    }
    for note in notes {
        let file = note_file(note);
        if chips.iter().any(|c| c.file == file) { continue; }
        let mut preview_lines = Vec::new();
        let mut add = 0usize;
        for m in turn.iter().filter(|m| m.role == "tool") {
            if let Ok(val) = serde_json::from_str::<Value>(&m.content) {
                if val.get("title").or_else(|| val.get("pageId")).and_then(Value::as_str) == Some(note.as_str()) {
                    if let Some(md) = val.get("markdown").and_then(Value::as_str) {
                        add = md.lines().filter(|l| !l.trim().is_empty()).count();
                        for l in md.lines().map(str::trim).filter(|l| !l.is_empty()).take(4) {
                            preview_lines.push(ProcessDetailLine { text: l.chars().take(72).collect(), tone: DetailTone::Add });
                        }
                    }
                }
            }
        }
        if preview_lines.is_empty() {
            add = note.chars().count().max(3) * 2;
            preview_lines.push(ProcessDetailLine { text: format!("# {note}"), tone: DetailTone::Neutral });
            preview_lines.push(ProcessDetailLine { text: format!("{} {file}", tr("已关联笔记", "Referenced note")), tone: DetailTone::Add });
        }
        chips.push(ProcessDiffChip { file, add: add.max(1), del: 0, lines: preview_lines });
    }
    chips
}

pub(super) fn format_headline(turn: &[Msg], steps: &[ProcessStep], stage: ProcessStage, streaming: bool) -> String {
    if streaming {
        return match stage {
            ProcessStage::Skill => tr("正在加载工作区技能…", "Loading workspace skill…"),
            ProcessStage::Scan => tr("正在扫描笔记本目录…", "Scanning notebook pages…"),
            ProcessStage::Read => tr("正在读取笔记内容…", "Reading notes…"),
            ProcessStage::Write => tr("正在生成笔记变更计划…", "Creating note change plan…"),
            _ => tr("正在汇总并生成回答…", "Preparing answer…"),
        }.to_owned();
    }
    match turn.iter().rev().find(|m| m.role == "turn_outcome").map(|m| m.content.as_str()) {
        Some("failed") => tr("执行失败", "Execution failed").into(),
        Some("stopped") => tr("已停止", "Stopped").into(),
        _ => {
            let calls: usize = turn.iter()
                .filter_map(|m| m.tool_calls.as_ref().and_then(Value::as_array).map(Vec::len))
                .sum::<usize>()
                .max(steps.len());
            let msgs = turn.iter()
                .filter(|m| matches!(m.role.as_str(), "user" | "assistant") && !m.content.trim().is_empty())
                .count()
                .max(1);
            if is_en() {
                format!("{calls} tool {}, {msgs} {}", if calls == 1 { "call" } else { "calls" }, if msgs == 1 { "message" } else { "messages" })
            } else {
                format!("{calls} 次工具调用，{msgs} 条消息")
            }
        }
    }
}
