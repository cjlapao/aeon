import { useState } from "react";
import { Alert, Button, FormField, Input, Select, Toggle } from "../../../components/kit";
import { errorSentence, toRpcError } from "../../../lib/bridge";
import type { LiveController, LiveState } from "../../../lib/live";
import { useComponentRoots } from "../../projects/useComponentRoots";
import { setVariable, type VariableWrite, type VaultVariable } from "./variablesRpc";

/**
 * Add or edit one variable. The value field is write-only: an edit starts EMPTY
 * (empty keeps the stored value, which this page cannot read back for a secret),
 * and a new variable is a secret hidden from agents until the person says
 * otherwise. Agent access is its own explicit box with a plain-words warning,
 * because it is the one switch that lets a model read the value.
 */
export interface VariableFormProps {
  /** Present = edit. The name and scope are the row's own and cannot change. */
  editing?: VaultVariable;
  controller: LiveController;
  live: LiveState;
  onSaved: () => void;
  onCancel: () => void;
}

export function VariableForm({ editing, controller, live, onSaved, onCancel }: VariableFormProps) {
  const roots = useComponentRoots(controller, live);
  const [name, setName] = useState(editing?.name ?? "");
  const [project, setProject] = useState(editing?.project ?? "");
  const [description, setDescription] = useState(editing?.description ?? "");
  const [value, setValue] = useState("");
  const [secret, setSecret] = useState(editing?.secret ?? true);
  const [agentVisible, setAgentVisible] = useState(editing?.agentVisible ?? false);
  // A new variable's agent box follows "secret" until the person touches it — the daemon's own default.
  const [agentTouched, setAgentTouched] = useState(editing !== undefined);
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);

  const ready = name.trim() !== "" && (editing !== undefined || value !== "");

  async function save() {
    setBusy(true);
    setRefusal(undefined);
    const write: VariableWrite = { name: name.trim(), secret, agentVisible, description };
    if (project) write.project = project;
    if (value !== "") write.value = value;
    try {
      await setVariable(write);
      onSaved();
    } catch (cause) {
      setRefusal(errorSentence(toRpcError(cause)));
    } finally {
      setBusy(false);
    }
  }

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

  return (
    <div className="space-y-8" data-testid="variable-form">
      {refusal !== undefined && <div data-testid="variable-refusal"><Alert intent="danger" title={refusal} /></div>}

      <section className="space-y-3">
        <h3 className={H}>What is it called, and where does it apply?</h3>
        <FormField label="Name">
          <Input variant="ghost" aria-label="variable name" value={name} disabled={editing !== undefined || busy} placeholder="GITHUB_TOKEN" onChange={(e) => setName(e.target.value)} />
        </FormField>
        <FormField label="Applies to">
          <Select variant="ghost" aria-label="variable scope" value={project} disabled={editing !== undefined || busy} onChange={(e) => setProject(e.target.value)}>
            <option value="">every project (global)</option>
            {roots.roots.map((root) => <option key={root.id} value={root.id}>only {root.name}</option>)}
          </Select>
        </FormField>
        <FormField label="What it is for">
          <Input variant="ghost" aria-label="variable description" value={description} disabled={busy} onChange={(e) => setDescription(e.target.value)} />
        </FormField>
      </section>

      <section className="space-y-3">
        <h3 className={H}>What does it hold?</h3>
        <p className="text-sm opacity-80">The value is only ever sent — this page cannot read a secret back.</p>
        <FormField label={editing ? "New value (leave empty to keep the stored one)" : "Value"}>
          <Input
            variant="ghost"
            aria-label="variable value"
            type={secret ? "password" : "text"}
            autoComplete="off"
            value={value}
            disabled={busy}
            onChange={(e) => setValue(e.target.value)}
          />
        </FormField>
        <Toggle
          size="sm"
          aria-label="this is a secret"
          checked={secret}
          disabled={busy}
          label="This is a secret"
          description="Its value is never shown again after you save it."
          onChange={(e) => { setSecret(e.target.checked); if (!agentTouched) setAgentVisible(!e.target.checked); }}
        />
      </section>

      <section className="space-y-3">
        <h3 className={H}>Who can read it?</h3>
        <Toggle
          size="sm"
          aria-label="agents can read this"
          checked={agentVisible}
          disabled={busy}
          label="Agents can read this"
          description="With the env_get tool. Off by default for a secret; every read is recorded under the agent's name."
          onChange={(e) => { setAgentTouched(true); setAgentVisible(e.target.checked); }}
        />
        {secret && agentVisible && (
          <div data-testid="agent-secret-warning"><Alert intent="warning" title="Agents will be able to read this secret." /></div>
        )}
      </section>

      <div className="flex gap-2">
        <Button size="sm" disabled={!ready || busy} onClick={() => void save()} data-testid="variable-save">Save</Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={onCancel}>Cancel</Button>
      </div>
    </div>
  );
}
