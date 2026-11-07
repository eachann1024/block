use std::{io::{BufRead, BufReader}, process::{Child, Command, Stdio}, sync::mpsc};

pub(super) enum Event { Text(String), Error(String), Done }

pub(super) struct Process { child: Child }

impl Process {
    pub(super) fn start() -> Result<(Self, mpsc::Receiver<Event>), String> {
        if !cfg!(target_os = "macos") { return Err("unsupported".into()); }
        let executable = std::env::current_exe().map_err(|error| error.to_string())?;
        let path = executable.parent().ok_or("helper_missing")?.join("block-dictation");
        if !path.is_file() { return Err("helper_missing".into()); }
        let mut child = Command::new(path).stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::null())
            .spawn().map_err(|error| format!("helper_start: {error}"))?;
        let stdout = child.stdout.take().ok_or("helper_start")?;
        let (sender, receiver) = mpsc::channel();
        std::thread::spawn(move || {
            for line in BufReader::new(stdout).lines() {
                let Ok(line) = line else { break };
                let Ok(value) = serde_json::from_str::<serde_json::Value>(&line) else { continue };
                let text = value["text"].as_str().unwrap_or_default().to_owned();
                let event = match value["type"].as_str() {
                    Some("text") => Event::Text(text),
                    Some("error") => Event::Error(text),
                    Some("done") => Event::Done,
                    _ => continue,
                };
                if sender.send(event).is_err() { break; }
            }
        });
        Ok((Self { child }, receiver))
    }
}

impl Drop for Process {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}
