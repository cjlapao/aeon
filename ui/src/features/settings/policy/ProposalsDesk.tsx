import { useCallback, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { RpcError } from "../../../lib/bridge";
import { useRpcQuery } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import { ProposalCard } from "./ProposalCard";
import type { ProposalListParamsWire, ProposalRow } from "./policyModel";
import type { RpcMethods } from "../../../proto/contract";

/**
 * The proposals desk — the Settings surface's decisions (PM1/PM12), the
 * counterpart of the bench `ProposalsPage`, which stays exactly as TD7 left
 * it (no commit affordance there; the desk here is the human door the
 * operator's ruling opened, and it is a different page).
 *
 * `proposal.list` arrives filtered in the daemon by its `status` param
 * (default `proposed` — the desk opens on the open pile); the status list is
 * not a client-side table, the value simply travels as a param.
 */
export interface ProposalsDeskProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  /**
   * S46 T11: the MCP section reuses THIS desk (not a copy) narrowed to the
   * kinds it owns — an additive optional filter over the rows the daemon
   * answered; absent, the desk shows every kind exactly as S45 shipped it.
   * (`proposal.list` filters by status daemon-side; there is no daemon kind
   * param, so this one filter is honestly client-side.)
   */
  kinds?: readonly string[];
}

/** The desk's default pile. A param on the wire (the door filters), not a
 *  client-side predicate. */
const DEFAULT_STATUS = "proposed";

export function ProposalsDesk({ controller, live, canWrite, kinds }: ProposalsDeskProps) {
  const { t } = useI18n();
  const [status, setStatus] = useState(DEFAULT_STATUS);

  const wireParams: ProposalListParamsWire = { status };
  // The contract types this method's params as the empty object (no fixture
  // ever carried the status filter); the wire field is real (protocol's
  // proposalListParams) and travels as given — one honest cast at the edge.
  const list = useRpcQuery("proposal.list", wireParams as RpcMethods["proposal.list"]["params"], controller, live);

  const rows = useMemo<ProposalRow[]>(() => {
    const all = Array.isArray(list.data?.proposals) ? (list.data?.proposals as unknown as ProposalRow[]) : [];
    return kinds === undefined ? all : all.filter((row) => kinds.includes(row.kind));
  }, [list.data, kinds]);

  const refresh = useCallback(() => list.refetch(), [list]);

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold">{t("settings.policy.proposals.title")}</h2>
        <p className="mt-1 text-sm opacity-70">{t("settings.policy.proposals.subtitle")}</p>
      </div>
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.proposals.status")}</span>
          <input
            aria-label={t("settings.policy.proposals.status")}
            className="rounded-md border border-slate-300 bg-transparent px-2 py-1 text-sm dark:border-slate-700"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          />
        </label>
        <Button size="sm" variant="ghost" onClick={refresh}>
          {t("settings.policy.refresh")}
        </Button>
      </div>

      {!canWrite && <Alert intent="warning" title={t("settings.policy.reconnecting")} className="mb-3" />}
      {list.loading ? (
        <EmptyState title={t("settings.policy.proposals.loading")} />
      ) : list.error ? (
        <ErrorPanel error={new RpcError(0, list.error)} onRetry={refresh} />
      ) : rows.length === 0 ? (
        <EmptyState title={t("settings.policy.proposals.empty", { status })} />
      ) : (
        <div className="space-y-4" data-testid="proposals-desk">
          {rows.map((row) => (
            <ProposalCard key={row.id} row={row} controller={controller} live={live} canWrite={canWrite} onDecided={refresh} />
          ))}
        </div>
      )}
    </section>
  );
}
