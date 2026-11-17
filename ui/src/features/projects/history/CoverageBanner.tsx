import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Panel, Pill } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import { useHistoryCoverage } from "./historyRpc";
import { coverageSpan, coverageTone, summarizeCoverage } from "./historyModel";

/**
 * CoverageBanner — `history.coverage` rendered as the three states it names
 * (S53 T5, HV1; P31's third state made visible).
 *
 * The three shapes are never collapsed into one line:
 *
 *  • **shown** — the live and archived counts, with the span the view
 *    actually covers;
 *  • **not-shown** — each `notShown` reason with the count it covers, the
 *    daemon's own reason words verbatim (on a step lane that is
 *    `history.NotShownArchivedSteps`: the archived loops the lane, by its
 *    nature, leaves out);
 *  • **cannot-say** — the undecodable archives, as a WARNING that names its
 *    ids. A blob that will not decode is neither shown nor honestly absent,
 *    so it never renders as silence — and never as a count alone: the count
 *    is the truth, the names are the accountability (the daemon caps its own
 *    list at `history.UndecodableIDsListed` for exactly that reason).
 *
 * The floor statement renders VERBATIM and is always present — it is S25's
 * own P32 sentence, and re-wording it would be the second formatter S25/H6
 * forbids. Nothing on this banner is computed here: the counts, the span,
 * the reason words, the ids and the sentence are the daemon's; the only
 * local decision is the colour (`historyModel.coverageTone`), and the word
 * always stands beside it.
 */
export interface CoverageBannerProps {
  draft: { root: string; lane: string; since: string };
  controller: LiveController;
  live: LiveState;
}

export function CoverageBanner({ draft, controller, live }: CoverageBannerProps) {
  const { t } = useI18n();
  const coverage = useHistoryCoverage(draft, controller, live);

  if (coverage.error !== undefined) {
    // A refused coverage read is said, never skipped: a banner that vanished
    // would leave the rows standing unqualified.
    return <Alert intent="danger" title={t("history.manage.coverage.failed")} description={coverage.error} data-testid="history-coverage-failed" />;
  }

  const summary = summarizeCoverage(coverage.data);
  if (summary === undefined) {
    return (
      <p className="text-sm text-slate-500" data-testid="history-coverage-loading">
        {coverage.loading ? t("history.manage.coverage.loading") : t("history.manage.coverage.none")}
      </p>
    );
  }
  const span = coverageSpan(summary);

  return (
    <Panel
      title={t("history.manage.coverage.title")}
      subtitle={summary.wholeSpan ? t("history.manage.coverage.scopeAll") : summary.scope}
      tone={coverageTone(summary.state)}
      data-testid="history-coverage"
      data-coverage-state={summary.state}
    >
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <Pill tone={summary.shown.live.loops > 0 ? "green" : "slate"} data-testid="history-coverage-live">
            {t("history.manage.coverage.live", { count: summary.shown.live.loops })}
          </Pill>
          <Pill tone={summary.shown.archived.loops > 0 ? "green" : "slate"} data-testid="history-coverage-archived">
            {t("history.manage.coverage.archived", { count: summary.shown.archived.loops })}
          </Pill>
          <span className="text-xs text-slate-500" data-testid="history-coverage-span">
            {t("history.manage.coverage.span", { oldest: span.oldest, newest: span.newest })}
          </span>
        </div>

        {summary.state === "warning" ? (
          <Alert
            intent="warning"
            title={t("history.manage.coverage.cannotSay", { count: summary.cannotSay.count })}
            // The ids the probe could not prove, listed (≤ the daemon's own
            // cap of twenty). They ride `description`, not `children`: `Alert`
            // renders one or the other and DISCARDS children when a description
            // is present — the kit's own trap, recorded in
            // `components/ErrorPanel.tsx` and in ui/README. Naming them is the
            // state: P31's third state with a name on it, not a number alone.
            description={
              <div className="space-y-1">
                <p>{t("history.manage.coverage.cannotSayHint")}</p>
                <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs" data-testid="history-coverage-ids">
                  {summary.cannotSay.ids.map((id) => (
                    <li key={id} data-testid={`history-coverage-id-${id}`}>
                      {id}
                    </li>
                  ))}
                </ul>
              </div>
            }
            data-testid="history-coverage-undecodable"
          />
        ) : (
          <p className="text-sm text-slate-500" data-testid="history-coverage-healthy">
            {t("history.manage.coverage.healthy")}
          </p>
        )}

        {summary.notShown.length > 0 && (
          <div data-testid="history-coverage-notshown">
            <p className="text-[11px] uppercase tracking-wide opacity-70">{t("history.manage.coverage.notShown")}</p>
            <ul className="mt-1 space-y-1 text-xs">
              {summary.notShown.map((entry, index) => (
                <li key={`${entry.reason}-${index}`} data-testid={`history-notshown-${index}`}>
                  {/* The reason is the daemon's sentence, verbatim; the count
                      is the number of entries it accounts for. */}
                  <span data-testid={`history-notshown-reason-${index}`}>{entry.reason}</span>
                  {" · "}
                  <span data-testid={`history-notshown-count-${index}`}>{t("history.manage.coverage.notShownCount", { count: entry.count })}</span>
                </li>
              ))}
            </ul>
          </div>
        )}

        <p className="text-xs text-slate-500" data-testid="history-coverage-floor">
          <span className="uppercase tracking-wide opacity-70">{t("history.manage.coverage.floorLabel")}</span>
          {" "}
          {/* Verbatim, always present — never re-worded, never recomputed. */}
          <span data-testid="history-floor-statement">{summary.floor}</span>
        </p>
      </div>
    </Panel>
  );
}
