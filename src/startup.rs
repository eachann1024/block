/// `BLOCK_BACKGROUND_LAUNCH=1` is an optional product launch: the real main window
/// is still created and kept alive, but it is not shown or focused. Any other value
/// (including unset) is a normal foreground launch. Later explicit reopen is unaffected.
pub fn background_launch() -> bool {
    std::env::var_os("BLOCK_BACKGROUND_LAUNCH").is_some_and(|value| value == "1")
}
