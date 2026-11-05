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
            unique.push(CandidateItem {
                name: SharedString::from(trimmed.to_string()),
                search_key: key,
            });
        }
    }
    unique.sort_by(|a, b| a.search_key.cmp(&b.search_key));
    unique
}

pub fn filter_candidates(
    catalog: &[CandidateItem],
    query: &str,
    current_font: &str,
) -> Rc<[FlatFontItem]> {
    let q = query.trim().to_lowercase();
    let mut flat = Vec::with_capacity(catalog.len());

    for item in catalog {
        if q.is_empty() || item.search_key.contains(&q) {
            flat.push(FlatFontItem {
                name: item.name.clone(),
                selected: item.name.as_ref().eq_ignore_ascii_case(current_font),
            });
        }
    }

    Rc::from(flat.into_boxed_slice())
}
