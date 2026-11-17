import { useCallback, useEffect, useState } from "react";
import { Alert, Button, Input, Panel, Pill } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import {
  FIRE_OUTCOMES,
  METHOD_AUTOMATION_TRIGGER_CREATE,
  METHOD_AUTOMATION_TRIGGER_DISABLE,
  METHOD_AUTOMATION_TRIGGER_ENABLE,
  METHOD_AUTOMATION_TRIGGER_FIRES,
  METHOD_AUTOMATION_TRIGGER_LIST,
  type EventFireView,
  type EventTriggerFiresResult,
  type EventTriggerListResult,
  type EventTriggerView,
  type FireOutcome,
} from "../../../lib/protocol";

/**
 * The automation bench (S40, T9) — triggers, enable/disable, the fire log.
 *
 * # What this page is
 *
 * A **bench**, not a product (S9 owns the designed experience; AU15 says so
 * in the slice's own words). It renders exactly what the six trigger methods
 * return and offers exactly the two writes a person is allowed to make from
 * a keyboard here: enable and disable. There is no create form — the JSON
 * textarea below is a bench's version of a write: it posts what you typed
 * and prints the daemon's refusal **untouched**, because the sentence IS the
 * product (T8's grammar speaks, this page repeats). There is no delete
 * button, no approve button, no "run now" — AU1 keeps authorship human and
 * S20 owns firing; a bench that grows those buttons has grown a door.
 *
 * # The five outcomes, zero included
 *
 * AU12 closes the fire vocabulary at five, and P31 says a count nobody took
 * must not read as a zero. The five Pills therefore render from the CLOSED
 * list — `FIRE_OUTCOMES.map(...)` — never from the keys of the counts map:
 * a missing key shows `0` beside its pill rather than hiding the pill, so
 * "could not say" stays visible even when the answer is none. It gets its
 * own tone (violet, not skipped's slate) and its own gloss: *we cannot
 * prove whether this started*. That sentence is the whole of AU13's
 * honesty on a bench.
 *
 * # Reasons are the daemon's words
 *
 * `fire.reason` and a disabled trigger's `disabledReason` print **verbatim**
 * in monospace — no trimming, no re-casing, no colour-coding into different
 * text (the PolicyPage rule, inherited here: a second formatter is a second
 * truth). The create box's refusal is the same rule at write time.
 *
 * # No new bridge, no timer
 *
 * Every call goes through the generic `rpc()` (AGENTS §4b: a Rust command
 * per method is a second method table). Loads happen on mount, after a
 * write, and on selecting a trigger — never on a timer.
 */

const OUTCOME_TONES: Record<FireOutcome, "green" | "red" | "amber" | "slate" | "violet"> = {
  started: "green",
  refused: "red",
  limited: "amber",
  skipped: "slate",
  "could-not-say": "violet",
};

function scopeOf(t: EventTriggerView): string {
  const parts = [
    t.rootId ? `root ${t.rootId}` : "",
    t.componentId ? `component ${t.componentId}` : "",
  ].filter((p) => p !== "");
  return parts.length > 0 ? parts.join(" · ") : "any root";
}

export function AutomationPage() {
  const [triggers, setTriggers] = useState<EventTriggerView[] | null>(null);
  const [fires, setFires] = useState<EventTriggerFiresResult | null>(null);
  const [selected, setSelected] = useState<string>(""); // "" = all triggers
  const [error, setError] = useState<RpcError | null>(null);
  const [firesError, setFiresError] = useState<RpcError | null>(null);
  const [unwired, setUnwired] = useState(false); // daemon answered "unknown method"
  const [draft, setDraft] = useState("");
  const [createError, setCreateError] = useState<string>("");
  const [disabling, setDisabling] = useState<{ id: string; reason: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setError(null);
    try {
      const list = await rpc<EventTriggerListResult>(METHOD_AUTOMATION_TRIGGER_LIST, {});
      setTriggers(list.triggers ?? []);
      setUnwired(false);
    } catch (e) {
      const err = toRpcError(e);
      // -32601 is not an empty desk — it is a daemon that predates S40. An
      // empty table here would be a confident wrong answer (P31).
      if (err.code === -32601) {
        setUnwired(true);
        setTriggers(null);
        setFires(null);
        return;
      }
      setUnwired(false);
      setError(err);
      return;
    }
    try {
      const log = await rpc<EventTriggerFiresResult>(METHOD_AUTOMATION_TRIGGER_FIRES, {
        triggerId: selected,
        limit: 50,
      });
      setFires(log);
      setFiresError(null);
    } catch (e) {
      setFiresError(toRpcError(e));
    }
  }, [selected]);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedTrigger = triggers?.find((t) => t.id === selected) ?? null;

  async function toggle(enable: boolean) {
    if (!selectedTrigger || busy) return;
    setBusy(true);
    try {
      if (enable) {
        await rpc(METHOD_AUTOMATION_TRIGGER_ENABLE, { id: selectedTrigger.id });
      } else {
        await rpc(METHOD_AUTOMATION_TRIGGER_DISABLE, {
          id: selectedTrigger.id,
          reason: (disabling?.reason ?? "").trim(),
        });
      }
      setDisabling(null);
      await load();
    } catch (e) {
      const err = toRpcError(e);
      // A refusal from a write is the daemon's sentence — print it whole in
      // the same place the create box prints its grammar sentence.
      setCreateError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function create() {
    setCreateError("");
    let params: unknown;
    try {
      params = JSON.parse(draft) as unknown;
    } catch (e) {
      // Local, and said locally: nothing goes out when the JSON does not
      // even parse — the daemon never learns a half-typed wish existed.
      setCreateError(`not valid JSON: ${e instanceof Error ? e.message : String(e)}`);
      return;
    }
    setBusy(true);
    try {
      await rpc(METHOD_AUTOMATION_TRIGGER_CREATE, params);
      setDraft("");
      await load();
    } catch (e) {
      setCreateError(toRpcError(e).message); // VERBATIM — the sentence is the product
    } finally {
      setBusy(false);
    }
  }

  if (unwired) {
    return (
      <div className="space-y-4" data-testid="automation-page">
        <Alert
          tone="amber"
          title="This daemon does not know the trigger methods (it predates S40)."
          description="Upgrade the daemon to `aeon serve` from a build that registers automation.trigger.* — an empty table here would be a lie."
          data-testid="unwired"
        />
      </div>
    );
  }

  return (
    <div className="space-y-4" data-testid="automation-page">
      {error && <ErrorPanel error={error} onRetry={() => void load()} />}

      <Panel tone="slate" data-testid="triggers-panel">
        <div className="flex items-center justify-between">
          <h2 className="text-sm font-semibold">Triggers</h2>
          <Button size="sm" variant="ghost" onClick={() => void load()} data-testid="refresh">
            refresh
          </Button>
        </div>
        {triggers === null ? (
          <p className="mt-2 text-xs text-slate-400" data-testid="triggers-pending">
            loading…
          </p>
        ) : triggers.length === 0 ? (
          <p className="mt-2 text-xs text-slate-400" data-testid="triggers-empty">
            no triggers — the desk is empty, and that is the desk's own answer, not the daemon's silence.
          </p>
        ) : (
          <table className="mt-2 w-full text-left text-xs">
            <thead className="text-slate-400">
              <tr>
                <th className="py-1 pr-2 font-normal">name</th>
                <th className="py-1 pr-2 font-normal">event</th>
                <th className="py-1 pr-2 font-normal">blueprint</th>
                <th className="py-1 pr-2 font-normal">scope</th>
                <th className="py-1 pr-2 font-normal">enabled</th>
                <th className="py-1 font-normal">last outcome</th>
              </tr>
            </thead>
            <tbody>
              {triggers.map((t) => (
                <tr
                  key={t.id}
                  data-testid={`trigger-row-${t.id}`}
                  onClick={() => setSelected(t.id === selected ? "" : t.id)}
                  className={`cursor-pointer border-t border-slate-800 ${t.id === selected ? "bg-slate-800/60" : ""}`}
                >
                  <td className="py-1 pr-2">
                    {t.name}
                    {!t.enabled && t.disabledReason !== "" && (
                      <div className="font-mono text-[10px] text-amber-300" data-testid={`disabled-reason-${t.id}`}>
                        {t.disabledReason}
                      </div>
                    )}
                  </td>
                  <td className="py-1 pr-2 font-mono">{t.match.event}</td>
                  <td className="py-1 pr-2 font-mono">{t.blueprint}</td>
                  <td className="py-1 pr-2">{scopeOf(t)}</td>
                  <td className="py-1 pr-2">
                    <Pill tone={t.enabled ? "green" : "slate"} size="sm" data-testid={`enabled-${t.id}`}>
                      {t.enabled ? "enabled" : "disabled"}
                    </Pill>
                  </td>
                  <td className="py-1">
                    {t.lastFire === null ? (
                      <span className="text-slate-500" data-testid={`last-fire-${t.id}`}>never fired</span>
                    ) : (
                      <span data-testid={`last-fire-${t.id}`}>
                        {t.lastFire.outcome === "" ? "claimed" : t.lastFire.outcome}
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-slate-800 pt-3">
          <span className="text-xs text-slate-400">
            {selectedTrigger ? `selected: ${selectedTrigger.name}` : "select a trigger row to change it"}
          </span>
          {selectedTrigger !== null && selectedTrigger.enabled && (
            <>
              <Input
                placeholder="reason for the pause"
                value={disabling?.reason ?? ""}
                onChange={(e) => setDisabling({ id: selectedTrigger.id, reason: e.target.value })}
                data-testid="disable-reason"
                className="max-w-56"
              />
              <Button
                size="sm"
                onClick={() => void toggle(false)}
                disabled={busy || (disabling?.id === selectedTrigger.id ? disabling.reason.trim() === "" : true)}
                data-testid="disable"
              >
                disable
              </Button>
            </>
          )}
          {selectedTrigger !== null && !selectedTrigger.enabled && (
            <Button size="sm" onClick={() => void toggle(true)} disabled={busy} data-testid="enable">
              enable
            </Button>
          )}
        </div>
      </Panel>

      <Panel tone="slate" data-testid="fire-log">
        <h2 className="text-sm font-semibold">
          Fire log {selected !== "" ? `· trigger ${selected}` : "· all triggers"}{" "}
          <span className="text-xs font-normal text-slate-400">(last 50)</span>
        </h2>
        <div className="mt-2 flex flex-wrap gap-2">
          {FIRE_OUTCOMES.map((o) => (
            <Pill key={o} tone={OUTCOME_TONES[o]} size="sm" data-testid={`pill-${o}`}>
              {o} {fires?.counts[o] ?? 0}
            </Pill>
          ))}
        </div>
        <p className="mt-1 text-[10px] text-slate-500" data-testid="could-not-say-gloss">
          could-not-say: we cannot prove whether this started (AU13) — a count nobody took is shown, not hidden.
        </p>
        {firesError && <ErrorPanel error={firesError} onRetry={() => void load()} />}
        {fires !== null && fires.fires.length === 0 && (
          <p className="mt-2 text-xs text-slate-400" data-testid="fires-empty">
            nothing has fired {selected !== "" ? "for this trigger" : "at all"} — the five counts above are the whole answer.
          </p>
        )}
        {fires !== null && fires.fires.length > 0 && (
          <table className="mt-2 w-full text-left text-xs">
            <thead className="text-slate-400">
              <tr>
                <th className="py-1 pr-2 font-normal">outcome</th>
                <th className="py-1 pr-2 font-normal">subject</th>
                <th className="py-1 pr-2 font-normal">depth</th>
                <th className="py-1 pr-2 font-normal">loop</th>
                <th className="py-1 pr-2 font-normal">cost</th>
                <th className="py-1 font-normal">reason</th>
              </tr>
            </thead>
            <tbody>
              {fires.fires.map((f: EventFireView) => (
                <tr key={f.id} className="border-t border-slate-800" data-testid={`fire-row-${f.id}`}>
                  <td className="py-1 pr-2">
                    {f.outcome === "" ? (
                      // The transient state (claimed, not yet decided) — its own
                      // label, never borrowed from one of the five (P31).
                      <Pill tone="slate" size="sm" data-testid={`fire-outcome-${f.id}`}>
                        claimed
                      </Pill>
                    ) : (
                      <Pill tone={OUTCOME_TONES[f.outcome]} size="sm" data-testid={`fire-outcome-${f.id}`}>
                        {f.outcome}
                      </Pill>
                    )}
                  </td>
                  <td className="py-1 pr-2 font-mono">
                    {f.subjectKind}:{f.subjectId}
                  </td>
                  <td className="py-1 pr-2">{f.depth}</td>
                  <td className="py-1 pr-2 font-mono">{f.loopSlug === "" ? "—" : f.loopSlug}</td>
                  <td className="py-1 pr-2">
                    {f.costNote !== "" && (
                      <Pill tone="amber" size="sm" data-testid={`cost-note-${f.id}`}>
                        {f.costNote}
                      </Pill>
                    )}
                  </td>
                  <td className="py-1 font-mono" data-testid={`fire-reason-${f.id}`}>
                    {f.reason}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel tone="slate" data-testid="create-panel">
        <h2 className="text-sm font-semibold">New trigger (JSON)</h2>
        <p className="mt-1 text-[10px] text-slate-500">
          A bench, not a form: type the params object and the daemon answers. Its refusal — every refusal —
          is printed exactly as it was spoken.
        </p>
        <textarea
          data-testid="create-json"
          aria-label="Trigger params JSON"
          spellCheck={false}
          className="mt-2 h-40 w-full rounded border border-slate-700 bg-slate-950 p-3 font-mono text-xs"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <div className="mt-2 flex items-center gap-2">
          <Button size="sm" onClick={() => void create()} disabled={busy || draft.trim() === ""} data-testid="create">
            create
          </Button>
        </div>
        {createError !== "" && (
          <div className="mt-2" data-testid="create-refusal">
            <Alert tone="red" title="The daemon's answer, verbatim" description={createError} />
          </div>
        )}
      </Panel>
    </div>
  );
}
