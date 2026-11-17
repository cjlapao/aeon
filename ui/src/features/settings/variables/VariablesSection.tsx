import { useMemo, useState } from "react";
import { Alert, Button, ConfirmModal, ContentActions, MultiToggle, SearchBar, Tabs } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { errorSentence, toRpcError } from "../../../lib/bridge";
import type { LiveController, LiveState } from "../../../lib/live";
import { VariableForm } from "./VariableForm";
import { VariablesHistory } from "./VariablesHistory";
import { VariablesTable } from "./VariablesTable";
import { deleteVariable, useVariableChanges, useVariables, type VaultVariable } from "./variablesRpc";

/**
 * Variables & secrets — the values the app holds for processes and agents.
 *
 * Three facts the page keeps in front of the person: a secret is never shown
 * back (its row says so), a process gets a variable only where something NAMES it
 * (an MCP server's env list, an environment's secret reference), and an agent
 * sees a variable only when the switch on its row says so — off by default.
 *
 * The frame is the Agents page's (docs/aeon/UI-DESIGN-LANGUAGE.md): a header with the
 * one primary verb, tabs, an action bar, and a table that fills the height. Add and
 * edit open in a side panel; delete asks first.
 */
export interface VariablesSectionProps {
  controller: LiveController;
  live: LiveState;
  canWrite?: boolean;
}

type Editing = { kind: "new" } | { kind: "edit"; variable: VaultVariable } | undefined;
type Kind = "all" | "secret" | "plain" | "agents";

const matches = (v: VaultVariable, q: string, kind: Kind): boolean => {
  if (kind === "secret" && !v.secret) return false;
  if (kind === "plain" && v.secret) return false;
  if (kind === "agents" && !v.agentVisible) return false;
  if (q === "") return true;
  return [v.name, v.description ?? "", v.projectName ?? v.project ?? ""].some((s) => s.toLowerCase().includes(q));
};

export function VariablesSection({ controller, live, canWrite = live.state === "current" }: VariablesSectionProps) {
  const variables = useVariables(controller, live);
  const history = useVariableChanges("", controller, live);
  const [view, setView] = useState<"variables" | "history">("variables");
  const [q, setQ] = useState("");
  const [kind, setKind] = useState<Kind>("all");
  const [editing, setEditing] = useState<Editing>(undefined);
  const [asking, setAsking] = useState<VaultVariable | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);

  const all = variables.data ?? [];
  const rows = useMemo(() => all.filter((v) => matches(v, q.trim().toLowerCase(), kind)), [all, q, kind]);

  async function remove(variable: VaultVariable) {
    setAsking(undefined);
    setError(undefined);
    try {
      await deleteVariable(variable.name, variable.project);
      variables.refetch();
      history.refetch();
    } catch (cause) {
      setError(errorSentence(toRpcError(cause)));
    }
  }

  const saved = () => {
    setEditing(undefined);
    variables.refetch();
    history.refetch();
  };

  const reason = canWrite ? undefined : "Reconnecting — changes are off until the daemon is current.";

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="variables-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="variables-header"
        actions={<Button size="sm" disabled={!canWrite} tooltip={reason} onClick={() => setEditing({ kind: "new" })} data-testid="variable-add">Add variable</Button>}
      >
        <div>
          <h1 className="text-3xl font-semibold">Variables &amp; secrets</h1>
          <p className="mt-1 text-sm opacity-70">Values are stored encrypted. A process gets a variable only where something names it; an agent reads a variable only when you allow it.</p>
        </div>
      </ContentActions>

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[{ id: "variables", label: "Variables" }, { id: "history", label: "History" }]}
          value={view}
          onChange={(id) => setView(id as "variables" | "history")}
        />
      </div>

      {(variables.error !== undefined || error !== undefined) && (
        <Alert className="shrink-0" intent="danger" title={error ?? variables.error ?? ""} data-testid="variables-error" />
      )}

      {view === "history" ? (
        <div className="min-h-0 flex-1 overflow-y-auto pr-5">
          <VariablesHistory changes={history.data} />
        </div>
      ) : (
        <>
          <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="variables-action-bar">
            <label className="block w-full max-w-md" data-testid="variables-search">
              <span className="sr-only">Search variables</span>
              <SearchBar placeholder="Search by name, description or project" debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ("")} />
            </label>
            <MultiToggle
              size="sm"
              aria-label="Show"
              options={[
                { value: "all", label: "All" },
                { value: "secret", label: "Secrets" },
                { value: "plain", label: "Plain" },
                { value: "agents", label: "Agents can read" },
              ]}
              value={kind}
              onChange={(next) => setKind(next as Kind)}
            />
          </div>
          <VariablesTable
            rows={rows}
            total={rows.length}
            loading={variables.loading && variables.data === undefined}
            filtered={q.trim() !== "" || kind !== "all"}
            canWrite={canWrite}
            resetKey={`${q}|${kind}|${all.length}`}
            onEdit={(v) => canWrite && setEditing({ kind: "edit", variable: v })}
            onDelete={setAsking}
          />
        </>
      )}

      {editing !== undefined && (
        <OverlaySidePanel
          title={editing.kind === "edit" ? `Edit ${editing.variable.name}` : "Add a variable"}
          titleClassName="text-xl font-semibold"
          variant="simple"
          width={520}
          onClose={() => setEditing(undefined)}
        >
          <VariableForm
            key={editing.kind === "edit" ? variableId(editing.variable) : "new"}
            editing={editing.kind === "edit" ? editing.variable : undefined}
            controller={controller}
            live={live}
            onSaved={saved}
            onCancel={() => setEditing(undefined)}
          />
        </OverlaySidePanel>
      )}

      {asking !== undefined && (
        <ConfirmModal
          isOpen
          title={`Delete ${asking.name}?`}
          description={`${asking.project ? `Only the copy for ${asking.projectName ?? asking.project}` : "The global copy"} is removed, and what it held cannot be read back. Anything that names it will find nothing.`}
          confirmLabel="Delete"
          cancelLabel="Keep it"
          onClose={() => setAsking(undefined)}
          onConfirm={() => void remove(asking)}
          data-testid="variable-delete-ask"
        />
      )}
    </div>
  );
}

const variableId = (v: VaultVariable): string => `${v.name}/${v.project ?? ""}`;
