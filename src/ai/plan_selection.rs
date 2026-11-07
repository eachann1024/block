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

pub(crate) fn diff(conv: u64, at: usize, page: &str, before: &str, after: &str) -> Result<Vec<DiffLine>, String> {
    SELECTIONS.with(|selections| {
        let mut selections = selections.borrow_mut();
        let selection = selections.entry((conv, at, page.to_owned())).or_default();
        if selection.lines.is_none() || selection.before != before || selection.after != after {
            *selection = Selection { before: before.to_owned(), after: after.to_owned(), excluded: HashSet::new(), lines: Some(lines(before, after)) };
        }
        let mut previous: Option<(i8, bool, usize)> = None;
        Ok(selection.lines.as_ref().expect("initialized").as_ref().map_err(Clone::clone)?.iter().enumerate().map(|(index, (change, text))| {
            let ordinary = !text.trim().is_empty() && !text.trim_start().starts_with('|');
            let selection_index = previous.filter(|(last_change, last_ordinary, _)| *change != 0 && *last_change == *change && *last_ordinary && ordinary)
                .map_or(index, |(_, _, group)| group);
            previous = Some((*change, ordinary, selection_index));
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
    let mut output = String::new();
    for line in diff(conv, at, page, before, after)?.into_iter().filter(|line|
        line.change == 0 || (line.change < 0 && !line.included) || (line.change > 0 && line.included)) {
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
