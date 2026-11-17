import { useCallback, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill, Select } from "../../../components/kit";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { AgentTagPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { InspectPanel } from "./InspectPanel";
import { ToolClassEditor } from "./ToolClassEditor";
import {
  buildAddParams,
  buildUpdateParams,
  canSubmit,
  classifyUnclassified,
  halfClassifiedTools,
  mergeInspectedTools,
  suggestServerName,
  unboundEnvNames,
  envNamesOnly,
  isUrlTransport,
  type McpVocabulary,
  type ServerDraft,
  type ServerUpdateParamsWire,
} from "./mcpModel";
import type { RpcMethods } from "../../../proto/contract";

/**
 * The add/edit form (MG: the form is BUILT from `mcp.vocabulary` — every
 * select's options are the daemon's arrays; changing the daemon's vocabulary
 * changes the form with no UI edit). Env entries are NAMES only and are
 * refused client-side before any call; a daemon refusal renders
 * `Config.Validate`'s sentence VERBATIM under the form and the draft stays
 * exactly as typed (MG4's law at the form).
 */
export interface ServerFormProps {
  /** Present = edit (name is the row's, immutable); absent = add. */
  editingName?: string;
  initial: ServerDraft;
  vocabulary: McpVocabulary | undefined;
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  onCancel: () => void;
  /** Called with the server name the daemon answered for. */
  onSaved: (name: string) => void;
}

export function ServerForm({ editingName, initial, vocabulary, canWrite, controller, live, onCancel, onSaved }: ServerFormProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const [draft, setDraft] = useState<ServerDraft>(initial);
  const [envEntry, setEnvEntry] = useState("");
  const [envRefusal, setEnvRefusal] = useState<string | undefined>(undefined);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);

  // The stored variables an env name can be bound to — names only; a secret's value never arrives.
  const variables = useRpcQuery("vault.list", {} as Record<string, never>, controller, live);
  const variableNames = useMemo(() => [...new Set((variables.data?.variables ?? []).map((v) => v.name))].sort(), [variables.data]);
  const unbound = useMemo(() => unboundEnvNames(draft), [draft]);
  const decision = useMemo(() => canSubmit(draft, vocabulary, { requireTools: editingName === undefined }), [draft, vocabulary, editingName]);
  const [bulkRerun, setBulkRerun] = useState("");
  const [bulkEffect, setBulkEffect] = useState("");
  const halfDone = useMemo(() => halfClassifiedTools(draft.tools), [draft.tools]);
  const unclassifiedCount = draft.tools.filter((tool) => tool.rerun === "" && tool.effect === "").length;
  const urlBased = isUrlTransport(vocabulary, draft.transportType);

  const addEnvEntry = useCallback(() => {
    const verdict = envNamesOnly(envEntry);
    if (!verdict.ok) {
      // Refused HERE, client-side — the value never reaches the daemon.
      setEnvRefusal(t(verdict.key));
      return;
    }
    setEnvRefusal(undefined);
    setDraft((current) => (current.env.includes(verdict.name) ? current : { ...current, env: [...current.env, verdict.name] }));
    setEnvEntry("");
  }, [envEntry, t]);

  const submit = useCallback(async () => {
    if (!decision.ok) return;
    setRefusal(undefined);
    const result = editingName === undefined
      ? await action.run(() => rpcTyped("mcp.server.add", buildAddParams(draft)), { errorTitle: t("settings.mcp.form.addFailed") })
      : await action.run(
          () => rpcTyped("mcp.server.update", buildUpdateParams(editingName, draft) as unknown as RpcMethods["mcp.server.update"]["params"]),
          { errorTitle: t("settings.mcp.form.updateFailed") },
        );
    if (result) onSaved(editingName ?? draft.name);
    // On failure the daemon's sentence lands in action.error — rendered
    // verbatim below; the draft is untouched, by construction.
  }, [action, decision.ok, draft, editingName, onSaved, t]);

  const off = disabledFor(canWrite, action.busy);
  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

  return (
    <div className="space-y-8" data-testid="mcp-server-form">
      {envRefusal && <div data-testid="env-refusal"><Alert intent="warning" title={envRefusal} /></div>}
      {/* The validator's sentence, verbatim, with the draft kept — this
          Alert is the refusal; there is no second one elsewhere. */}
      {(action.error || refusal) && <div data-testid="form-refusal"><Alert intent="danger" title={action.error ?? refusal ?? ""} /></div>}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.mcp.form.qWho")}</h3>
        <FormField label={t("settings.mcp.form.name")}>
          <Input variant="ghost" aria-label={t("settings.mcp.form.name")} value={draft.name} disabled={off || editingName !== undefined}
            onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
        </FormField>
        <FormField label={t("settings.mcp.form.title")} hint={t("settings.mcp.form.titleHint")}>
          <Input variant="ghost" aria-label={t("settings.mcp.form.title")} value={draft.title} disabled={off}
            onChange={(event) => setDraft((current) => ({ ...current, title: event.target.value }))} />
        </FormField>
        <FormField label={t("settings.mcp.form.description")} hint={t("settings.mcp.form.descriptionHint")}>
          <Input variant="ghost" aria-label={t("settings.mcp.form.description")} value={draft.description} disabled={off}
            onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} />
        </FormField>
        <FormField label={t("settings.mcp.form.tier")}>
          <Select
            variant="ghost"
            aria-label={t("settings.mcp.form.tier")}
            value={draft.tier}
            disabled={off}
            onChange={(event) => setDraft((current) => ({ ...current, tier: event.target.value, agent: event.target.value === "public" ? "" : current.agent }))}
          >
            <option value="">{t("settings.mcp.form.choose")}</option>
            <option value="public">{t("settings.mcp.tier.public")}</option>
            <option value="private">{t("settings.mcp.tier.private")}</option>
          </Select>
        </FormField>
        {draft.tier === "private" && (
          <FormField label={t("settings.mcp.form.owner")}>
            <AgentTagPicker value={draft.agent} onChange={(agent) => setDraft((current) => ({ ...current, agent }))} disabled={off} controller={controller} live={live} />
          </FormField>
        )}
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.mcp.form.qRuns")}</h3>
        <FormField label={t("settings.mcp.form.transport")}>
          <Select
            variant="ghost"
            aria-label={t("settings.mcp.form.transport")}
            value={draft.transportType}
            disabled={off}
            onChange={(event) => setDraft((current) => ({ ...current, transportType: event.target.value }))}
          >
            <option value="">{t("settings.mcp.form.choose")}</option>
            {(vocabulary?.transports ?? []).map((transport) => (
              <option key={transport} value={transport}>{transport}</option>
            ))}
          </Select>
        </FormField>

        {draft.transportType !== "" && (urlBased ? (
          <FormField label={t("settings.mcp.form.url")}>
            <Input variant="ghost" aria-label={t("settings.mcp.form.url")} value={draft.url} disabled={off}
              onChange={(event) => setDraft((current) => ({ ...current, url: event.target.value }))} />
          </FormField>
        ) : (
          <>
            <FormField label={t("settings.mcp.form.command")}>
              <Input variant="ghost" aria-label={t("settings.mcp.form.command")} value={draft.command} disabled={off}
                onChange={(event) => setDraft((current) => ({ ...current, command: event.target.value }))} />
            </FormField>
            <FormField label={t("settings.mcp.form.args")}>
              <Input
                variant="ghost"
                aria-label={t("settings.mcp.form.args")}
                value={draft.args.join(" ")}
                disabled={off}
                placeholder={t("settings.mcp.form.argsHint")}
                onChange={(event) => setDraft((current) => ({ ...current, args: event.target.value === "" ? [] : event.target.value.split(/\s+/) }))}
              />
            </FormField>
          </>
        ))}
      </section>

      {draft.transportType !== "" && !urlBased && (
        <section className="space-y-3">
          <h3 className={H}>{t("settings.mcp.form.qNeeds")}</h3>
          <p className="text-sm opacity-80">{t("settings.mcp.form.envNames")}</p>
          <div className="flex flex-wrap items-center gap-2">
            <div className="min-w-48 flex-1">
              <Input variant="ghost" aria-label={t("settings.mcp.form.envEntry")} value={envEntry} disabled={off}
                placeholder={t("settings.mcp.form.envEntryHint")} onChange={(event) => setEnvEntry(event.target.value)} />
            </div>
            <Button size="sm" variant="ghost" disabled={off || envEntry === ""} onClick={addEnvEntry}>{t("settings.mcp.form.envAdd")}</Button>
          </div>
          {draft.env.length > 0 && (
            <div className="flex flex-wrap gap-2" data-testid="env-chips">
              {draft.env.map((name) => (
                <button key={name} type="button" className="cursor-pointer" disabled={off} onClick={() => setDraft((current) => dropEnvName(current, name))}>
                  <Pill tone="blue">{name} ×</Pill>
                </button>
              ))}
            </div>
          )}
          {draft.env.length > 0 && (
            <ul className="space-y-3" data-testid="env-bindings">
              {draft.env.map((name) => (
                <EnvBindingRow
                  key={name}
                  name={name}
                  draft={draft}
                  variables={variableNames}
                  disabled={off}
                  unbound={unbound.includes(name)}
                  onChange={(patch) => setDraft((current) => bindEnv(current, name, patch))}
                />
              ))}
            </ul>
          )}
        </section>
      )}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.mcp.form.qTools")}</h3>
        {draft.transportType !== "" && (
          <InspectPanel
            draft={draft}
            vocabulary={vocabulary}
            disabled={off}
            onUse={(picked, server) =>
              setDraft((current) => ({
                ...current,
                // The server's own name is only a suggestion, and only when the person has not typed one.
                name: current.name === "" ? suggestServerName(server.serverName) : current.name,
                tools: mergeInspectedTools(current.tools, picked),
              }))
            }
          />
        )}
        <div className="flex items-center justify-between">
          <p className="text-sm opacity-80">{t("settings.mcp.form.tools")}</p>
          <Button size="sm" variant="ghost" disabled={off} onClick={() => setDraft((current) => ({ ...current, tools: [...current.tools, { name: "", rerun: "", effect: "" }] }))}>
            {t("settings.mcp.form.tool.add")}
          </Button>
        </div>
        {unclassifiedCount > 1 && (
          <div className="flex flex-wrap items-center gap-2 text-xs" data-testid="bulk-classify">
            <span className="opacity-70">{t("settings.mcp.form.inspect.bulkLabel")}</span>
            <Select variant="ghost" size="md" aria-label={t("settings.mcp.form.tool.rerun")} value={bulkRerun} onChange={(event) => setBulkRerun(event.target.value)}>
              <option value="">{t("settings.mcp.form.tool.unclassifiedOption")}</option>
              {(vocabulary?.rerun ?? []).map((value) => <option key={value} value={value}>{value}</option>)}
            </Select>
            <Select variant="ghost" size="md" aria-label={t("settings.mcp.form.tool.effect")} value={bulkEffect} onChange={(event) => setBulkEffect(event.target.value)}>
              <option value="">{t("settings.mcp.form.tool.unclassifiedOption")}</option>
              {(vocabulary?.effect ?? []).map((value) => <option key={value} value={value}>{value}</option>)}
            </Select>
            <Button size="xs" variant="ghost" disabled={off || bulkRerun === "" || bulkEffect === ""} onClick={() => setDraft((current) => ({ ...current, tools: classifyUnclassified(current.tools, bulkRerun, bulkEffect) }))}>
              {t("settings.mcp.form.inspect.bulkApply")}
            </Button>
          </div>
        )}
        {draft.tools.map((tool, index) => (
          <ToolClassEditor
            key={index}
            tool={tool}
            vocabulary={vocabulary}
            disabled={off}
            onChange={(next) => setDraft((current) => ({ ...current, tools: current.tools.map((entry, at) => (at === index ? next : entry)) }))}
            onRemove={() => setDraft((current) => ({ ...current, tools: current.tools.filter((_, at) => at !== index) }))}
            incomplete={halfDone.includes(index)}
          />
        ))}
      </section>

      {decision.flags.length > 0 && (
        <div data-testid="unclassified-warning"><Alert intent="warning" title={t("settings.mcp.form.unclassifiedWarning", { count: decision.flags.length })} /></div>
      )}
      {decision.errors.length > 0 && (
        <ul className="space-y-1 text-xs text-rose-600 dark:text-rose-400" data-testid="form-errors">
          {decision.errors.map((key) => (
            <li key={key}>{t(key.split("#")[0] as string)}</li>
          ))}
          {halfDone.length > 0 && (
            // WHICH rows — the rule alone does not say, and an imported list can be long.
            <li data-testid="half-classified-list">
              {t("settings.mcp.form.tool.halfList", { names: halfDone.map((index) => draft.tools[index]?.name || `#${index + 1}`).join(", ") })}
            </li>
          )}
        </ul>
      )}

      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={!canWrite || action.busy || !decision.ok} onClick={() => void submit()} title={!canWrite ? t("settings.mcp.reconnecting") : undefined} data-testid="form-submit">
          {editingName === undefined ? t("settings.mcp.form.add") : t("settings.mcp.form.save")}
        </Button>
        <Button size="sm" variant="ghost" onClick={onCancel}>{t("settings.mcp.form.cancel")}</Button>
      </div>
    </div>
  );
}

function disabledFor(canWrite: boolean, busy: boolean): boolean {
  return !canWrite || busy;
}

/** Update params travel through the wire shim (pointer fields the
 *  generated contract flattened) — one honest cast at the edge, beside
 *  S45's `ProposalListParamsWire`. */
export type { ServerUpdateParamsWire };

/** Remove an env name and whatever it was bound to. */
function dropEnvName(draft: ServerDraft, name: string): ServerDraft {
  const { [name]: _from, ...envFrom } = draft.envFrom;
  const { [name]: _value, ...envValues } = draft.envValues;
  return { ...draft, env: draft.env.filter((entry) => entry !== name), envFrom, envValues };
}

/** Bind one env name: a stored variable OR a typed value, never both (the last choice wins). */
function bindEnv(draft: ServerDraft, name: string, patch: { variable?: string; value?: string }): ServerDraft {
  const envFrom = { ...draft.envFrom };
  const envValues = { ...draft.envValues };
  if (patch.variable !== undefined) {
    if (patch.variable === "") delete envFrom[name];
    else envFrom[name] = patch.variable;
    delete envValues[name];
  }
  if (patch.value !== undefined) {
    if (patch.value === "") delete envValues[name];
    else {
      envValues[name] = patch.value;
      delete envFrom[name];
    }
  }
  return { ...draft, envFrom, envValues };
}

/** One env name and where its value comes from. A typed value is write-only: the
 *  daemon keeps it as a hidden secret in Variables & secrets and stores a binding. */
function EnvBindingRow({ name, draft, variables, disabled, unbound, onChange }: {
  name: string;
  draft: ServerDraft;
  variables: string[];
  disabled: boolean;
  unbound: boolean;
  onChange: (patch: { variable?: string; value?: string }) => void;
}) {
  const { t } = useI18n();
  const typed = draft.envValues[name] ?? "";
  const bound = draft.envFrom[name] ?? "";
  const mode: "variable" | "value" = typed !== "" ? "value" : "variable";
  const [forceValue, setForceValue] = useState(false);
  const showValue = mode === "value" || forceValue;
  return (
    <li className="flex flex-wrap items-center gap-2 text-xs" data-testid={`env-binding-${name}`} data-unbound={unbound || undefined}>
      <code className="min-w-32 font-mono text-sm">{name}</code>
      <Select
        variant="ghost"
        size="md"
        aria-label={`${name} source`}
        value={showValue ? "__value" : bound}
        disabled={disabled}
        onChange={(event) => {
          if (event.target.value === "__value") {
            setForceValue(true);
            onChange({ variable: "" });
          } else {
            setForceValue(false);
            onChange({ variable: event.target.value, value: "" });
          }
        }}
      >
        <option value="">{t("settings.mcp.form.envBinding.pick")}</option>
        {variables.map((v) => <option key={v} value={v}>{v}</option>)}
        {bound !== "" && !variables.includes(bound) && <option value={bound}>{bound}</option>}
        <option value="__value">{t("settings.mcp.form.envBinding.value")}</option>
      </Select>
      {showValue && (
        <Input
          variant="ghost"
          type="password"
          autoComplete="off"
          aria-label={`${name} value`}
          value={typed}
          disabled={disabled}
          placeholder={t("settings.mcp.form.envBinding.valueHint")}
          onChange={(event) => onChange({ value: event.target.value })}
        />
      )}
      {unbound && <Pill tone="amber" data-testid={`env-unbound-${name}`}>{t("settings.mcp.form.envBinding.unbound")}</Pill>}
      {!showValue && bound !== "" && <span className="opacity-60">{t("settings.mcp.form.envBinding.boundTo", { variable: bound })}</span>}
    </li>
  );
}
