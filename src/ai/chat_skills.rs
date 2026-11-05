use std::path::{Path, PathBuf};
use super::super::backend::skill_resources::SkillResourceGrant;

pub(in crate::editor::ai) fn catalog(project: Option<&Path>) -> Vec<(String, SkillResourceGrant)> {
    let mut parents: Vec<PathBuf> = Vec::new();
    if let Some(home) = dirs::home_dir() {
        for name in [".agents/skills", ".codex/skills", ".claude/skills", ".dsh/skills"] { parents.push(home.join(name)); }
    }
    if let Some(root) = project {
        for name in [".agents/skills", ".codex/skills", ".claude/skills", ".dsh/skills"] { parents.push(root.join(name)); }
    }
    let mut result = Vec::new();
    for parent in parents {
        let Ok(entries) = std::fs::read_dir(parent) else { continue };
        for entry in entries.flatten() {
            let Ok(root) = entry.path().canonicalize() else { continue };
            if !root.is_dir() || !root.join("SKILL.md").is_file() || result.iter().any(|(_, grant): &(String, SkillResourceGrant)| grant.canonical_root == root) { continue; }
            use std::hash::{Hash, Hasher};
            let mut hash = std::collections::hash_map::DefaultHasher::new(); root.hash(&mut hash);
            let stable_id = format!("skill-{:016x}", hash.finish());
            let name = entry.file_name().to_string_lossy().into_owned();
            result.push((name, SkillResourceGrant { stable_id, canonical_root: root }));
        }
    }
    result.sort_by(|a, b| a.1.stable_id.cmp(&b.1.stable_id));
    result
}
