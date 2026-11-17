import { useEffect, useState } from "react";
import { CustomIcon } from "../../components/kit";
import type { RunOption } from "./center/useChatActions";

/**
 * The *Run blueprint…* dialogue (S39 CR5): what could be run here, said by
 * the daemon BEFORE any start. Entries are never hidden — a blueprint whose
 * requires are unmet is listed DISABLED with the daemon's reason (the S9
 * start-form rule: hiding an option is an opinion; showing it with its
 * verdict is information). Choosing one of the offered candidates — an
 * approved goal, an approved manual task — is what enables Run; the person
 * naming a record is the confirmation, and no ask row sits in front of this
 * door (it is the human's door; the agent's twin wears the operator's ask).
 */
export interface RunBlueprintPanelProps {
  load(): Promise<RunOption[]>;
  onRun(req: { blueprint: string; goalId?: string; taskId?: string; message?: string }): Promise<void>;
  onClose(): void;
}

export function RunBlueprintPanel({ load, onRun, onClose }: RunBlueprintPanelProps) {
  const [options, setOptions] = useState<RunOption[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [chosen, setChosen] = useState<Record<string, number>>({});
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let live = true;
    load().then((rows) => { if (live) setOptions(rows); }, (cause) => { if (live) setLoadError(cause instanceof Error ? cause.message : String(cause)); });
    return () => { live = false; };
  }, [load]);

  const run = async (option: RunOption) => {
    setBusy(true); setError("");
    try {
      const picked = option.candidates?.[chosen[option.blueprint] ?? -1];
      await onRun({
        blueprint: option.blueprint,
        ...(picked?.kind === "goal" ? { goalId: picked.id } : {}),
        ...(picked?.kind === "task" ? { taskId: picked.id } : {}),
        ...(message.trim() ? { message: message.trim() } : {}),
      });
      onClose(); // the loop.move is the event's to make (loop.run.started)
    } catch (cause) {
      // The daemon's sentence, verbatim, and the panel stays open — the
      // person has not finished the conversation.
      setError(cause instanceof Error ? cause.message : String(cause));
      setBusy(false);
    }
  };

  return <div data-testid="run-blueprint-panel" className="border-b border-slate-200 bg-slate-50/80 px-4 py-3 dark:border-white/10! dark:bg-white/5!">
    <div className="flex items-center gap-2">
      <span className="text-sm font-semibold text-slate-800 dark:text-slate-100!">Run blueprint…</span>
      <button type="button" onClick={onClose} className="ml-auto rounded-lg border border-slate-200 px-2.5 py-1 text-xs text-slate-600 hover:bg-white dark:border-white/15! dark:text-slate-300!">Close</button>
    </div>
    <input value={message} onChange={(e) => setMessage(e.target.value)} placeholder="First message (optional)" aria-label="First message (optional)"
      className="mt-2 w-full rounded-lg border border-slate-300 bg-white px-3 py-1.5 text-sm dark:border-white/20! dark:bg-black/20! dark:text-slate-100!" />
    {loadError && <p role="alert" className="mt-2 text-xs text-red-700">{loadError}</p>}
    {options === null && !loadError && <p className="mt-2 text-sm text-slate-500">Asking the daemon what can run…</p>}
    {options?.length === 0 && <p className="mt-2 text-sm text-slate-500">The daemon offered no blueprint for this loop.</p>}
    <ul className="mt-2 space-y-2">
      {(options ?? []).map((option) => {
        const candidate = chosen[option.blueprint];
        const picked = option.candidates?.[candidate ?? -1];
        const runnable = option.runnable || Boolean(picked);
        return <li key={option.blueprint} data-testid={`run-blueprint-${option.blueprint}`}
          className={`rounded-lg border px-3 py-2 ${runnable ? "border-slate-200 bg-white dark:border-white/10! dark:bg-white/5!" : "border-slate-200 bg-slate-100/60 dark:border-white/5! dark:bg-white/[0.02]!"}`}>
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-100!">{option.blueprint}</span>
            {option.version !== undefined && <span className="font-mono text-xs text-slate-500">v{option.version}</span>}
            {!option.runnable && option.reason && <span className="text-xs text-slate-500">{option.reason}</span>}
            <button type="button" disabled={!runnable || busy} onClick={() => void run(option)}
              className="ml-auto inline-flex items-center gap-1.5 rounded-lg bg-blue-700 px-3 py-1 text-sm font-medium text-white hover:bg-blue-800 disabled:cursor-not-allowed disabled:opacity-50">
              <CustomIcon icon="Send" size="xs" />Run
            </button>
          </div>
          {!option.runnable && option.candidates && option.candidates.length > 0 && (
            <select aria-label={`goal or task for ${option.blueprint}`} value={candidate ?? ""} className="mt-1.5 rounded-lg border border-slate-300 bg-white px-2 py-1 text-xs dark:border-white/20! dark:bg-black/20! dark:text-slate-100!"
              onChange={(e) => setChosen((c) => ({ ...c, [option.blueprint]: Number(e.target.value) }))}>
              <option value="" disabled>choose what it needs…</option>
              {option.candidates.map((c, i) => <option key={`${c.kind}:${c.id}`} value={String(i)}>{c.kind} {c.title}</option>)}
            </select>
          )}
        </li>;
      })}
    </ul>
    {error && <p role="alert" className="mt-2 text-xs text-red-700">{error}</p>}
  </div>;
}
