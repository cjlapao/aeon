import { useI18n } from "@cjlapao/ui-kit";
import { Link } from "react-router-dom";
import { Button, Panel, Pill } from "../../../components/kit";
import type { HistoryRow } from "./historyModel";
import { rowRender } from "./historyModel";

/**
 * HistoryRows — the row table (S53 T5, HV2/HV7).
 *
 * Every word on a row is the daemon's. The lane and outcome words are the
 * row's own `describe` fields (`history.Describe`, the ONE formatter), the
 * cost is `describe.cost` — a floor, never a bare number (H6) — with its
 * honesty word beside it, and a blocked loop's reason is the stored reason
 * verbatim (D4/H4). Nothing here re-phrases, groups, re-sorts or re-decides:
 * this is the fifth surface reading the same rows the CLI and the socket
 * answer, and a row's shape is not this screen's to improve.
 *
 * The links are the row's `describe.links` — `{kind, id}` as DATA (HV2). A
 * kind this app has a screen for becomes a link; one it has no screen for
 * renders as the plain words it is, never as a link that goes nowhere. The
 * kind word a person reads is the daemon's own `link.kind`; the app's route
 * table (`historyModel.LINK_ROUTES`) only decides where it goes.
 */
export interface HistoryRowsProps {
  rows: HistoryRow[];
  openId: string | null;
  onOpen: (id: string) => void;
}

export function HistoryRows({ rows, openId, onOpen }: HistoryRowsProps) {
  const { t } = useI18n();
  return (
    <div className="space-y-2" data-testid="history-rows">
      {rows.map((row) => {
        const view = rowRender(row);
        const open = openId === view.id;
        return (
          <Panel
            key={view.id}
            title={view.title === "" ? view.slug || view.id : view.title}
            subtitle={`${view.id} · ${view.kind} · ${view.timeline}`}
            tone={open ? "blue" : "slate"}
            data-testid={`history-row-${view.id}`}
          >
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2 text-xs" data-testid={`history-describe-${view.id}`}>
                {/* The lane word and the outcome word: both served by
                    history.vocabulary, both rendered as this row's own bytes. */}
                <Pill tone="slate" data-testid={`history-lane-${view.id}`}>
                  <span className="opacity-70">{t("history.manage.row.lane")}</span> {view.lane}
                </Pill>
                <Pill tone="slate" data-testid={`history-outcome-${view.id}`}>
                  <span className="opacity-70">{t("history.manage.row.outcome")}</span> {view.outcome}
                </Pill>
                {/* The cost is a floor (H6) and its honesty state rides beside
                    it as the daemon named it — an unknown word is its own dot,
                    never a rename. */}
                <Pill tone={view.costTone} data-testid={`history-cost-${view.id}`}>
                  <span className="opacity-70">{t("history.manage.row.cost")}</span> {view.cost}
                  {" · "}
                  <span data-testid={`history-cost-state-${view.id}`}>{view.costState}</span>
                </Pill>
                {view.duration !== "" && (
                  <Pill tone="slate" data-testid={`history-duration-${view.id}`}>
                    <span className="opacity-70">{t("history.manage.row.duration")}</span> {view.duration}
                  </Pill>
                )}
              </div>

              {view.reason !== "" && (
                <p className="text-sm text-slate-500" data-testid={`history-reason-${view.id}`}>
                  <span className="uppercase tracking-wide opacity-70">{t("history.manage.row.reason")}</span> {view.reason}
                </p>
              )}

              {view.links.length > 0 && (
                <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs" data-testid={`history-links-${view.id}`}>
                  {view.links.map((link) => (
                    <li key={`${link.kind}-${link.id}`} data-testid={`history-link-${view.id}-${link.kind}`}>
                      <span className="opacity-70">{link.kind}</span>{" "}
                      {link.href === undefined ? (
                        // No screen for this kind yet: the words, not a dead link.
                        <span>{link.id}</span>
                      ) : (
                        <Link to={link.href} className="aeon-accent-text underline">
                          {link.id}
                        </Link>
                      )}
                    </li>
                  ))}
                </ul>
              )}

              {/* The drill-down the screen opens beside the table; while a row
                  is open its own button is gone (the drill-down carries the
                  way back), so one control is always exactly one action. */}
              {!open && (
                <Button size="sm" variant="soft" data-testid={`history-open-${view.id}`} onClick={() => onOpen(view.id)}>
                  {t("history.manage.row.show")}
                </Button>
              )}
            </div>
          </Panel>
        );
      })}
    </div>
  );
}
