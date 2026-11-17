import { useEffect, useState } from "react";
import { Button, Input } from "../../components/kit";
import { rpc, rpcTyped, toRpcError } from "../../lib/bridge";
import type { LoopDetail } from "../loops/pages/LoopScreen";

// Why a loop stopped, and the way to move it again (S76). A blocked loop used to
// say so only in the header status; a person who reopened the chat saw a
// history that simply ended. The reason is the loop's own (`lastReason`), and the
// way out is the daemon's own verdict on `resume` (`loop.control`), shown with its
// refusal sentence when it says no — never a button that cannot work, never a
// refusal the UI invented.

export interface LoopBlockedBannerProps {
  loop: LoopDetail;
  canWrite: boolean;
  /** After a resume the page re-reads the loop. */
  onResumed: () => void;
}

export function LoopBlockedBanner({ loop, canWrite, onResumed }: LoopBlockedBannerProps) {
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);
  const [eligible, setEligible] = useState<boolean | undefined>(undefined);

  useEffect(() => {
    let alive = true;
    void rpcTyped("loop.control", { id: loop.id }).then((c) => {
      if (!alive) return;
      setEligible(c.eligibility.resume.eligible);
      setRefusal(c.eligibility.resume.eligible ? undefined : c.eligibility.resume.refusal?.detail);
    }).catch(() => { if (alive) setEligible(undefined); });
    return () => { alive = false; };
  }, [loop.id, loop.status]);

  const resume = async () => {
    setBusy(true);
    setError(undefined);
    try {
      await rpc("loop.resume", { id: loop.id, ...(note.trim() ? { message: note.trim() } : {}) });
      setNote("");
      onResumed();
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  // A block that "has its own remedy, not a resume" is still not a dead end: the step that blocked can always be run again.
  const recover = async () => {
    setBusy(true);
    setError(undefined);
    try {
      const res = await rpc<{ refused?: { detail: string } }>("loop.recover", { id: loop.id });
      if (res?.refused) { setError(res.refused.detail); return; }
      onResumed();
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  // One compact strip: the reason (clamped, scrolls if long), Resume, and the
  // note behind a disclosure. It used to stack an Alert, an input and a refusal
  // line, and a long reason widened the whole page past the window.
  const [noting, setNoting] = useState(false);
  return (
    <div data-testid="loop-blocked-banner" className="min-w-0 max-w-full space-y-1 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm dark:border-red-500/40! dark:bg-red-500/10!">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
        <strong className="shrink-0 text-red-800 dark:text-red-200!">Blocked</strong>
        <span className="min-w-0 flex-1 break-words text-red-900 [overflow-wrap:anywhere] line-clamp-2 dark:text-red-100!" title={loop.lastReason} data-testid="loop-blocked-reason">
          {loop.lastReason || "The daemon recorded no reason."}
        </span>
        <Button size="xs" variant="ghost" onClick={() => setNoting((v) => !v)} aria-expanded={noting} data-testid="loop-blocked-note-toggle">
          {noting ? "Hide note" : "Add a note"}
        </Button>
        <Button size="xs" variant={eligible === false ? "solid" : "ghost"} disabled={!canWrite || busy} onClick={() => void recover()} title="Tell the agent what went wrong and continue the same step, keeping its context" data-testid="loop-blocked-recover">
          {busy ? "Working…" : "Retry this step"}
        </Button>
        <Button size="xs" disabled={!canWrite || busy || eligible === false} onClick={() => void resume()} data-testid="loop-blocked-resume">
          {busy ? "Resuming…" : "Resume"}
        </Button>
      </div>
      {noting && (
        <Input aria-label="Note for the agent" placeholder="A note for the agent, sent with the resume" value={note} onChange={(e) => setNote(e.target.value)} className="w-full" />
      )}
      {eligible === false && refusal && <p className="min-w-0 break-words text-xs text-slate-600 dark:text-slate-300!" data-testid="loop-blocked-refusal">{refusal}</p>}
      {error && <p className="min-w-0 break-words text-xs text-red-700" role="alert" data-testid="loop-blocked-error">{error}</p>}
    </div>
  );
}
