import { useCallback, useEffect, useState } from "react";
import {
  Alert,
  Button,
  EmptyState,
  FormField,
  Input,
  Panel,
  Pill,
  Spinner,
} from "../../../components/kit";
import { rpc, RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * Parallel agents — S32's bench CRUD, built around the one rule this page
 * exists to test: the cap is a number on the component, and the app reaches it
 * through the daemon's own component.list / component.update door (P34: the
 * socket is the primary interface; this page is the app consuming it, never a
 * second door).
 *
 * # What this page does and does not do
 *
 *   - It reads the cap through `component.list` and sets it through
 *     `component.update`. `component.update` is a FULL update — the daemon maps
 *     the whole view back — so a cap edit re-sends every field the list
 *     returned. An empty field on the way in means "keep", the same rule the
 *     key obeys elsewhere.
 *   - It never touches a shared array or switch: it self-registers via
 *     `registerBenchPage` (the "careful with other agents" rule, AGENTS.md §3).
 *   - The three honest states: `unwired` (the method answered -32601, the
 *     daemon has not wired this slice yet), `failed` (a real error), and the
 *     loaded view. A page that cannot show *unknown* would show a confident
 *     wrong number.
 *
 * # Say what it does not do
 *
 * The cap is a configuration. It is NOT a proof that any unit is running
 * concurrently — that is the loop's own runtime (the S31 event stream shows
 * concurrently-running positions). This page sets the ceiling and reads it
 * back; it does not run a loop.
 */

type ComponentCap = {
  id: string;
  name: string;
  slug: string;
  root: string;
  maxParallelAgents: number;
  shortDescription?: string;
  description?: string;
  defaultBranch?: string;
  branchPrefix?: string;
  remote?: string;
  remoteKey?: string;
  repoKey?: string;
  owner?: string;
  icon: string;
  tags: string[];
  createdAt: string;
  updatedAt: string;
};

type ComponentsListResult = { components: ComponentCap[] };

export function ParallelAgentsPage() {
  const [state, setState] = useState<ComponentsListResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [unwired, setUnwired] = useState(false);
  const [busy, setBusy] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [cap, setCap] = useState("");

  const refresh = useCallback(async () => {
    try {
      setState(await rpc<ComponentsListResult>("component.list"));
      setUnwired(false);
      setError(null);
    } catch (e) {
      const err = e as RpcError;
      // -32601 is the honest "unwired" state, not a crash: the daemon has not
      // wired this slice yet, and a page that treats that as a failure would
      // hide the very fact that the capability is not live.
      if (err?.code === -32601) {
        setUnwired(true);
        setState(null);
        setError(null);
      } else {
        setUnwired(false);
        setError(err);
      }
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const startEdit = (c: ComponentCap) => {
    setEditId(c.id);
    setCap(String(c.maxParallelAgents || 1));
  };

  const save = async (c: ComponentCap) => {
    setBusy(true);
    try {
      // component.update is a full update: re-send every field the list
      // returned, with the new cap. A blank cap keeps the existing one.
      const params: Record<string, unknown> = {
        id: c.id,
        name: c.name,
        slug: c.slug,
        remote: c.remote ?? "",
        remoteKey: c.remoteKey ?? "",
        repoKey: c.repoKey ?? "",
        owner: c.owner ?? "",
        icon: c.icon ?? "",
        tags: c.tags ?? [],
        shortDescription: c.shortDescription ?? "",
        description: c.description ?? "",
        defaultBranch: c.defaultBranch ?? "",
        branchPrefix: c.branchPrefix ?? "",
      };
      const n = Number(cap.trim());
      if (Number.isFinite(n) && n > 0) {
        params.maxParallelAgents = n;
      }
      await rpc("component.update", params);
      setEditId(null);
      await refresh();
    } catch (e) {
      setError(e as RpcError);
    } finally {
      setBusy(false);
    }
  };

  if (unwired) {
    return (
      <EmptyState
        title="Not connected"
        subtitle="The daemon did not answer component.list — the cap is not wired yet (unwired), not a failure."
        data-testid="unwired"
      />
    );
  }

  if (!state && !error) {
    return <Spinner />;
  }

  return (
    <div className="space-y-4">
      {error && <ErrorPanel error={error} onRetry={() => void refresh()} />}

      <Alert
        tone="slate"
        title="The cap is a ceiling on ready units, not a proof of concurrency"
        description="maxParallelAgents bounds how many ready units the daemon dispatches at once; 1 is serial (byte-identical to before S32). This page sets and reads it through the daemon's component door. It does not run a loop — the S31 event stream shows concurrently-running positions, a different question."
        data-testid="parallel-note"
      />

      {state && state.components.length === 0 && (
        <EmptyState
          title="No components"
          subtitle="Add a component (component.create) to set its parallel cap."
          data-testid="no-components"
        />
      )}

      {state?.components.map((c) => (
        <Panel key={c.id} data-testid={`component-${c.id}`}>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <code className="font-medium">{c.name}</code>
                <Pill tone={c.maxParallelAgents > 1 ? "violet" : "slate"} size="sm" data-testid={`cap-${c.id}`}>
                  cap {c.maxParallelAgents || 1}
                </Pill>
              </div>
              <div className="mt-1 text-sm opacity-70">{c.slug}</div>
              <div className="mt-1 text-xs opacity-60">{c.root}</div>
              {editId === c.id && (
                <div className="mt-3" data-testid={`edit-${c.id}`}>
                  <FormField label="Max parallel agents" hint="1 is serial (byte-identical). Bounded 1–16; the daemon refuses a value outside the range.">
                    <Input
                      type="number"
                      min={1}
                      max={16}
                      value={cap}
                      onChange={(e) => setCap(e.target.value)}
                      data-testid={`cap-input-${c.id}`}
                    />
                  </FormField>
                  <div className="flex gap-2">
                    <Button size="sm" loading={busy} onClick={() => void save(c)} data-testid={`save-${c.id}`}>
                      Save
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => setEditId(null)}>
                      Cancel
                    </Button>
                  </div>
                </div>
              )}
            </div>
            {editId !== c.id && (
              <Button size="sm" variant="outline" disabled={busy} onClick={() => startEdit(c)} data-testid={`edit-${c.id}`}>
                Set cap
              </Button>
            )}
          </div>
        </Panel>
      ))}
    </div>
  );
}

// The page self-registers: no shared array, import list, or switch is touched.
// See benchRegistry.ts and AGENTS.md §3 "Adding a bench page".
registerBenchPage({
  id: "parallel-agents",
  label: "Parallel Agents",
  path: "/bench/parallel-agents",
  order: 900,
  needsConnection: true,
  render: () => <ParallelAgentsPage />,
});
