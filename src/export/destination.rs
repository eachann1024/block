//! Suggested filenames and the last successfully used export directory.
use super::ExportFormat;

pub(super) fn suggested_name(title: &str, format: ExportFormat) -> String {
    let clean: String = title.chars().map(|ch| {
        if ch.is_control() || matches!(ch, '/' | '\\' | ':' | '*' | '?' | '"' | '<' | '>' | '|') {
            '-'
        } else {
            ch
        }
    }).collect();
    // Leave room for the extension on filesystems with 255-byte filename limits.
    let mut stem = String::new();
    for ch in clean.trim_matches(|ch: char| ch.is_whitespace() || ch == '.').chars() {
        if stem.len() + ch.len_utf8() > 180 { break; }
        stem.push(ch);
    }
    stem.truncate(stem.trim_end_matches(|ch: char| ch.is_whitespace() || ch == '.').len());
    if stem.is_empty() { stem = crate::theme::tr("未命名", "Untitled").into(); }
    let base = stem.split('.').next().unwrap_or_default().trim_end().to_ascii_uppercase();
    let reserved = matches!(base.as_str(), "CON" | "PRN" | "AUX" | "NUL" | "CLOCK$")
        || ["COM", "LPT"].into_iter().any(|prefix| base.strip_prefix(prefix)
            .is_some_and(|n| matches!(n, "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" | "¹" | "²" | "³")));
    if reserved { stem.insert(0, '_'); }
    format!("{stem}.{}", format.extension())
}

#[cfg(not(target_family = "wasm"))]
pub(super) fn directory() -> std::path::PathBuf {
    if let Some(last) = crate::files::load_state().ok().and_then(|s| s.export_directory) {
        // A removed subfolder can still reopen at its nearest surviving parent.
        if let Some(folder) = last.ancestors().find(|p| p.is_absolute() && p.parent().is_some() && std::fs::read_dir(p).is_ok()) {
            return folder.to_path_buf();
        }
    }
    [dirs::download_dir(), dirs::document_dir(), dirs::home_dir()]
        .into_iter().flatten().find(|p| std::fs::read_dir(p).is_ok())
        .unwrap_or_else(std::env::temp_dir)
}

#[cfg(not(target_family = "wasm"))]
pub(super) fn remember(path: &std::path::Path) -> std::io::Result<()> {
    if let Some(parent) = path.parent().filter(|p| p.is_absolute()) {
        crate::files::update_state(|state| state.export_directory = Some(parent.to_path_buf()))?;
    }
    Ok(())
}
