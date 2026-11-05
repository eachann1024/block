//! Custom font disk storage and runtime loading (`fonts/` in app data directory).
use std::borrow::Cow;
use std::path::PathBuf;
use gpui::App;

pub fn custom_fonts_dir() -> Option<PathBuf> {
    let base = dirs::state_dir().or_else(dirs::data_local_dir)?;
    Some(base.join("Block").join("fonts"))
}

pub fn font_families_from_bytes(bytes: &[u8]) -> Vec<String> {
    let mut db = cosmic_text::fontdb::Database::new();
    db.load_font_data(bytes.to_vec());
    let mut families = Vec::new();
    for face in db.faces() {
        for (family, _) in &face.families {
            let name = family.trim();
            if !name.is_empty() && !families.iter().any(|f: &String| f.eq_ignore_ascii_case(name)) {
                families.push(name.to_string());
            }
        }
    }
    families
}

pub fn load_custom_fonts_data() -> Vec<Vec<u8>> {
    let Some(fonts_dir) = custom_fonts_dir() else { return Vec::new() };
    let Ok(entries) = std::fs::read_dir(&fonts_dir) else { return Vec::new() };
    let mut out = Vec::new();
    for entry in entries.flatten() {
        let path = entry.path();
        let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
        if !matches!(ext.as_str(), "ttf" | "otf" | "ttc") { continue; }
        if let Ok(bytes) = std::fs::read(&path) {
            if crate::fonts::is_valid_font_header(&bytes) && !font_families_from_bytes(&bytes).is_empty() {
                out.push(bytes);
            }
        }
    }
    out
}

pub fn load_custom_fonts(cx: &mut App) {
    let Some(fonts_dir) = custom_fonts_dir() else { return };
    if let Err(e) = std::fs::create_dir_all(&fonts_dir) {
        crate::log::warn(&format!("Failed to create custom fonts directory {:?}: {e}", fonts_dir));
        return;
    }
    let Ok(entries) = std::fs::read_dir(&fonts_dir) else { return };
    let mut all_families = Vec::new();
    for entry in entries.flatten() {
        let path = entry.path();
        let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("").to_lowercase();
        if !matches!(ext.as_str(), "ttf" | "otf" | "ttc") { continue; }
        let bytes = match std::fs::read(&path) {
            Ok(b) => b,
            Err(e) => {
                crate::log::warn(&format!("Failed to read custom font {:?}: {e}", path));
                continue;
            }
        };
        if !crate::fonts::is_valid_font_header(&bytes) {
            crate::log::warn(&format!("Invalid font header in custom font {:?}", path));
            continue;
        }
        let families = font_families_from_bytes(&bytes);
        if families.is_empty() {
            crate::log::warn(&format!("No valid font family found in custom font {:?}", path));
            continue;
        }
        if let Err(e) = cx.text_system().add_fonts(vec![Cow::Owned(bytes.clone())]) {
            crate::log::warn(&format!("Failed to register custom font {:?}: {e}", path));
            continue;
        }
        all_families.extend(families);
    }
    if !all_families.is_empty() {
        crate::theme::add_imported_fonts(&all_families);
    }
}
