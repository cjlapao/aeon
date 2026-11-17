import { useCallback, useEffect, useRef, useState } from "react";
import { Modal } from "@cjlapao/ui-kit";
import { Alert, Button, Pill } from "../../components/kit";
import { rpcTyped, toRpcError } from "../../lib/bridge";
import type { LiveState } from "../../lib/live";
import { useLiveSubscription } from "../../lib/useLiveSubscription";

/**
 * A loop's permission mode, from the loop page: the switch that lets its agents run without asking.
 *
 * Three modes. ASK is the default: a shell, a file write, the network or another agent asks you first.
 * PRE-APPROVED lets the named tools run without asking (the way an unattended loop gets to use them, since
 * nobody is there to answer). FULL ACCESS lets every asked call run. None of them overrides a rule that
 * DENIES something, and the person turns it off again with one click. Turning full access on is a
 * deliberate act: a modal says what it gives the agents and wants an explicit "I understand".
 */

type Access = { id: string; mode: string; tools: string[]; setBy?: string; setAt?: string; askingTools: string[] };

export function LoopAccess({ loopId, live, canWrite, openSignal = 0, askSignal = 0, onAccess }: {
  loopId: string; live: LiveState; canWrite: boolean; openSignal?: number; askSignal?: number; onAccess?: (mode: "ask" | "tools" | "full", label: string) => void;
}) {
  const [error, setError] = useState("");
  const [access, setAccess] = useState<Access | undefined>();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"ask" | "tools" | "full">("ask");
  const [tools, setTools] = useState<string[]>([]);
  const [understood, setUnderstood] = useState(false);
  const [busy, setBusy] = useState(false);

  const read = useCallback(async () => {
    try {
      setAccess((await rpcTyped("loop.access", { id: loopId })) as Access);
    } catch {
      setAccess(undefined); // an older daemon has no such door: the control stays out of the way
    }
  }, [loopId]);
  useEffect(() => { if (live.state === "current") void read(); }, [live.state, read]);
  useLiveSubscription(live, ["loop.access.changed"], (event) => {
    if ((event.payload as { loopId?: string } | undefined)?.loopId === loopId) void read();
  });

  // The ask card's "Allow everything in this loop" button bumps the signal: open the dialog on full access, so the
  // person still reads the warning and ticks "I understand" before anything is granted.
  const lastSignal = useRef(openSignal);
  useEffect(() => {
    if (openSignal === lastSignal.current || !access) return;
    lastSignal.current = openSignal;
    setMode("full"); setTools(access.tools); setUnderstood(false); setError(""); setOpen(true);
  }, [openSignal, access]);

  // The composer's shield menu picks Ask directly: going back to asking needs no warning, so it applies at once.
  const lastAsk = useRef(askSignal);
  useEffect(() => {
    if (askSignal === lastAsk.current || !access) return;
    lastAsk.current = askSignal;
    void (async () => {
      try { await rpcTyped("loop.setAccess", { id: loopId, mode: "ask" } as { id: string; mode: string }); await read(); }
      catch (cause) { setError(toRpcError(cause).message); setMode("ask"); setOpen(true); }
    })();
  }, [askSignal, access, loopId, read]);

  const on = access ? access.mode !== "ask" : false;
  const label = !access ? "" : access.mode === "full" ? "Full access" : access.mode === "tools" ? `Pre-approved: ${access.tools.length}` : "Asks first";
  const reportMode = access?.mode as "ask" | "tools" | "full" | undefined;
  useEffect(() => { if (reportMode) onAccess?.(reportMode, label); }, [reportMode, label, onAccess]);
  if (!access) return null;

  const openModal = () => {
    setMode(access.mode as "ask" | "tools" | "full");
    setTools(access.tools);
    setUnderstood(false);
    setError("");
    setOpen(true);
  };
  const apply = async () => {
    setBusy(true);
    try {
      // the generated contract only saw the tool-less fixture, so the optional list is typed through
      await rpcTyped("loop.setAccess", (mode === "tools" ? { id: loopId, mode, tools } : { id: loopId, mode }) as { id: string; mode: string });
      setOpen(false);
      await read();
    } catch (cause) {
      setError(toRpcError(cause).message); // the daemon's own words, kept inside the dialog so they are read
    } finally {
      setBusy(false);
    }
  };
  const toggleTool = (name: string) => setTools((cur) => (cur.includes(name) ? cur.filter((t) => t !== name) : [...cur, name]));
  const blocked = mode === "full" ? !understood : mode === "tools" ? tools.length === 0 : false;

  return (
    <>
      <button
        type="button"
        onClick={openModal}
        className="rounded-full focus:outline-none focus-visible:ring-2"
        aria-label={`Loop permissions: ${label}`}
        data-testid="loop-access"
      >
        <Pill size="sm" tone={access.mode === "full" ? "red" : on ? "amber" : "slate"}>{label}</Pill>
      </button>
      {open && (
        <Modal isOpen onClose={() => setOpen(false)} title="What may this loop's agents do without asking?" size="md" data-testid="loop-access-modal">
          <div className="space-y-4 text-sm">
            <p className="opacity-80">By default an agent asks you before it runs a shell command, writes a file, reaches the network or talks to another agent. You answer each one as it comes up.</p>
            <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-current/10 p-3">
              <input type="radio" name="access-mode" checked={mode === "ask"} onChange={() => setMode("ask")} className="mt-1" />
              <span><span className="block font-medium">Ask me (default)</span><span className="block text-xs opacity-70">Every one of those asks first.</span></span>
            </label>
            <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-current/10 p-3">
              <input type="radio" name="access-mode" checked={mode === "tools"} onChange={() => setMode("tools")} className="mt-1" />
              <span className="min-w-0 flex-1">
                <span className="block font-medium">Pre-approve some tools</span>
                <span className="block text-xs opacity-70">These run without asking. Needed for a loop nobody is watching, since nobody can answer.</span>
                {mode === "tools" && (
                  <span className="mt-2 flex flex-wrap gap-2" role="group" aria-label="Pre-approved tools">
                    {access.askingTools.map((name) => (
                      <label key={name} className="inline-flex items-center gap-1.5 rounded-lg border border-current/10 px-2 py-1 text-xs">
                        <input type="checkbox" checked={tools.includes(name)} onChange={() => toggleTool(name)} aria-label={name} />{name}
                      </label>
                    ))}
                  </span>
                )}
              </span>
            </label>
            <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-red-500/40 p-3">
              <input type="radio" name="access-mode" checked={mode === "full"} onChange={() => setMode("full")} className="mt-1" />
              <span><span className="block font-medium text-red-600 dark:text-red-400">Full access</span><span className="block text-xs opacity-70">Every call that would ask you runs instead.</span></span>
            </label>
            {mode === "full" && (
              <Alert intent="danger" title="This gives the agents super powers" data-testid="loop-access-warning">
                <p>With full access on, this loop's agents can run any shell command, change or delete any file in the workspace, call the network and message other agents, without asking you. A mistake or a bad instruction in a file they read can do real damage, and you will not be asked first.</p>
                <p className="mt-2">A rule that <b>denies</b> something still denies it. You can turn this off at any time, and it applies to this loop only.</p>
                <label className="mt-3 flex items-start gap-2">
                  <input type="checkbox" checked={understood} onChange={(e) => setUnderstood(e.target.checked)} aria-label="I understand" className="mt-1" />
                  <span>I understand that the agents in this loop will no longer ask before they act.</span>
                </label>
              </Alert>
            )}
            {error && <Alert intent="danger" title="That did not change" description={error} data-testid="loop-access-error" />}
            {!canWrite && <Alert intent="warning" title="Reconnecting — changes are off until the daemon is current." />}
            <div className="flex justify-end gap-2 pt-1">
              <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
              <Button color={mode === "full" ? "red" : undefined} onClick={() => void apply()} disabled={!canWrite || blocked} loading={busy} data-testid="loop-access-apply">
                {mode === "full" ? "Turn on full access" : mode === "tools" ? "Pre-approve these tools" : "Ask me again"}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}
