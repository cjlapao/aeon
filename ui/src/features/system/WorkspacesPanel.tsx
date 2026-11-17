import { useCallback, useEffect, useState } from "react";
import { Button, Input, Panel, Pill } from "../../components/kit";
import { toRpcError, workspaceActive, workspaceAdd, workspaceList, workspaceRemove, workspaceSelect, type Workspace } from "../../lib/bridge";
import { useRpcAction } from "../../lib/useRpcAction";
import { useTypeToConfirm } from "../../lib/useConfirm";

/** Label for a workspace's target: nothing for `Local` (its name says it
 * all), `host:port` for a remote one — the same shape `rpc::Target`'s own
 * `Display` renders on the Rust side, so the two never drift. */
function targetLabel(workspace: Workspace): string {
  return workspace.kind === "remote" ? `${workspace.host}:${workspace.port}` : "this machine's bundled daemon";
}

/**
 * Switching workspaces is a hard reload (S41): nothing here tears down
 * `LiveController` or any other React state itself — `workspace_select`
 * marks the new workspace active on the Rust side, and the reload is what
 * makes "the whole app context resets" exact rather than approximated.
 */
export function WorkspacesPanel() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [activeId, setActiveId] = useState<string>("local");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [actingOn, setActingOn] = useState<string | null>(null);
  const rowAction = useRpcAction();
  const addAction = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [name, setName] = useState("");
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [formError, setFormError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const [list, active] = await Promise.all([workspaceList(), workspaceActive()]);
      setWorkspaces(list);
      setActiveId(active.id);
      setLoadError("");
    } catch (cause) {
      setLoadError(toRpcError(cause).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  async function switchTo(id: string) {
    if (id === activeId || rowAction.busy) return;
    setActingOn(id);
    // No successTitle: the page reloads immediately on success, so a toast
    // would never be seen — only the failure path needs one.
    const result = await rowAction.run(() => workspaceSelect(id));
    if (result !== undefined) {
      window.location.reload();
      return;
    }
    setActingOn(null);
  }

  async function remove(workspace: Workspace) {
    if (rowAction.busy) return;
    const confirmed = await confirm({
      title: `Remove ${workspace.name}?`,
      description: "This only removes it from this app's list of workspaces — it never touches whatever the workspace itself was pointed at.",
      confirmValue: workspace.name,
      confirmValueLabel: "workspace name",
      confirmLabel: "Remove",
    });
    if (!confirmed) return;
    setActingOn(workspace.id);
    await rowAction.run(() => workspaceRemove(workspace.id), { successTitle: `Removed ${workspace.name}` });
    await refresh();
    setActingOn(null);
  }

  async function add() {
    const trimmedName = name.trim();
    const trimmedHost = host.trim();
    const parsedPort = Number(port);
    if (!trimmedName || !trimmedHost || !Number.isInteger(parsedPort) || parsedPort <= 0 || parsedPort > 65535) {
      setFormError("A workspace needs a name, a host, and a port between 1 and 65535.");
      return;
    }
    setFormError("");
    const result = await addAction.run(() => workspaceAdd(trimmedName, trimmedHost, parsedPort), { successTitle: `Added ${trimmedName}` });
    if (result !== undefined) {
      setName(""); setHost(""); setPort("");
      await refresh();
    }
  }

  return <Panel>
    {modal}
    <h2 className="text-lg font-semibold">Workspaces</h2>
    <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">
      Which daemon this app talks to. Switching reloads the app against the one you pick.
    </p>
    {loadError && <p role="alert" className="mt-2 text-sm text-red-700 dark:text-red-300">{loadError}</p>}
    {loading ? <p className="mt-3 text-sm text-slate-500">Loading…</p> : (
      <ul className="mt-3 space-y-2">
        {workspaces.map((workspace) => (
          <li key={workspace.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-white/10 p-3 text-sm">
            <div className="min-w-0">
              <span className="font-medium">{workspace.name}</span>
              {workspace.id === activeId && <Pill tone="emerald" size="sm" className="ml-2">active</Pill>}
              <p className="truncate text-xs text-slate-500 dark:text-slate-400">{targetLabel(workspace)}</p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {workspace.id !== activeId && (
                <Button size="sm" disabled={rowAction.busy} onClick={() => void switchTo(workspace.id)}>
                  {actingOn === workspace.id ? "Switching…" : "Switch to this"}
                </Button>
              )}
              {workspace.kind === "remote" && (
                <Button size="sm" variant="soft" disabled={rowAction.busy} aria-label={`Remove ${workspace.name}`} onClick={() => void remove(workspace)}>
                  {actingOn === workspace.id ? "Removing…" : "Remove"}
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
    )}
    <div className="mt-4 border-t border-white/10 pt-4">
      <p className="text-sm font-medium">Add a remote workspace</p>
      <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">
        Nothing answers a remote workspace yet — the daemon's network listener is its own, separate, deferred piece of work.
        Saving one here is safe; connecting to it will honestly report that it's unreachable.
      </p>
      <div className="mt-2 flex flex-wrap items-end gap-2">
        <label className="block text-sm">Name
          <Input aria-label="Workspace name" value={name} onChange={(event) => setName(event.target.value)} placeholder="Dev box" />
        </label>
        <label className="block text-sm">Host
          <Input aria-label="Workspace host" value={host} onChange={(event) => setHost(event.target.value)} placeholder="192.168.1.50" />
        </label>
        <label className="block text-sm">Port
          <Input aria-label="Workspace port" value={port} onChange={(event) => setPort(event.target.value)} placeholder="7777" inputMode="numeric" />
        </label>
        <Button size="sm" disabled={addAction.busy} onClick={() => void add()}>{addAction.busy ? "Adding…" : "Add"}</Button>
      </div>
      {(formError || addAction.error) && <p role="alert" className="mt-2 text-sm text-red-700 dark:text-red-300">{formError || addAction.error}</p>}
    </div>
  </Panel>;
}
