//! Version shown in the app. Releases stamp `1.0.<git commit count>` before compiling.

pub const VERSION: &str = env!("CARGO_PKG_VERSION");
