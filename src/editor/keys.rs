use gpui::{App, KeyBinding};

use super::view::{
    Backspace, Bold, Code, Copy, Cut, Delete, Down, Enter, Italic, Left, Paste, Right, SelectAll,
    SelectLeft, SelectRight, SelectUp, SelectDown, Strike, Up, Undo, Redo, Cancel, Save,
};

pub fn bind_keys(cx: &mut App) {
    cx.bind_keys([
        KeyBinding::new("left", Left, Some("Editor")),
        KeyBinding::new("right", Right, Some("Editor")),
        KeyBinding::new("shift-left", SelectLeft, Some("Editor")),
        KeyBinding::new("shift-right", SelectRight, Some("Editor")),
        KeyBinding::new("up", Up, Some("Editor")),
        KeyBinding::new("down", Down, Some("Editor")),
        KeyBinding::new("shift-up", SelectUp, Some("Editor")),
        KeyBinding::new("shift-down", SelectDown, Some("Editor")),
        KeyBinding::new("cmd-z", Undo, Some("Editor")),
        KeyBinding::new("cmd-shift-z", Redo, Some("Editor")),
        KeyBinding::new("escape", Cancel, Some("Editor")),
        KeyBinding::new("cmd-s", Save, Some("Editor")),
        KeyBinding::new("enter", Enter, Some("Editor")),
        KeyBinding::new("backspace", Backspace, Some("Editor")),
        KeyBinding::new("delete", Delete, Some("Editor")),
        KeyBinding::new("cmd-a", SelectAll, Some("Editor")),
        KeyBinding::new("cmd-c", Copy, Some("Editor")),
        KeyBinding::new("cmd-x", Cut, Some("Editor")),
        KeyBinding::new("cmd-v", Paste, Some("Editor")),
        KeyBinding::new("cmd-b", Bold, Some("Editor")),
        KeyBinding::new("cmd-i", Italic, Some("Editor")),
        KeyBinding::new("cmd-shift-s", Strike, Some("Editor")),
        KeyBinding::new("cmd-e", Code, Some("Editor")),
    ]);
}
