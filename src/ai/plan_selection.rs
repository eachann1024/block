use std::{cell::RefCell, collections::{HashMap, HashSet}};

#[derive(Clone)]
pub(crate) struct DiffLine { pub text: String, pub change: i8, pub index: usize, pub selection_index: usize, pub included: bool }

#[derive(Default)]
struct Selection { before: String, after: String, excluded: HashSet<usize>, lines: Option<Result<Vec<(i8, String)>, String>> }

thread_local! { static SELECTIONS: RefCell<HashMap<(u64, usize, String), Selection>> = RefCell::new(HashMap::new()); }

fn lines(before: &str, after: &str) -> Result<Vec<(i8, String)>, String> {
    let left: Vec<_> = before.split_inclusive('\n').collect();
    let right: Vec<_> = after.split_inclusive('\n').collect();
    if left.len().saturating_mul(right.len()) > 4_000_000 { return Err("Change is too large for selective review".into()); }
    let width = right.len() + 1;
    let mut table = vec![0u32; (left.len() + 1) * width];
    for row in (0..left.len()).rev() {
        for col in (0..right.len()).rev() {
            table[row * width + col] = if left[row] == right[col] { 1 + table[(row + 1) * width + col + 1] }
                else { table[(row + 1) * width + col].max(table[row * width + col + 1]) };
        }
    }
    let (mut row, mut col) = (0, 0);
    let mut result = Vec::new();
    while row < left.len() || col < right.len() {
        if row < left.len() && col < right.len() && left[row] == right[col] {
            result.push((0, left[row].to_owned())); row += 1; col += 1;
        } else if row < left.len() && (col == right.len() || table[(row + 1) * width + col] >= table[row * width + col + 1]) {
            result.push((-1, left[row].to_owned())); row += 1;
        } else { result.push((1, right[col].to_owned())); col += 1; }
    }
    Ok(result)
}

fn is_table_row_line(trimmed: &str) -> bool {
    trimmed.starts_with('|') || trimmed.to_ascii_lowercase().starts_with("<tr")
}

fn is_html_table_scaffolding(trimmed: &str) -> bool {
    let lower = trimmed.to_ascii_lowercase();
    lower.starts_with("<table") || lower.starts_with("</table") || lower.starts_with("<colgroup")
        || lower.starts_with("</colgroup") || lower.starts_with("<col ") || lower.starts_with("<col>")
        || lower.starts_with("<thead") || lower.starts_with("</thead") || lower.starts_with("<tbody")
        || lower.starts_with("</tbody") || lower.starts_with("<tfoot") || lower.starts_with("</tfoot")
        || lower == "<tr>" || lower == "</tr>"
}

fn is_pipe_separator(trimmed: &str) -> bool {
    trimmed.starts_with('|') && trimmed.chars().all(|ch| matches!(ch, '|' | ' ' | '-' | ':'))
}

pub(crate) fn diff(conv: u64, at: usize, page: &str, before: &str, after: &str) -> Result<Vec<DiffLine>, String> {
    SELECTIONS.with(|selections| {
        let mut selections = selections.borrow_mut();
        let selection = selections.entry((conv, at, page.to_owned())).or_default();
        if selection.lines.is_none() || selection.before != before || selection.after != after {
            *selection = Selection { before: before.to_owned(), after: after.to_owned(), excluded: HashSet::new(), lines: Some(lines(before, after)) };
        }
        let raw = selection.lines.as_ref().expect("initialized").as_ref().map_err(Clone::clone)?;
        let mut groups = vec![0usize; raw.len()];
        let mut previous: Option<(i8, bool, usize)> = None;
        for (index, (change, text)) in raw.iter().enumerate() {
            let trimmed = text.trim();
            if *change == 0 {
                previous = None;
                groups[index] = index;
            } else if trimmed.is_empty() {
                groups[index] = previous.filter(|(last_change, _, _)| *last_change == *change).map_or(index, |(_, _, group)| group);
            } else {
                let ordinary = !is_table_row_line(trimmed);
                let group = previous.filter(|(last_change, last_ordinary, _)| *last_change == *change && *last_ordinary && ordinary)
                    .map_or(index, |(_, _, group)| group);
                groups[index] = group;
                previous = Some((*change, ordinary, group));
            }
        }
        let mut next_group: Option<(i8, usize)> = None;
        for index in (0..raw.len()).rev() {
            let (change, text) = &raw[index];
            if *change == 0 {
                next_group = None;
            } else if text.trim().is_empty() {
                if groups[index] == index && let Some((next_change, group)) = next_group.filter(|(c, _)| *c == *change) {
                    let _ = next_change;
                    groups[index] = group;
                }
            } else {
                next_group = Some((*change, groups[index]));
            }
        }
        Ok(raw.iter().enumerate().map(|(index, (change, text))| {
            let selection_index = groups[index];
            DiffLine { text: text.clone(), change: *change, index, selection_index, included: !selection.excluded.contains(&selection_index) }
        }).collect())
    })
}

pub(crate) fn toggle(conv: u64, at: usize, page: &str, index: usize) {
    SELECTIONS.with(|selections| if let Some(selection) = selections.borrow_mut().get_mut(&(conv, at, page.to_owned())) {
        if !selection.excluded.remove(&index) { selection.excluded.insert(index); }
    });
}

pub(crate) fn selected(conv: u64, at: usize, page: &str, before: &str, after: &str) -> Result<String, String> {
    let has_selection = SELECTIONS.with(|selections| selections.borrow().get(&(conv, at, page.to_owned()))
        .is_some_and(|selection| !selection.excluded.is_empty()));
    if !has_selection { return Ok(after.to_owned()); }
    let lines = diff(conv, at, page, before, after)?;
    let mut keep: Vec<bool> = lines.iter().map(|line| {
        line.change == 0 || (line.change < 0 && !line.included) || (line.change > 0 && line.included)
    }).collect();
    let mut i = 0;
    while i < lines.len() {
        let lower = lines[i].text.trim().to_ascii_lowercase();
        if lower.starts_with("<table") {
            let start = i;
            while i < lines.len() && !lines[i].text.trim().to_ascii_lowercase().starts_with("</table") { i += 1; }
            let end = (i + 1).min(lines.len());
            let any_row = (start..end).any(|idx| {
                let t = lines[idx].text.trim();
                t.to_ascii_lowercase().starts_with("<tr") && !is_html_table_scaffolding(t) && keep[idx]
            });
            for idx in start..end {
                if is_html_table_scaffolding(lines[idx].text.trim()) { keep[idx] = any_row; }
            }
        } else if lines[i].text.trim().starts_with('|') {
            let start = i;
            while i < lines.len() && lines[i].text.trim().starts_with('|') && lines[i].change == lines[start].change { i += 1; }
            let end = i;
            let any_row = (start..end).any(|idx| !is_pipe_separator(lines[idx].text.trim()) && keep[idx]);
            for idx in start..end {
                if is_pipe_separator(lines[idx].text.trim()) { keep[idx] = any_row; }
            }
            continue;
        }
        i += 1;
    }
    let mut output = String::new();
    for (idx, line) in lines.into_iter().enumerate() {
        if !keep[idx] { continue; }
        if !output.is_empty() && !output.ends_with('\n') { output.push('\n'); }
        output.push_str(&line.text);
    }
    Ok(output)
}

pub(crate) fn clear(conv: u64, at: usize) {
    SELECTIONS.with(|selections| selections.borrow_mut().retain(|(id, index, _), _| *id != conv || *index != at));
}

pub(crate) fn clear_page(conv: u64, at: usize, page: &str) {
    SELECTIONS.with(|selections| { selections.borrow_mut().remove(&(conv, at, page.to_owned())); });
}
