//! Native page-history archive. One mutex, one worker, durable pending before snapshot promotion.
use std::{
    collections::{HashMap, HashSet},
    fs,
    io::{self, Write},
    path::{Component, Path, PathBuf},
    sync::{
        atomic::{AtomicU64, Ordering},
        Mutex, Once, OnceLock,
    },
    time::Duration,
};
use serde::{Deserialize, Serialize};
use super::super::{
    hash::{fingerprint_bytes, sha256_hex},
    time::{format_version_time, now_nanos, now_seconds},
    types::{HistoryVersion, SnapshotMeta},
};

include!("storage_io.rs");
include!("storage_index.rs");
include!("storage_snap.rs");

const QUIET_SECS: u64 = 30;
const CONTINUOUS_SECS: u64 = 300;

#[derive(Clone)]
struct Pending {
    content: String,
    sha: String,
    first_at: u64,
    last_at: u64,
}

struct Store {
    index: Option<PathIndex>,
    pending: HashMap<String, Pending>,
    reported: HashSet<String>,
}

fn store() -> &'static Mutex<Store> {
    static STORE: OnceLock<Mutex<Store>> = OnceLock::new();
    STORE.get_or_init(|| Mutex::new(Store { index: None, pending: HashMap::new(), reported: HashSet::new() }))
}

fn lock() -> Result<std::sync::MutexGuard<'static, Store>, String> {
    store().lock().map_err(|error| format!("history lock poisoned: {error}"))
}

fn boot(store: &mut Store) -> Result<(), String> {
    if store.index.is_none() {
        store.index = Some(load_index().map_err(|error| error.to_string())?);
    }
    Ok(())
}

fn report(store: &mut Store, key: &str, error: &str) {
    if store.reported.insert(key.to_string()) {
        crate::log::error(&format!("history pending for {key} is unreadable: {error}"));
    }
}

fn hydrate_pending(store: &mut Store, key: &str, page_id: &str) -> Result<(), String> {
    if store.pending.contains_key(key) { return Ok(()); }
    match read_pending(page_id) {
        Ok(None) => Ok(()),
        Ok(Some(disk)) => {
            store.pending.insert(key.to_string(), Pending {
                content: disk.content, sha: disk.sha, first_at: disk.first_at, last_at: disk.last_at,
            });
            Ok(())
        }
        Err(error) => {
            report(store, key, &error.to_string());
            Ok(())
        }
    }
}

fn hydrate_known(store: &mut Store) {
    let pairs: Vec<(String, String)> = store.index.as_ref()
        .map(|index| index.pages.iter().map(|(key, id)| (key.clone(), id.clone())).collect())
        .unwrap_or_default();
    for (key, id) in pairs {
        if !safe_component(&id) {
            report(store, &key, "invalid history page id");
            continue;
        }
        let _ = hydrate_pending(store, &key, &id);
    }
}

fn promote(store: &mut Store, path: &Path, force: bool) -> Result<(), String> {
    let key = path_key(path).map_err(|error| error.to_string())?;
    let page_id = get_or_create(store, path)?;
    hydrate_pending(store, &key, &page_id)?;
    let Some(pending) = store.pending.get(&key).map(|item| (item.content.clone(), item.sha.clone(), item.first_at, item.last_at)) else {
        return Ok(());
    };
    let now = now_seconds();
    let due = force || now.saturating_sub(pending.3) >= QUIET_SECS || now.saturating_sub(pending.2) >= CONTINUOUS_SECS;
    if !due { return Ok(()); }
    commit_snapshot(&page_id, &pending.0, &pending.1).map_err(|error| error.to_string())?;
    store.pending.remove(&key);
    clear_pending(&page_id).map_err(|error| error.to_string())?;
    Ok(())
}

fn stage(path: &Path, content: &str) -> Result<(), String> {
    let mut store = lock()?;
    boot(&mut store)?;
    let key = path_key(path).map_err(|error| error.to_string())?;
    let page_id = get_or_create(&mut store, path)?;
    hydrate_pending(&mut store, &key, &page_id)?;
    let sha = sha256_hex(content.as_bytes());
    let metas = load_metas(&page_id).map_err(|error| error.to_string())?;
    if metas.last().is_some_and(|meta| meta.sha256 == sha) {
        store.pending.remove(&key);
        return clear_pending(&page_id).map_err(|error| error.to_string());
    }
    if store.pending.get(&key).is_some_and(|item| item.sha == sha) {
        return Ok(());
    }
    let now = now_seconds();
    let first_at = store.pending.get(&key).map(|item| item.first_at).unwrap_or(now);
    write_pending(&page_id, content, &sha, first_at, now).map_err(|error| error.to_string())?;
    store.pending.insert(key, Pending { content: content.to_string(), sha, first_at, last_at: now });
    ensure_worker();
    Ok(())
}

fn ensure_worker() {
    static START: Once = Once::new();
    START.call_once(|| {
        if let Err(error) = std::thread::Builder::new().name("block-history".into()).spawn(|| {
            loop {
                std::thread::sleep(Duration::from_secs(1));
                match lock() {
                    Ok(mut store) => {
                        if let Err(error) = boot(&mut store) {
                            crate::log::warn(&format!("history archive: {error}"));
                            continue;
                        }
                        hydrate_known(&mut store);
                        let now = now_seconds();
                        let due: Vec<String> = store.pending.iter().filter(|(_, item)| {
                            now.saturating_sub(item.last_at) >= QUIET_SECS || now.saturating_sub(item.first_at) >= CONTINUOUS_SECS
                        }).map(|(key, _)| key.clone()).collect();
                        for key in due {
                            let path = std::path::PathBuf::from(&key);
                            if let Err(error) = promote(&mut store, &path, false) {
                                crate::log::warn(&format!("history archive {key} failed: {error}"));
                            }
                        }
                    }
                    Err(error) => crate::log::error(&format!("history archive: {error}")),
                }
            }
        }) {
            crate::log::error(&format!("history worker failed to start: {error}"));
        }
    });
}

include!("storage_api.rs");
