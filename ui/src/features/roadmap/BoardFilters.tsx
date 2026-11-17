import { useI18n } from "@cjlapao/ui-kit";
import { FormField, Input, Panel, Select } from "../../components/kit";
import type { FilterDraft, Vocabulary } from "./roadmapModel";

/**
 * BoardFilters — the three dials of the management board (S50 T7, RM6).
 *
 * The component owns INPUTS and nothing else: it fetches nothing, filters
 * nothing client-side and hides nothing it was handed. The draft travels up
 * to the page, the page composes `roadmap.board` PARAMS from it, and the
 * DAEMON filters — a board that swept a fetched list would be a second,
 * smaller truth about what matches.
 *
 * The status dial is the vocabulary's OWN statuses array (every daemon
 * status, none hardcoded, the terminal ones included — a cancelled item is
 * still an item); the epic is free text because the epic is a tag, not a
 * row (R2), and the component root is required because the board answers
 * ONE root at a time.
 */
export interface BoardFiltersProps {
  draft: FilterDraft;
  vocabulary: Vocabulary | undefined;
  busy?: boolean;
  onChange: (next: FilterDraft) => void;
}

export function BoardFilters({ draft, vocabulary, busy = false, onChange }: BoardFiltersProps) {
  const { t } = useI18n();
  return (
    <Panel tone="slate" data-testid="board-filters">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <FormField label={t("roadmap.manage.filters.root")} hint={t("roadmap.manage.filters.rootHint")}>
          <Input
            value={draft.componentRoot}
            disabled={busy}
            placeholder="/srv/repos/ledger"
            aria-label={t("roadmap.manage.filters.root")}
            data-testid="board-filter-root"
            onChange={(e) => onChange({ ...draft, componentRoot: e.target.value })}
          />
        </FormField>
        <FormField label={t("roadmap.manage.filters.status")}>
          <Select
            value={draft.status}
            disabled={busy}
            aria-label={t("roadmap.manage.filters.status")}
            data-testid="board-filter-status"
            onChange={(e) => onChange({ ...draft, status: e.target.value })}
          >
            <option value="">{t("roadmap.manage.filters.statusAll")}</option>
            {(vocabulary?.statuses ?? []).map((entry) => (
              // The vocabulary's array, in the daemon's order, words as the
              // daemon spelled them — a terminal status stays offerable,
              // because listing a cancelled item is not a state change.
              <option key={entry.status} value={entry.status}>
                {entry.status}
              </option>
            ))}
          </Select>
        </FormField>
        <FormField label={t("roadmap.manage.filters.epic")}>
          <Input
            value={draft.epic}
            disabled={busy}
            placeholder={t("roadmap.manage.filters.epicPlaceholder")}
            aria-label={t("roadmap.manage.filters.epic")}
            data-testid="board-filter-epic"
            onChange={(e) => onChange({ ...draft, epic: e.target.value })}
          />
        </FormField>
      </div>
    </Panel>
  );
}
