//! Open a local path in the OS default app. Arguments only, never a shell string.

use std::{path::Path, process::Command};

pub(super) fn open_default(path: &Path) -> Result<(), String> {
    if !path.is_file() {
        return Err(super::copy("找不到这个文件", "This file could not be found"));
    }
    let mut child = command(path)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .spawn()
        .map_err(|_| super::copy("无法用默认应用打开", "Could not open the default app"))?;
    std::thread::spawn(move || { let _ = child.wait(); });
    Ok(())
}

fn command(path: &Path) -> Command {
    #[cfg(target_os = "macos")]
    {
        let mut cmd = Command::new("/usr/bin/open");
        cmd.arg(path);
        cmd
    }
    #[cfg(target_os = "windows")]
    {
        let mut cmd = Command::new("explorer");
        cmd.arg(path);
        cmd
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        let mut cmd = Command::new("xdg-open");
        cmd.arg(path);
        cmd
    }
}
