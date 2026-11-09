//! Model discovery state shared by chat and settings pickers.
use std::{collections::BTreeMap, time::{Duration, Instant}};
use super::{BackendKind, ModelOption};

const FRESH_FOR: Duration = Duration::from_secs(5 * 60);

#[derive(Clone, Debug, Default, PartialEq)]
pub(crate) enum ModelStatus { #[default] Idle, Loading, Ready, Error(String) }

#[derive(Default)]
struct Entry {
    choices: Vec<ModelOption>,
    status: ModelStatus,
    refreshed_at: Option<Instant>,
    generation: u64,
}

#[derive(Default)]
pub(crate) struct ModelCache {
    entries: BTreeMap<BackendKind, Entry>,
    generation: u64,
}

impl ModelCache {
    pub(crate) fn choices(&self, kind: BackendKind) -> &[ModelOption] {
        self.entries.get(&kind).map_or(&[], |entry| entry.choices.as_slice())
    }

    pub(crate) fn status(&self, kind: BackendKind) -> ModelStatus {
        self.entries.get(&kind).map_or(ModelStatus::Idle, |entry| entry.status.clone())
    }

    /// Keep existing choices visible and allow only one discovery per backend.
    pub(crate) fn begin_refresh(&mut self, kind: BackendKind) -> Option<u64> {
        let entry = self.entries.entry(kind).or_default();
        if entry.status == ModelStatus::Loading
            || entry.refreshed_at.is_some_and(|at| at.elapsed() < FRESH_FOR) {
            return None;
        }
        self.generation += 1;
        entry.generation = self.generation;
        entry.status = ModelStatus::Loading;
        Some(entry.generation)
    }

    /// A failed refresh never removes a usable list. Ignore invalidated requests.
    pub(crate) fn finish(&mut self, kind: BackendKind, generation: u64,
        result: Result<Vec<ModelOption>, String>) -> bool {
        let Some(entry) = self.entries.get_mut(&kind).filter(|entry| entry.generation == generation) else { return false; };
        match result {
            Ok(choices) => {
                entry.choices = choices;
                entry.refreshed_at = Some(Instant::now());
                entry.status = ModelStatus::Ready;
            }
            Err(error) => entry.status = ModelStatus::Error(error),
        }
        true
    }

    pub(crate) fn invalidate(&mut self, kind: BackendKind) {
        self.entries.remove(&kind);
    }
}
