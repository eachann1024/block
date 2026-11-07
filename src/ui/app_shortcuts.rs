//! Application shortcuts run before focused inputs; editor commands remain contextual.
use gpui::{App, Global, KeyBinding, KeystrokeEvent, Window};

#[derive(Default)]
struct Registry(Vec<KeyBinding>);
impl Global for Registry {}

pub(crate) fn bind(cx: &mut App, bindings: impl IntoIterator<Item = KeyBinding>) {
    let bindings: Vec<_> = bindings.into_iter().collect();
    if !cx.has_global::<Registry>() { cx.set_global(Registry::default()); }
    let registry = cx.global_mut::<Registry>();
    for binding in &bindings {
        // Only modified application commands override text input. Escape/arrows stay local.
        registry.0.retain(|old| old.keystrokes() != binding.keystrokes() || old.action().name() != binding.action().name());
        registry.0.push(binding.clone());
    }
    cx.bind_keys(bindings);
}

pub(crate) fn capture(event: &KeystrokeEvent, window: &mut Window, cx: &mut App) {
    if event.input_preference == gpui::InputPreference::CharacterInput { return; }
    let modifiers = event.keystroke.modifiers;
    if !modifiers.platform && !modifiers.control && !modifiers.alt { return; }
    let contexts = window.context_stack();
    let action = cx.try_global::<Registry>().and_then(|registry| registry.0.iter().filter(|binding| {
        binding.match_keystrokes(std::slice::from_ref(&event.keystroke)) == Some(false)
            && binding.predicate().is_none_or(|predicate| predicate.eval(&contexts))
    }).max_by_key(|binding| binding.predicate().map_or(0, |predicate| {
        (1..=contexts.len()).find(|end| predicate.eval(&contexts[..*end])).unwrap_or(0)
    }))).map(|binding| binding.action().boxed_clone());
    if let Some(action) = action {
        cx.stop_propagation();
        window.dispatch_action(action, cx);
    }
}

/// Local editing bindings stay local; context-free commands join the application router.
pub(crate) fn bind_editor(cx: &mut App, bindings: impl IntoIterator<Item = KeyBinding>) {
    let bindings: Vec<_> = bindings.into_iter().collect();
    bind(cx, bindings.iter().filter(|binding| binding.predicate().is_none()).cloned());
    cx.bind_keys(bindings.into_iter().filter(|binding| binding.predicate().is_some()));
}
