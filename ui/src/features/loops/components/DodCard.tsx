import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ConfirmModal, Panel, Pill } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";

/**
 * The DoD card on the real loop screen (S57 T11, VG22): what the loop's
 * brief committed as its definition of done, item by item, with the state
 * the daemon holds and the evidence a met item names.
 *
 * The state words here (`unchecked`, `met`, `unmet`, `unverifiable`) and
 * the evidence kind `manual` are PROTOCOL CONSTANTS — the store's own
 * CHECK constraints (migration 20261102064454, line 64) — because the
 * protocol ships no `dod.vocabulary` door to read them from. The card
 * captions them through i18n and compares only structurally (the
 * `// wire-member` lines the guard exempts); a refusal to mark is the
 * daemon's sentence, verbatim, and the ledger below only ever shows what
 * `dod.list` and the mark's own response carried.
 */
type DodItem = RpcMethods["dod.list"]["result"]["items"][number];

export interface DodCardProps {
  /** The loop's slug — `dod.list` is keyed by it, never by a UI guess. */
  slug: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

// The four states are the migration's CHECK (20261102064454_merge_rules.sql
// l.64): the protocol ships no `dod.vocabulary` door, so this table is the
// card's ONE naming of the constants, for pill tones only — no behaviour
// branches on them beyond the met-needs-note law the daemon also enforces.
const DOD_TONES: Record<string, "emerald" | "rose" | "amber" | "slate"> = {
  met: "emerald", unmet: "rose", unverifiable: "amber", unchecked: "slate", // wire-member: the migration's closed set
};
const MET = "met"; // wire-member: the state whose evidence the store refuses to leave empty
const UNMET = "unmet"; // wire-member: the state a human marks when the item is simply not done

export function DodCard({ slug, controller, live, canWrite }: DodCardProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const ledger = useRpcQuery("dod.list", { loop: slug }, controller, live);
  const [confirming, setConfirming] = useState<{ item: DodItem; state: string } | undefined>(undefined);
  const [note, setNote] = useState("");
  const [locallyMarked, setLocallyMarked] = useState<Record<string, DodItem>>({});

  const items = (ledger.data?.items ?? []).map((item) => locallyMarked[item.id] ?? item);

  const mark = async (item: DodItem, state: string) => {
    const params: RpcMethods["dod.mark"]["params"] = { id: item.id, state };
    if (note.trim() !== "") {
      // The daemon's own evidence pair, upper-cased as the Go struct
      // marshals it — the `mark.json` fixture spelling.
      params.evidence = { Kind: "manual", Ref: note.trim() }; // wire-member
    }
    const result = await action.run(() => rpcTyped("dod.mark", params), {
      errorTitle: t("loops.dod.markFailed"),
    });
    if (result) {
      // The list updates from the RESPONSE — the item the daemon answered
      // with — and the full ledger re-reads on the next natural refetch.
      setLocallyMarked((current) => ({ ...current, [result.item.id]: result.item }));
      setNote("");
      void ledger.refetch();
    }
  };

  return (
    <Panel title={t("loops.dod.title")} subtitle={t("loops.dod.subtitle")} data-testid="dod-card">
      {ledger.error && (
        <Alert intent="danger" title={t("loops.dod.failed")} description={ledger.error} data-testid="dod-failed">
          <Button size="sm" onClick={ledger.refetch}>
            {t("loops.dod.retry")}
          </Button>
        </Alert>
      )}
      {!ledger.error && !ledger.loading && items.length === 0 && (
        <p className="text-sm opacity-70" data-testid="dod-empty">
          {t("loops.dod.empty")}
        </p>
      )}
      {action.rpcError && (
        // Refused marks render their parts: "a met item names its evidence"
        // is the sentence that teaches the repair, and it lives in the data.
        <div data-testid="dod-error">
          <ErrorPanel error={action.rpcError} />
        </div>
      )}
      <ul className="space-y-2" data-testid="dod-list">
        {items.map((item) => (
          <li key={item.id} className="rounded-xl border border-slate-200 p-3 text-sm dark:border-slate-800" data-testid={`dod-item-${item.id}`}>
            <div className="flex flex-wrap items-center gap-2">
              <span className="opacity-70">#{item.idx + 1}</span>
              <span className="font-medium">{item.text}</span>
              <Pill tone={DOD_TONES[item.state] ?? "slate"} data-testid={`dod-state-${item.id}`}>
                {t(`loops.dod.word.${item.state}`)}
              </Pill>
            </div>
            {(item.evidenceKind || item.evidenceRef) && (
              <p className="text-xs opacity-80" data-testid={`dod-evidence-${item.id}`}>
                {t("loops.dod.evidence", { kind: item.evidenceKind ?? "", ref: item.evidenceRef ?? "" })}
              </p>
            )}
            {canWrite && (
              <div className="mt-2 flex flex-wrap gap-2" data-testid={`dod-actions-${item.id}`}>
                <Button size="sm" onClick={() => { setConfirming({ item, state: MET }); setNote(""); }} data-testid={`dod-mark-met-${item.id}`}>
                  {t("loops.dod.markMet")}
                </Button>
                <Button size="sm" onClick={() => { setConfirming({ item, state: UNMET }); setNote(""); }} data-testid={`dod-mark-unmet-${item.id}`}>
                  {t("loops.dod.markUnmet")}
                </Button>
              </div>
            )}
          </li>
        ))}
      </ul>

      {confirming && (
        <ConfirmModal
          isOpen
          title={t("loops.dod.confirmTitle", { state: t(`loops.dod.word.${confirming.state}`) })}
          description={confirming.item.text}
          confirmLabel={t("loops.dod.confirm")}
          isConfirmDisabled={confirming.state === MET && note.trim() === ""}
          onClose={() => setConfirming(undefined)}
          onConfirm={() => {
            const target = confirming;
            setConfirming(undefined);
            void mark(target.item, target.state);
          }}
        >
          {/* The kit renders a block body through children (its `description`
              is a paragraph). The note is the evidence the met mark needs. */}
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-xs uppercase tracking-wide opacity-70">{t("loops.dod.note")}</span>
            <input
              className="rounded-md border border-slate-300 bg-transparent px-2 py-1 dark:border-slate-700 dark:bg-slate-900"
              value={note}
              onChange={(event) => setNote(event.target.value)}
              data-testid="dod-confirm-note"
            />
            {confirming.state === MET && note.trim() === "" && (
              <span className="text-xs opacity-80" data-testid="dod-confirm-note-required">
                {t("loops.dod.noteRequired")}
              </span>
            )}
          </label>
          <p className="text-xs italic opacity-80">{t("loops.dod.confirmHuman")}</p>
        </ConfirmModal>
      )}
    </Panel>
  );
}
