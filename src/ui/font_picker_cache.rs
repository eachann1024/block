//! Font catalog caching and query filtering for the font picker popover.
use std::cell::RefCell;
use std::collections::HashSet;
use std::rc::Rc;
use gpui::SharedString;
use crate::files::state::LanguageMode;
use crate::theme::{
    font_catalog_version, imported_fonts, installed_fonts, is_monospace_name,
    language_mode, resolved_mono_font, tr,
};
use super::font_picker::FontPickerRole;

#[derive(Clone)]
pub struct CandidateItem {
    pub id: Option<String>,
    pub name: SharedString,
    pub search_key: String,
}

#[derive(Clone)]
pub struct CandidateGroup {
    pub title: SharedString,
    pub items: Vec<CandidateItem>,
}

#[derive(Clone)]
pub enum FlatFontItem {
    Header(SharedString),
    Font {
        id: Option<String>,
        name: SharedString,
        selected: bool,
    },
}

struct CachedCatalog {
    version: u64,
    lang: LanguageMode,
    catalog: Rc<Vec<CandidateGroup>>,
}

thread_local! {
    static BODY_CATALOG: RefCell<Option<CachedCatalog>> = const { RefCell::new(None) };
    static CODE_CATALOG: RefCell<Option<CachedCatalog>> = const { RefCell::new(None) };
}

pub fn get_or_build_catalog(role: FontPickerRole) -> Rc<Vec<CandidateGroup>> {
    let ver = font_catalog_version();
    let lang = language_mode();

    let slot = match role {
        FontPickerRole::Body => &BODY_CATALOG,
        FontPickerRole::Code => &CODE_CATALOG,
    };

    slot.with(|cell| {
        let mut guard = cell.borrow_mut();
        if let Some(cached) = guard.as_ref() {
            if cached.version == ver && cached.lang == lang {
                return cached.catalog.clone();
            }
        }
        let built = Rc::new(build_candidate_groups(role));
        *guard = Some(CachedCatalog {
            version: ver,
            lang,
            catalog: built.clone(),
        });
        built
    })
}

fn build_candidate_groups(role: FontPickerRole) -> Vec<CandidateGroup> {
    let imported = imported_fonts();
    let installed = installed_fonts();
    let imported_set: HashSet<String> = imported.iter().map(|f| f.to_lowercase()).collect();
    let installed_deduped: Vec<String> = installed
        .into_iter()
        .filter(|f| !imported_set.contains(&f.to_lowercase()))
        .collect();

    let to_candidates = |list: Vec<String>| -> Vec<CandidateItem> {
        list.into_iter()
            .map(|f| {
                let search_key = f.to_lowercase();
                CandidateItem {
                    id: Some(f.clone()),
                    name: f.into(),
                    search_key,
                }
            })
            .collect()
    };

    let mut groups = Vec::new();
    match role {
        FontPickerRole::Body => {
            let sys = vec![
                CandidateItem {
                    id: None,
                    name: tr("系统无衬线", "System Sans").into(),
                    search_key: tr("系统无衬线", "System Sans").to_lowercase(),
                },
                CandidateItem {
                    id: Some("system-serif".into()),
                    name: tr("系统衬线", "System Serif").into(),
                    search_key: tr("系统衬线", "System Serif").to_lowercase(),
                },
            ];
            groups.push(CandidateGroup {
                title: tr("系统", "System").into(),
                items: sys,
            });

            let imp = to_candidates(imported);
            if !imp.is_empty() {
                groups.push(CandidateGroup {
                    title: tr("已导入", "Imported").into(),
                    items: imp,
                });
            }

            let inst = to_candidates(installed_deduped);
            if !inst.is_empty() {
                groups.push(CandidateGroup {
                    title: tr("已安装", "Installed").into(),
                    items: inst,
                });
            }
        }
        FontPickerRole::Code => {
            let auto_label = format!("{} ({})", tr("自动", "Auto"), resolved_mono_font());
            let auto_key = auto_label.to_lowercase();
            let sys = vec![CandidateItem {
                id: None,
                name: auto_label.into(),
                search_key: auto_key,
            }];
            groups.push(CandidateGroup {
                title: tr("系统", "System").into(),
                items: sys,
            });

            let imp = to_candidates(imported);
            if !imp.is_empty() {
                groups.push(CandidateGroup {
                    title: tr("已导入", "Imported").into(),
                    items: imp,
                });
            }

            let mono_src: Vec<String> = installed_deduped
                .into_iter()
                .filter(|f| is_monospace_name(f))
                .collect();
            let mono = to_candidates(mono_src);
            if !mono.is_empty() {
                groups.push(CandidateGroup {
                    title: tr("等宽", "Monospace").into(),
                    items: mono,
                });
            }
        }
    }
    groups
}

pub fn filter_candidates(
    catalog: &[CandidateGroup],
    query: &str,
    current_font: Option<&str>,
) -> Rc<[FlatFontItem]> {
    let q = query.trim().to_lowercase();
    let mut flat = Vec::new();

    for group in catalog {
        let mut matches = Vec::new();
        for item in &group.items {
            if q.is_empty() || item.search_key.contains(&q) {
                let selected = match (item.id.as_deref(), current_font) {
                    (None, None) => true,
                    (Some(id), Some(cur)) => id == cur,
                    _ => false,
                };
                matches.push(FlatFontItem::Font {
                    id: item.id.clone(),
                    name: item.name.clone(),
                    selected,
                });
            }
        }
        if !matches.is_empty() {
            flat.push(FlatFontItem::Header(group.title.clone()));
            flat.extend(matches);
        }
    }

    Rc::from(flat.into_boxed_slice())
}
