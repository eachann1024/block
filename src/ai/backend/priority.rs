/// Drain terminal events before taking work and again immediately afterwards.
pub(crate) fn next_tool<T>(mut terminated: impl FnMut() -> bool, next: impl FnOnce() -> Option<T>) -> Result<Option<T>, ()> {
    if terminated() { return Err(()); }
    let work = next();
    if terminated() { Err(()) } else { Ok(work) }
}
