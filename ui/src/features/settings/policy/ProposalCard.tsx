import { useCallback, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Panel, Pill } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import type { LiveController, LiveState } from "../../../lib/live";
import { actionTone } from "./PolicyList";
import { type ProposalRow } from "./policyModel";

/**
 * One proposal, as a decision card (S45 T9, PM7).
 *
 * The card pulls its **live-vs-proposed preview** and its **digest** from
 * `proposal.get` — the digest the person read is the digest the commit
 * carries (`proposal.commit {id, digest}`; a mismatch is the daemon's
 * "the proposal changed since you read it", shown verbatim). Commit runs
 * behind a confirmation showing the preview and the current in-force rows
 * the daemon answered with it, and the result renders `effectApplied`,
 * `before`, `after` and `warnings` word for word.
 *
 * Reject REQUIRES a reason (required, ≤ 512) — the socket door refuses an
 * unreasoned reject (PM7.5); the UI stops it before the call. Discard stays
 * exactly as S35 left it. Since S45 T10 `agent_import` commits like every
 * other kind (PM4 — one implementation, two doors): its confirmation shows
 * the CONSENT bytes (proposal.get answers the kind's preview with S37's own
 * renderer, verbatim), and the commit RESULT carries the import's own
 * post-import print as data — the daemon captured it, printed nothing
 * itself (AA4); this card renders what came back, word for word.
 *
 * The "proposed by an agent" badge (kind `agent`) makes visible which
 * restrictions agents asked for (PM9).
 */
export interface ProposalCardProps {
  row: ProposalRow;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  /** The desk re-reads the list after any decision. */
  onDecided: () => void;
}

const REJECT_MAX = 512;

/** Wire rows the contract types as `Array<unknown>` (a fixture never showed
 *  their fields) are read defensively: whatever object the daemon sent is
 *  rendered field by field, and nothing is invented for an absent one. */
function rowsOf(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value) ? (value as Array<Record<string, unknown>>) : [];
}

function answerLine(answer: Record<string, unknown>): string {
  return [answer["state"], answer["policyID"], answer["render"]]
    .filter((part) => typeof part === "string" && part !== "")
    .join(" · ");
}

export function ProposalCard({ row, controller, live, canWrite, onDecided }: ProposalCardProps) {
  const { t } = useI18n();
  const preview = useRpcQuery("proposal.get", { id: row.id }, controller, live);
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [reason, setReason] = useState("");
  const [outcome, setOutcome] = useState<{ kind: "commit" | "reject"; text: string[]; effectApplied?: boolean } | null>(null);

  const get = preview.data;
  const isAgentProposal = row.createdByKind === "agent";
  const shownDigest = get?.digest;

  const commit = useCallback(async () => {
    if (!get || shownDigest === undefined) return;
    const lines = rowsOf(get.preview?.inForce)
      .map(answerLine)
      .filter((line) => line !== "");
    const confirmed = await confirm({
      title: t("settings.policy.proposals.commitConfirmTitle", { id: row.id }),
      description: [
        get.preview?.describe ?? t("settings.policy.proposals.noPreviewSentence"),
        ...lines,
        t("settings.policy.proposals.digestLine", { digest: shownDigest }),
      ].join("\n"),
      confirmValue: String(row.id),
      confirmValueLabel: t("settings.policy.proposals.confirmValueLabel"),
      confirmLabel: t("settings.policy.proposals.commit"),
    });
    if (!confirmed) return; // decline sends NOTHING
    const result = await action.run(() => rpcTyped("proposal.commit", { id: row.id, digest: shownDigest }), {
      errorTitle: t("settings.policy.proposals.commitFailed"),
    });
    if (result) {
      const lines = [
        ...((result.before?.answers ?? []) as Array<Record<string, unknown>>).map((a) => String(a["render"] ?? a["state"] ?? "")),
        "→",
        ...((result.after?.answers ?? []) as Array<Record<string, unknown>>).map((a) => String(a["render"] ?? a["state"] ?? "")),
        ...(result.warnings ?? []).map((w) => String(w)),
      ].filter((l) => l !== "");
      setOutcome({ kind: "commit", text: lines, effectApplied: result.effectApplied });
      onDecided();
    }
  }, [action, confirm, get, onDecided, row.id, shownDigest, t]);

  const reject = useCallback(async () => {
    const trimmed = reason.trim();
    if (trimmed === "" || trimmed.length > REJECT_MAX) return; // the reason is required
    const result = await action.run(() => rpcTyped("proposal.reject", { id: row.id, reason: trimmed }), {
      errorTitle: t("settings.policy.proposals.rejectFailed"),
    });
    if (result) {
      setOutcome({ kind: "reject", text: [result.proposal.decideReason ?? ""] });
      onDecided();
    }
  }, [action, onDecided, reason, row.id, t]);

  const discard = useCallback(async () => {
    // As S35 had it: the socket's own discard, no ceremony — it loosens nothing.
    const result = await action.run(() => rpcTyped("proposal.discard", { id: row.id }), {
      errorTitle: t("settings.policy.proposals.discardFailed"),
    });
    if (result) onDecided();
  }, [action, onDecided, row.id, t]);

  return (
    <Panel data-testid={`proposal-card-${row.id}`}>
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <Pill tone="slate">{row.kind}</Pill>
          <Pill tone={row.status === "proposed" ? "amber" : "slate"}>{row.status}</Pill>
          {isAgentProposal && (
            <Pill tone="violet" data-testid="proposal-agent-badge">
              {t("settings.policy.proposals.byAgent")}
            </Pill>
          )}
          <span className="font-mono text-xs" title={row.createdAt}>
            {row.createdByKind}:{row.createdById}
          </span>
          <RelativeTime iso={row.createdAt} />
          <span className="ml-auto font-mono text-xs opacity-60">#{row.id}</span>
        </div>

        {/* The reason an agent proposal carries — the daemon's words. */}
        {row.payload.reason ? (
          <p className="text-sm" data-testid={`proposal-reason-${row.id}`}>{row.payload.reason}</p>
        ) : null}

        {/* Live-vs-proposed preview, from proposal.get. */}
        <div className="space-y-1" data-testid={`proposal-preview-${row.id}`}>
          <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.proposals.preview")}</p>
          {preview.loading ? (
            <p className="text-sm opacity-70">{t("settings.policy.proposals.previewLoading")}</p>
          ) : preview.error ? (
            <p className="text-sm text-red-600 dark:text-red-400">{preview.error}</p>
          ) : get?.preview === null || get?.preview === undefined ? (
            <p className="text-sm opacity-70" data-testid={`proposal-preview-none-${row.id}`}>
              {t("settings.policy.proposals.previewNone")}
            </p>
          ) : (
            <>
              {get.preview.describe !== undefined && <p className="text-sm" data-testid="proposal-preview-describe">{get.preview.describe}</p>}
              {get.preview.target !== undefined && <p className="font-mono text-xs">{get.preview.target}</p>}
              <ul className="space-y-1 text-sm">
                {(get.preview.inForce ?? []).map((answer, index) => (
                  <li key={index} className="flex flex-wrap items-center gap-2">
                    <Pill tone={actionTone(answer.state)}>{answer.state}</Pill>
                    <span className="font-mono text-xs">{answer.policyID}</span>
                    <span>{answer.render}</span>
                  </li>
                ))}
              </ul>
              {(() => {
                // `note` is additive on the wire (protocol's ProposalPreview)
                // and the fixture-derived contract never saw one; render it
                // when the daemon sent it, never otherwise.
                const note = (get.preview as { note?: string } | null | undefined)?.note;
                return note !== undefined && note !== "" ? <p className="text-sm opacity-70">{note}</p> : null;
              })()}
            </>
          )}
          {shownDigest !== undefined && (
            <p className="font-mono text-[11px] opacity-60" data-testid={`proposal-digest-${row.id}`}>
              {t("settings.policy.proposals.digestLine", { digest: shownDigest })}
            </p>
          )}
        </div>

        {/* The exact CLI command the row carries — the same command, the
            same implementation; the road is the caller's choice (PM4). */}
        <code className="block font-mono text-[11px] text-slate-500 dark:text-slate-400" data-testid={`proposal-command-${row.id}`}>
          {row.command}
        </code>

        {action.error && <Alert intent="danger" title={action.error} data-testid="proposal-action-error" />}

        {outcome && (
          <div className="space-y-1" data-testid={`proposal-outcome-${row.id}`}>
            <Alert
              intent={outcome.effectApplied === false ? "warning" : "success"}
              title={
                outcome.kind === "commit"
                  ? t("settings.policy.proposals.committed", { effectApplied: outcome.effectApplied === true ? t("settings.policy.proposals.effectAppliedYes") : t("settings.policy.proposals.effectAppliedNo") })
                  : t("settings.policy.proposals.rejected")
              }
            />
            {outcome.text.map((line, index) => (
              <p key={index} className="text-sm" data-testid="proposal-outcome-line">{line}</p>
            ))}
          </div>
        )}

        {row.status === "proposed" && (
          <div className="flex flex-wrap items-end gap-3">
            <Button
              size="sm"
              disabled={!canWrite || action.busy || shownDigest === undefined}
              onClick={() => void commit()}
              title={!canWrite ? t("settings.policy.reconnecting") : undefined}
            >
              {t("settings.policy.proposals.commit")}
            </Button>
            <label className="flex min-w-56 flex-col gap-1 text-sm">
              <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.proposals.rejectReason")}</span>
              <input
                aria-label={t("settings.policy.proposals.rejectReason")}
                maxLength={REJECT_MAX}
                className="rounded-md border border-slate-300 bg-transparent px-2 py-1 text-sm dark:border-slate-700"
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              />
            </label>
            <Button
              size="sm"
              variant="outline"
              color="red"
              // An unreasoned reject never reaches the socket (PM7.5's
              // direction-of-risk rule, honoured at the door too).
              disabled={!canWrite || action.busy || reason.trim() === "" || reason.trim().length > REJECT_MAX}
              onClick={() => void reject()}
              title={!canWrite ? t("settings.policy.reconnecting") : undefined}
            >
              {t("settings.policy.proposals.reject")}
            </Button>
            <Button size="sm" variant="ghost" disabled={!canWrite || action.busy} onClick={() => void discard()}>
              {t("settings.policy.proposals.discard")}
            </Button>
          </div>
        )}
      </div>
      {modal}
    </Panel>
  );
}
