//! Remember paper choices independently of a note's content and export scope.
use serde::{Deserialize, Serialize};
use super::ExportTheme;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct ExportPreferences {
    pub style: ExportTheme,
    pub light_style: ExportTheme,
    pub dark_style: ExportTheme,
    pub show_title: bool,
    pub watermark: bool,
}

impl Default for ExportPreferences {
    fn default() -> Self {
        Self {
            style: ExportTheme::default(),
            light_style: ExportTheme::default_for(false),
            dark_style: ExportTheme::default_for(true),
            show_title: true,
            watermark: true,
        }
    }
}

impl ExportPreferences {
    pub fn load() -> Self {
        let mut saved = crate::files::load_state().ok().and_then(|s| s.export_preferences).unwrap_or_default();
        if saved.light_style.is_dark() { saved.light_style = ExportTheme::default_for(false); }
        if !saved.dark_style.is_dark() { saved.dark_style = ExportTheme::default_for(true); }
        if saved.style.is_dark() { saved.dark_style = saved.style; }
        else { saved.light_style = saved.style; }
        saved
    }

    pub fn save(self) -> std::io::Result<()> {
        crate::files::update_state(|state| state.export_preferences = Some(self)).map(|_| ())
    }
}
