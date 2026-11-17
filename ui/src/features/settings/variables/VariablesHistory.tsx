import type { VaultChange } from "./variablesRpc";

// What changed, newest first: when, what was done, by whom, in the daemon's own sentence.
// A change never carries a value, so there is nothing here to hide.

export function VariablesHistory({ changes }: { changes: VaultChange[] | undefined }) {
  return (
    <ul className="space-y-3 text-sm" data-testid="variables-history">
      {changes?.length === 0 && <li className="opacity-70">Nothing has changed yet.</li>}
      {changes?.map((c) => (
        <li key={c.id} className="border-l-2 border-white/15 pl-3">
          <div>{c.summary}</div>
          <div className="text-xs opacity-60"><time dateTime={c.occurredAt}>{c.occurredAt}</time> · {c.op} · {c.actorKind}:{c.actorId}</div>
        </li>
      ))}
    </ul>
  );
}
