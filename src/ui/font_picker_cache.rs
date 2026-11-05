//! Font catalog caching and query filtering for the font picker popover.
use std::cell::RefCell;
use std::collections::HashSet;
use std::rc::Rc;
use gpui::SharedString;
use crate::theme::{
    font_catalog_version, imported_fonts, installed_fonts, is_monospace_name,
    resolved_body_font, resolved_mono_font,
};
use super::font_picker::FontPickerRole;

#[derive(Clone)]
pub struct CandidateItem {
    pub name: SharedString,
    pub search_key: String,
    pub words: Vec<String>,
    pub subwords: Vec<String>,
}

#[derive(Clone)]
pub struct FlatFontItem {
    pub name: SharedString,
    pub selected: bool,
}

struct CachedCatalog {
    version: u64,
    catalog: Rc<[CandidateItem]>,
}

thread_local! {
    static BODY_CATALOG: RefCell<Option<CachedCatalog>> = const { RefCell::new(None) };
    static CODE_CATALOG: RefCell<Option<CachedCatalog>> = const { RefCell::new(None) };
}

pub fn get_or_build_catalog(role: FontPickerRole) -> Rc<[CandidateItem]> {
    let ver = font_catalog_version();

    let slot = match role {
        FontPickerRole::Body => &BODY_CATALOG,
        FontPickerRole::Code => &CODE_CATALOG,
    };

    slot.with(|cell| {
        let mut guard = cell.borrow_mut();
        if let Some(cached) = guard.as_ref() {
            if cached.version == ver {
                return cached.catalog.clone();
            }
        }
        let built: Rc<[CandidateItem]> = Rc::from(build_candidates(role).into_boxed_slice());
        *guard = Some(CachedCatalog {
            version: ver,
            catalog: built.clone(),
        });
        built
    })
}

fn build_candidates(role: FontPickerRole) -> Vec<CandidateItem> {
    let mut names = imported_fonts();
    let installed = installed_fonts();
    match role {
        FontPickerRole::Body => {
            names.extend(installed);
            names.push(resolved_body_font().to_string());
        }
        FontPickerRole::Code => {
            names.extend(installed.into_iter().filter(|f| is_monospace_name(f)));
            names.push(resolved_mono_font().to_string());
        }
    }

    let mut seen = HashSet::new();
    let mut unique = Vec::new();
    for name in names {
        let trimmed = name.trim();
        if trimmed.is_empty() {
            continue;
        }
        let key = trimmed.to_lowercase();
        if seen.insert(key.clone()) {
            let (words, subwords) = tokenize_font_name(trimmed);
            unique.push(CandidateItem {
                name: SharedString::from(trimmed.to_string()),
                search_key: key,
                words,
                subwords,
            });
        }
    }
    unique.sort_by(|a, b| a.search_key.cmp(&b.search_key));
    unique
}

fn is_delimiter(c: char) -> bool {
    c.is_whitespace() || matches!(c, '-' | '_' | '/' | '.' | '(' | ')')
}

fn tokenize_font_name(name: &str) -> (Vec<String>, Vec<String>) {
    let mut words = Vec::new();
    let mut subwords = Vec::new();
    for raw_word in name.split(is_delimiter).filter(|s| !s.is_empty()) {
        words.push(raw_word.to_lowercase());
        let chars: Vec<char> = raw_word.chars().collect();
        let mut start = 0;
        for i in 1..chars.len() {
            let (prev, curr) = (chars[i - 1], chars[i]);
            let next = chars.get(i + 1).copied();
            let camel = (prev.is_lowercase() || prev.is_ascii_digit()) && curr.is_uppercase();
            let acronym = prev.is_uppercase() && curr.is_uppercase() && next.is_some_and(char::is_lowercase);
            let digit_edge = prev.is_alphabetic() != curr.is_alphabetic()
                && (prev.is_ascii_digit() || curr.is_ascii_digit());
            if camel || acronym || digit_edge {
                let piece: String = chars[start..i].iter().collect();
                subwords.push(piece.to_lowercase());
                start = i;
            }
        }
        let tail: String = chars[start..].iter().collect();
        subwords.push(tail.to_lowercase());
    }
    (words, subwords)
}

fn score_candidate(item: &CandidateItem, q: &str, q_tokens: &[&str]) -> Option<(u8, usize)> {
    if item.search_key == q {
        return Some((0, 0));
    }
    if item.search_key.starts_with(q) {
        return Some((1, 0));
    }
    if let Some(idx) = item.words.iter().skip(1).position(|w| w.starts_with(q)) {
        return Some((2, idx + 1));
    }
    if let Some(idx) = item.subwords.iter().skip(1).position(|sw| sw.starts_with(q)) {
        return Some((3, idx + 1));
    }
    if q_tokens.len() > 1 {
        let mut sw_iter = item.subwords.iter().enumerate();
        let mut first_pos = None;
        let mut all_matched = true;
        for &tok in q_tokens {
            if let Some((idx, _)) = sw_iter.find(|(_, sw)| sw.starts_with(tok)) {
                first_pos.get_or_insert(idx);
            } else {
                all_matched = false;
                break;
            }
        }
        if all_matched {
            return Some((3, first_pos.unwrap_or(0)));
        }
    }
    if (!q.is_ascii() || q.len() >= 3)
        && let Some(pos) = item.search_key.find(q)
    {
        return Some((4, pos));
    }
    None
}

pub fn filter_candidates(
    catalog: &[CandidateItem],
    query: &str,
    current_font: &str,
) -> Rc<[FlatFontItem]> {
    let q = query.trim().to_lowercase();
    if q.is_empty() {
        let flat: Vec<FlatFontItem> = catalog
            .iter()
            .map(|item| FlatFontItem {
                name: item.name.clone(),
                selected: item.name.as_ref().eq_ignore_ascii_case(current_font),
            })
            .collect();
        return Rc::from(flat.into_boxed_slice());
    }

    let q_tokens: Vec<&str> = q.split_whitespace().collect();
    let mut ranked: Vec<((u8, usize), &CandidateItem)> = catalog
        .iter()
        .filter_map(|item| score_candidate(item, &q, &q_tokens).map(|score| (score, item)))
        .collect();

    ranked.sort_by(|(sa, a), (sb, b)| sa.cmp(sb).then_with(|| a.search_key.cmp(&b.search_key)));

    let flat: Vec<FlatFontItem> = ranked
        .into_iter()
        .map(|(_, item)| FlatFontItem {
            name: item.name.clone(),
            selected: item.name.as_ref().eq_ignore_ascii_case(current_font),
        })
        .collect();

    Rc::from(flat.into_boxed_slice())
}
