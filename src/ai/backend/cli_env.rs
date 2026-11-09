use std::{env, ffi::OsString, path::PathBuf, process::Command};
use super::BackendKind;

pub(crate) fn enriched_path() -> OsString {
    let mut paths: Vec<PathBuf> = env::var_os("PATH")
        .map(|val| env::split_paths(&val).collect())
        .unwrap_or_default();
    if let Some(home) = dirs::home_dir() {
        for rel in [
            ".npm-global/bin",
            ".local/share/fnm/aliases/default/bin",
            ".local/bin",
            ".bun/bin",
            ".cargo/bin",
        ] {
            let dir = home.join(rel);
            if !paths.contains(&dir) {
                paths.push(dir);
            }
        }
    }
    for fixed in [
        "/opt/homebrew/bin",
        "/usr/local/bin",
        "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS",
        "/usr/bin",
        "/bin",
        "/usr/sbin",
        "/sbin",
    ] {
        let dir = PathBuf::from(fixed);
        if !paths.contains(&dir) {
            paths.push(dir);
        }
    }
    env::join_paths(paths).unwrap_or_default()
}

pub(crate) fn resolve(bin: &str) -> Option<PathBuf> {
    let path = enriched_path();
    for dir in env::split_paths(&path) {
        let candidate = dir.join(bin);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

pub(crate) fn command(bin: &str) -> Command {
    let executable = resolve(bin).unwrap_or_else(|| PathBuf::from(bin));
    let mut cmd = Command::new(executable);
    cmd.env("PATH", enriched_path());
    cmd
}

pub(crate) fn is_installed(kind: BackendKind) -> bool {
    match kind {
        BackendKind::Builtin => true,
        BackendKind::Codex => resolve("codex").is_some(),
        BackendKind::Claude => resolve("claude").is_some(),
        BackendKind::Dsh => resolve("dsh").is_some(),
        BackendKind::Pi => resolve("pi").is_some(),
        BackendKind::Agy => resolve("agy").is_some(),
    }
}
