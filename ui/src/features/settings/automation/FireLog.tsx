import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill, Select } from "../../../components/kit";
import { RelativeTime } from "../../../components/RelativeTime";
import type { LiveController, LiveState } from "../../../lib/live";
import { useAutomationFires } from "./automationRpc";
import { isOutcome, outcomePill, NO_FIRE_PAGE, type FirePage, type Vocabulary } from "./automationModel";

/**
 * FireLog — `automation.fires` across ALL families (AN16): the five outcomes keep five distinct
 * pills, and `could-not-say` keeps its own — it is not a skip and never folds into one; the row's
 * reason sentence is the daemon's.
 *
 * The counts line is the daemon's `counts` object restated word for word — including
 * `could-not-say`, whose count is information about the RECORDS, not about the automation, and
 * disappears the moment a UI merges it with another column. Paging is the daemon's cursor
 * (`nextBeforeId`), used opaquely; the log is newest-first and a page never re-reads a page.
 */
export interface FireLogProps {
  controller: LiveController;
  live: LiveState;
  vocabulary: Vocabulary | undefined;
  /** When set, the per-automation view (source+id travel as params). */
  scope?: { source: string; id: string };
  embedded?: boolean;
}

const PAGE_SIZE = 25;

export function FireLog({ controller, live, vocabulary, scope, embedded = false }: FireLogProps) {
  const { t } = useI18n();
  const [outcome, setOutcome] = useState("");
  const [page, setPage] = useState<FirePage>(NO_FIRE_PAGE);

  const query = useAutomationFires({ source: scope?.source ?? "", outcome, limit: PAGE_SIZE, page }, controller, live);
  const rows = query.data?.fires ?? [];
  const counts = query.data?.counts as (typeof query.data extends undefined ? never : NonNullable<typeof query.data>["counts"]) | undefined;
  const nextBeforeId = typeof query.data?.nextBeforeId === "string" ? query.data.nextBeforeId : "";

  return (
    <div className="flex flex-col gap-3" data-testid={embedded ? "fire-log-embedded" : "fire-log"}>
      {scope === undefined && (
        <div className="flex flex-wrap items-center gap-3">
          <div className="w-56 shrink-0">
            <Select
              variant="ghost"
              size="md"
              aria-label={t("settings.automation.firelog.outcomeFilter")}
              value={outcome}
              onChange={(e) => { setOutcome(e.target.value); setPage(NO_FIRE_PAGE); }}
              data-testid="firelog-outcome-filter"
            >
              <option value="">{t("settings.automation.firelog.outcomeFilter")}: {t("settings.automation.firelog.allOutcomes")}</option>
              {(vocabulary?.outcomes ?? []).map((entry) => <option key={`${entry.family}:${entry.word}`} value={entry.word}>{entry.word}</option>)}
            </Select>
          </div>
          <Button size="sm" variant="ghost" onClick={() => { query.refetch(); setPage(NO_FIRE_PAGE); }}>{t("settings.automation.firelog.reload")}</Button>
        </div>
      )}

      {counts !== undefined && (
        <div className="flex flex-wrap gap-1.5" data-testid="firelog-counts">
          {(["started", "refused", "limited", "skipped", "could-not-say"] as const).map((word) => {
            const pill = outcomePill(word);
            // The wire spells the fifth count couldNotSay (camel, like every field on that struct);
            // the PILL word stays the wire outcome "could-not-say" — five pills, five counts, never merged.
            const held = word === "could-not-say" ? counts.couldNotSay : counts[word];
            return <Pill key={word} size="sm" tone={pill.tone} data-testid={`fire-count-${word}`}>{t(`settings.automation.outcome.${word}`)}: {held}</Pill>;
          })}
        </div>
      )}

      {query.error !== undefined ? (
        <Alert intent="danger" title={query.error} data-testid="firelog-error" />
      ) : rows.length === 0 ? (
        <EmptyState showIcon={false} title={t("settings.automation.firelog.empty")} subtitle={t("settings.automation.firelog.emptyBody")} data-testid="firelog-empty" />
      ) : (
        <ul className="divide-y divide-[var(--border)]">
          {rows.map((row) => {
            const pill = outcomePill(row.outcome);
            return (
              <li key={`${row.source}:${row.fireId}`} className="space-y-1 py-2" data-testid={`fire-row-${row.fireId}`}>
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <Pill size="sm" tone="slate">{row.source}</Pill>
                  {/* The daemon's word on the pill even when it is not one of the five — unrecognised
                      words render verbatim in the could-not-say colour; renaming them would be guessing. */}
                  <Pill size="sm" tone={pill.tone} data-testid={`fire-outcome-${row.fireId}`}>{isOutcome(row.outcome) ? t(`settings.automation.outcome.${row.outcome}`) : row.outcome}</Pill>
                  <code className="text-xs">{row.triggerId}</code>
                  <span className="text-xs opacity-70"><RelativeTime iso={row.at} /></span>
                  {row.loopSlug !== undefined && row.loopSlug !== "" && <code className="text-xs opacity-70">→ {row.loopSlug}</code>}
                </div>
                {row.reason !== "" && <p className="text-xs opacity-80">{row.reason}</p>}
              </li>
            );
          })}
        </ul>
      )}

      {nextBeforeId !== "" && rows.length >= PAGE_SIZE && (
        <div><Button size="sm" variant="ghost" onClick={() => setPage({ beforeId: nextBeforeId })} data-testid="firelog-more">{t("settings.automation.firelog.older")}</Button></div>
      )}
    </div>
  );
}
