import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, Input, Modal, Panel, Pill } from '../../components/kit';
import { rpc, toRpcError } from '../../lib/bridge';
import { formatUSD } from '../../lib/cost';
import { resetDetachedNotes } from '../../components/DetachedNote';

export interface ComponentLoop {
  id: string; slug: string; title?: string; phase?: string; status: string; updatedAt: string; archivedAt?: string;
}

/**
 * The two ways to tidy a loop away, as buttons you can always see: Archive (hide it, everything stays; Restore brings it
 * back) and Delete… (remove it, keep its totals and its tasks). Used on the loop's card and in the loops table.
 */
export function LoopActions({ loop, canWrite, onChanged, onDelete, className = '' }: {
  loop: ComponentLoop; canWrite: boolean; onChanged: () => void; onDelete: (loop: ComponentLoop) => void; className?: string;
}) {
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const running = loop.status === 'running';
  const hidden = Boolean(loop.archivedAt);
  const setArchived = async (archived: boolean) => {
    setBusy(true); setError('');
    try { await rpc('loop.setArchived', { id: loop.id, archived }); resetDetachedNotes(); onChanged(); }
    catch (cause) { setError(toRpcError(cause).message); }
    finally { setBusy(false); }
  };
  return (
    <span className={`inline-flex flex-wrap items-center justify-end gap-1.5 ${className}`} onClick={(e) => e.stopPropagation()}>
      <Button size="sm" variant="outline" disabled={!canWrite || busy || (running && !hidden)} data-testid="loop-archive"
        title={running && !hidden ? 'Stop the loop first' : hidden ? 'Show this loop in the lists again' : 'Hide this loop; everything in it stays, and its open tasks are closed (not done) until you restore it'}
        onClick={() => void setArchived(!hidden)}>{hidden ? 'Restore' : 'Archive'}</Button>
      <Button size="sm" variant="outline" disabled={!canWrite || busy || running} data-testid="loop-delete"
        title={running ? 'Stop the loop first' : 'Delete this loop; its totals and its tasks stay'} onClick={() => onDelete(loop)}>Delete…</Button>
      {error && <span role="alert" className="basis-full text-right text-xs text-red-700 dark:text-red-300">{error}</span>}
    </span>
  );
}

/** A loop in the component page. */
export function LoopCard({ loop, canWrite, onChanged, onDelete }: { loop: ComponentLoop; canWrite: boolean; onChanged: () => void; onDelete: (loop: ComponentLoop) => void }) {
  const hidden = Boolean(loop.archivedAt);
  return (
    <Panel tone="slate" data-testid="component-loop">
      <div className="flex items-start justify-between gap-3">
        <h3 className="min-w-0 truncate font-medium" title={loop.title || loop.slug}>{loop.title || loop.slug}</h3>
        <div className="flex items-center gap-1.5">
          {hidden && <Pill tone="slate">archived</Pill>}
          <Pill tone={loop.status === 'blocked' ? 'red' : loop.status === 'waiting' ? 'amber' : 'emerald'}>{loop.status}</Pill>
        </div>
      </div>
      <p className="mt-2 text-sm text-slate-500">{loop.phase || 'Not started'} · {new Date(loop.updatedAt).toLocaleString()}</p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Link className="aeon-accent-text underline" to={`/loops/${encodeURIComponent(loop.id)}`}>Open workflow and files →</Link>
        <LoopActions loop={loop} canWrite={canWrite} onChanged={onChanged} onDelete={onDelete} className="ml-auto" />
      </div>
    </Panel>
  );
}

interface DeleteResult { tombstone?: { steps: number; toolCalls: number; costUsd?: number; costState: string } }

/**
 * What deleting a loop takes and what it keeps, and the loop's slug to type before it can be done. After the delete it
 * stays open to say it is done and what was kept, and closes when told to.
 */
export function DeleteLoopDialog({ loop, onClose, onDeleted }: { loop: ComponentLoop; onClose: () => void; onDeleted: () => void }) {
  const [typed, setTyped] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState<DeleteResult | null>(null);
  const field = useRef<HTMLInputElement>(null);
  // Focus the field once, when the dialog opens; never again, so selecting the text above it is never interrupted.
  useEffect(() => { field.current?.focus(); }, []);
  const confirm = async () => {
    setBusy(true); setError('');
    try {
      const res = await rpc<DeleteResult>('loop.delete', { id: loop.id, confirm: typed, ...(note.trim() ? { note: note.trim() } : {}) });
      setDone(res ?? {});
      resetDetachedNotes();
      onDeleted();
    } catch (cause) { setError(toRpcError(cause).message); }
    finally { setBusy(false); }
  };
  const t = done?.tombstone;
  return (
    <Modal isOpen onClose={onClose} title={done ? 'Loop deleted' : 'Delete this loop?'} ariaLabel={done ? 'Loop deleted' : 'Delete this loop'} role="dialog"
      footer={done
        ? <div className="flex justify-end"><Button size="sm" onClick={onClose} data-testid="loop-delete-close">Close</Button></div>
        : <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" onClick={() => void confirm()} disabled={busy || typed !== loop.slug} data-testid="loop-delete-confirm">{busy ? 'Deleting…' : 'Delete loop'}</Button>
        </div>}>
      {done ? (
        <div data-testid="loop-delete-done" className="space-y-3 text-sm text-slate-700 dark:text-slate-200">
          <p><strong>{loop.title || loop.slug}</strong> was deleted.</p>
          <p className="text-slate-600 dark:text-slate-400">
            Kept: the component's costs and statistics, and this loop's totals
            {t ? ` (${t.steps} steps, ${t.toolCalls} calls, ${t.costUsd === undefined ? 'cost not reported' : `$${formatUSD(t.costUsd)}`})` : ''},
            and every task, roadmap item and queue entry that was linked to it, which now say their loop was deleted.
          </p>
        </div>
      ) : (<>
        <p className="text-sm text-slate-700 dark:text-slate-200">
          <strong>{loop.title || loop.slug}</strong> will go for good, with its steps, chat, journal, questions and documents. This cannot be undone.
          To keep it out of sight without losing anything, archive it instead.
        </p>
        <p className="mt-3 text-sm text-slate-600 dark:text-slate-400">
          <strong>Kept:</strong> the component's costs and statistics, this loop's own totals (as a record that it existed), and every task, roadmap item and queue
          entry that was linked to it. They stay and say that their loop was deleted; the ones still open are closed as not done, so no agent picks them up.
        </p>
        <div className="mt-4 text-sm">
          <p className="text-slate-600 dark:text-slate-400">Type <code className="font-mono">{loop.slug}</code> to confirm</p>
          <Input ref={field} value={typed} onChange={(e) => setTyped(e.target.value)} aria-label="loop slug" data-testid="loop-delete-slug" />
        </div>
        <div className="mt-3 text-sm">
          <p className="text-slate-600 dark:text-slate-400">Why (optional)</p>
          <Input value={note} onChange={(e) => setNote(e.target.value)} aria-label="reason" />
        </div>
        {error && <p role="alert" className="mt-3 text-sm text-red-700 dark:text-red-300">{error}</p>}
      </>)}
    </Modal>
  );
}

/** The host side of deleting: one dialog for the whole list, so a refresh of the list can never close or reset it. */
export function useDeleteLoop(onChanged: () => void) {
  const [loop, setLoop] = useState<ComponentLoop | null>(null);
  const dialog = loop ? <DeleteLoopDialog key={loop.id} loop={loop} onClose={() => setLoop(null)} onDeleted={onChanged} /> : null;
  return { open: setLoop, dialog };
}

export interface Tombstone {
  loopId: string; slug: string; title: string; deletedAt: string; steps: number; turns: number; toolCalls: number; costUsd?: number; costState: string;
}

/** The loops deleted from this component, with the totals they had earned: the record a deletion leaves. */
export function DeletedLoops({ tombstones }: { tombstones: readonly Tombstone[] }) {
  if (tombstones.length === 0) return null;
  const known = tombstones.filter((t) => t.costUsd !== undefined);
  const total = known.reduce((a, t) => a + (t.costUsd ?? 0), 0);
  return (
    <Panel tone="slate" data-testid="deleted-loops">
      <h3 className="font-medium">Deleted loops · {tombstones.length}</h3>
      <p className="mt-1 text-sm text-slate-500">Their costs and totals are still counted in this component's statistics.
        {known.length > 0 && ` Together they spent ${known.length < tombstones.length ? 'at least ' : ''}$${formatUSD(total)}.`}</p>
      <ul className="mt-2 divide-y divide-slate-200 text-sm dark:divide-white/10">
        {tombstones.map((t) => (
          <li key={t.loopId} className="flex flex-wrap items-center gap-x-3 py-1.5">
            <span className="min-w-0 flex-1 truncate">{t.title || t.slug}</span>
            <span className="font-mono text-xs text-slate-500">{t.steps} steps · {t.toolCalls} calls · {t.costUsd === undefined ? 'cost not reported' : `$${formatUSD(t.costUsd)}`}</span>
            <span className="font-mono text-xs text-slate-400">{new Date(t.deletedAt).toLocaleDateString()}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}
