//! Which daemon this app talks to.
//!
//! A **workspace** is a daemon: `Local`, always present, pointing at the
//! endpoint `endpoint.rs` resolves — or a saved **Remote** entry, a name plus
//! a host and port. Exactly one is active at a time; `connect` and
//! `endpoint_path` in `main.rs` both resolve against whichever that is, so
//! neither needed to change shape to become workspace-aware.
//!
//! Saved remote entries persist as one JSON file in the app's own config
//! directory — never in the web layer's `localStorage` (§4c in `AGENTS.md`):
//! this is where a connection credential will have to live once the deferred
//! remote-listener slice (`docs/aeon/S41-workspaces/design.md`) gives remote
//! workspaces something to authenticate to. `credential` is reserved on
//! `SavedWorkspace` for exactly that, and the rule that shapes it even while
//! unused: it round-trips through `save`/`load` but the `Workspace` type the
//! web layer actually sees has no field that could carry it back out.

use std::fs;
use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use crate::rpc::Target;

pub const LOCAL_ID: &str = "local";

#[derive(Serialize, Deserialize, Debug, Clone, Copy, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub enum Kind {
    Local,
    Remote,
}

/// What the web layer sees. Never a credential field — see this module's own
/// doc comment.
#[derive(Serialize, Debug, Clone, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct Workspace {
    pub id: String,
    pub name: String,
    pub kind: Kind,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub host: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub port: Option<u16>,
}

/// A saved remote entry, as it lives on disk. `Local` is never one of
/// these — it is synthesized in `list`/`active`, always present, always
/// first, so it can never be deleted, duplicated, or omitted by a hand-edited
/// file.
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
struct SavedWorkspace {
    id: String,
    name: String,
    host: String,
    port: u16,
    /// Reserved; see this module's doc comment. Nothing sets this today.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    credential: Option<String>,
}

#[derive(Serialize, Deserialize, Debug, Clone, Default)]
#[serde(rename_all = "camelCase")]
struct Saved {
    #[serde(default)]
    workspaces: Vec<SavedWorkspace>,
    #[serde(default = "default_active_id")]
    active_id: String,
}

fn default_active_id() -> String {
    LOCAL_ID.to_string()
}

/// The saved set, plus the file it persists to.
///
/// A missing or unreadable file loads as empty — `Local` only, active — the
/// same state a fresh install starts in. A corrupt file is treated the same
/// way rather than refused: a worse day for whoever edited it by hand than
/// losing a workspace list is an app that will not start.
pub struct WorkspaceStore {
    path: PathBuf,
    saved: Saved,
}

impl WorkspaceStore {
    pub fn load(path: PathBuf) -> Self {
        let saved = fs::read_to_string(&path)
            .ok()
            .and_then(|text| serde_json::from_str::<Saved>(&text).ok())
            .unwrap_or_default();
        WorkspaceStore { path, saved }
    }

    fn save(&self) -> Result<(), String> {
        let text = serde_json::to_string_pretty(&self.saved).map_err(|e| e.to_string())?;
        if let Some(parent) = self.path.parent() {
            fs::create_dir_all(parent)
                .map_err(|e| format!("create {}: {e}", parent.display()))?;
        }
        fs::write(&self.path, text).map_err(|e| format!("write {}: {e}", self.path.display()))
    }

    fn local() -> Workspace {
        Workspace {
            id: LOCAL_ID.to_string(),
            name: "Local".to_string(),
            kind: Kind::Local,
            host: None,
            port: None,
        }
    }

    /// `Local` first, always, then every saved remote workspace.
    pub fn list(&self) -> Vec<Workspace> {
        let mut out = vec![Self::local()];
        out.extend(self.saved.workspaces.iter().map(|w| Workspace {
            id: w.id.clone(),
            name: w.name.clone(),
            kind: Kind::Remote,
            host: Some(w.host.clone()),
            port: Some(w.port),
        }));
        out
    }

    /// The active workspace. An `activeId` naming nothing that exists (a
    /// removed workspace, a hand-edited file) falls back to `Local` rather
    /// than erroring — the one daemon this app can always reach.
    pub fn active(&self) -> Workspace {
        self.list()
            .into_iter()
            .find(|w| w.id == self.saved.active_id)
            .unwrap_or_else(Self::local)
    }

    /// Where the active workspace resolves to, for `Session::connect`.
    pub fn active_target(&self, local_endpoint: PathBuf) -> Target {
        let active = self.active();
        match active.kind {
            Kind::Local => Target::Local(local_endpoint),
            Kind::Remote => Target::Remote {
                host: active.host.unwrap_or_default(),
                port: active.port.unwrap_or_default(),
            },
        }
    }

    pub fn add(&mut self, name: String, host: String, port: u16) -> Result<Workspace, String> {
        let name = name.trim().to_string();
        if name.is_empty() {
            return Err("a workspace needs a name".into());
        }
        let host = host.trim().to_string();
        if host.is_empty() {
            return Err("a workspace needs a host".into());
        }
        if port == 0 {
            return Err("a workspace needs a port".into());
        }
        let id = next_id(&self.saved.workspaces);
        self.saved.workspaces.push(SavedWorkspace {
            id: id.clone(),
            name,
            host,
            port,
            credential: None,
        });
        self.save()?;
        Ok(self
            .list()
            .into_iter()
            .find(|w| w.id == id)
            .expect("just inserted"))
    }

    /// Removing the active workspace falls back to `Local` — stated as a
    /// deliberate choice (left open in the design note): a workspace that no
    /// longer exists cannot stay "active" with nothing to reconnect to, and
    /// refusing the removal instead would make deleting a stale entry
    /// depend on first switching away from it for no reason the user can see.
    pub fn remove(&mut self, id: &str) -> Result<(), String> {
        if id == LOCAL_ID {
            return Err("the Local workspace cannot be removed".into());
        }
        let before = self.saved.workspaces.len();
        self.saved.workspaces.retain(|w| w.id != id);
        if self.saved.workspaces.len() == before {
            return Err(format!("no workspace {id}"));
        }
        if self.saved.active_id == id {
            self.saved.active_id = LOCAL_ID.to_string();
        }
        self.save()
    }

    pub fn select(&mut self, id: &str) -> Result<Workspace, String> {
        if id != LOCAL_ID && !self.saved.workspaces.iter().any(|w| w.id == id) {
            return Err(format!("no workspace {id}"));
        }
        self.saved.active_id = id.to_string();
        self.save()?;
        Ok(self.active())
    }
}

/// `remote-N`, smallest N not already in use — stable, readable ids without
/// pulling in a UUID dependency for a list that stays small by construction.
fn next_id(existing: &[SavedWorkspace]) -> String {
    let mut n = 1u64;
    loop {
        let candidate = format!("remote-{n}");
        if !existing.iter().any(|w| w.id == candidate) {
            return candidate;
        }
        n += 1;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_path() -> PathBuf {
        std::env::temp_dir().join(format!(
            "aeon-workspaces-test-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ))
    }

    #[test]
    fn starts_with_only_local_active() {
        let store = WorkspaceStore::load(temp_path());
        assert_eq!(store.list().len(), 1);
        assert_eq!(store.active().id, LOCAL_ID);
        assert_eq!(store.active().kind, Kind::Local);
    }

    #[test]
    fn adds_lists_selects_and_removes_a_remote_workspace() {
        let path = temp_path();
        let mut store = WorkspaceStore::load(path.clone());
        let added = store
            .add("Dev box".into(), "192.168.1.50".into(), 7777)
            .unwrap();
        assert_eq!(added.kind, Kind::Remote);
        assert_eq!(store.list().len(), 2);

        store.select(&added.id).unwrap();
        assert_eq!(store.active().id, added.id);
        let target = store.active_target(PathBuf::from("/tmp/local.sock"));
        match target {
            Target::Remote { host, port } => {
                assert_eq!(host, "192.168.1.50");
                assert_eq!(port, 7777);
            }
            Target::Local(_) => panic!("expected a remote target"),
        }

        // Reloaded from disk, the saved entry and the active selection survive.
        let reloaded = WorkspaceStore::load(path);
        assert_eq!(reloaded.active().id, added.id);

        let mut reloaded = reloaded;
        reloaded.remove(&added.id).unwrap();
        assert_eq!(reloaded.list().len(), 1);
        // Removing the active workspace falls back to Local (stated design choice).
        assert_eq!(reloaded.active().id, LOCAL_ID);
    }

    #[test]
    fn local_cannot_be_removed() {
        let mut store = WorkspaceStore::load(temp_path());
        assert!(store.remove(LOCAL_ID).is_err());
    }

    #[test]
    fn selecting_an_unknown_id_is_refused() {
        let mut store = WorkspaceStore::load(temp_path());
        assert!(store.select("remote-does-not-exist").is_err());
        assert_eq!(store.active().id, LOCAL_ID);
    }

    #[test]
    fn a_stale_active_id_falls_back_to_local() {
        let path = temp_path();
        fs::write(&path, r#"{"workspaces":[],"activeId":"remote-gone"}"#).unwrap();
        let store = WorkspaceStore::load(path);
        assert_eq!(store.active().id, LOCAL_ID);
    }

    #[test]
    fn a_credential_round_trips_on_disk_but_never_reaches_the_workspace_view() {
        let path = temp_path();
        fs::write(
            &path,
            r#"{"workspaces":[{"id":"remote-1","name":"Dev","host":"h","port":1,"credential":"secret"}],"activeId":"local"}"#,
        )
        .unwrap();
        let mut store = WorkspaceStore::load(path.clone());
        // Loaded fine (parses `credential` without error) ...
        assert_eq!(store.list().len(), 2);
        // ... and a save roundtrips it on disk ...
        store.select(LOCAL_ID).unwrap();
        let on_disk = fs::read_to_string(&path).unwrap();
        assert!(on_disk.contains("secret"));
        // ... but it is structurally absent from the type the web layer sees.
        let view = serde_json::to_string(&store.list()[1]).unwrap();
        assert!(!view.contains("secret"));
    }
}
