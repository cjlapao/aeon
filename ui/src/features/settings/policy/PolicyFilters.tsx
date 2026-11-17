import { useI18n } from "@cjlapao/ui-kit";
import { Button, MultiToggle, Select } from "../../../components/kit";
import { AgentTagPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import type { PolicyFilterDraft, Vocabulary } from "./policyModel";

/**
 * The Rules-tab filter bar. Every option list is the vocabulary the daemon
 * answered (PM6) plus the origins the daemon has actually shown on rows;
 * "—" simply omits the filter from the request. Filtering happens in the
 * daemon: this component assembles the **params** (`applyFilters` composes
 * them) and never touches the rows.
 */
export interface PolicyFiltersProps {
  draft: PolicyFilterDraft;
  vocabulary: Vocabulary | undefined;
  /**
   * The origin filter's option list. `policy.vocabulary` carries types,
   * target kinds, sources and actions — origin is the one filter the
   * vocabulary does NOT ship (it is T12's provenance column, not a closed
   * policy set), so the section offers the origins it has seen the daemon
   * send. The UI never names the set itself; the value still travels as a
   * `policy.list` **param** the daemon validates with its own `ParseOrigin`
   * (an unknown one is refused by name, verbatim).
   */
  originOptions: readonly string[];
  onChange: (next: PolicyFilterDraft) => void;
  onClear: () => void;
  controller: LiveController;
  live: LiveState;
}

function OptionSelect({
  label,
  value,
  options,
  onSelect,
}: {
  label: string;
  value: string;
  options: readonly string[];
  onSelect: (value: string) => void;
}) {
  return (
    <label className="flex w-48 shrink-0 flex-col gap-1 text-sm">
      <span className="text-xs uppercase tracking-wide opacity-70">{label}</span>
      <Select aria-label={label} variant="ghost" size="md" value={value} onChange={(event) => onSelect(event.target.value)}>
        <option value="">—</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </Select>
    </label>
  );
}

export function PolicyFilters({ draft, vocabulary, originOptions, onChange, onClear, controller, live }: PolicyFiltersProps) {
  const { t } = useI18n();
  const set = (patch: Partial<PolicyFilterDraft>): void => onChange({ ...draft, ...patch });
  // A target id only makes sense once a kind is chosen; the kind alone is a
  // complete filter (the daemon parses the bare-kind spelling and owns which
  // kinds need an id — a wrong spelling comes back as its refusal, verbatim).
  const showTargetId = draft.targetKind !== "";
  const isFiltering =
    draft.type !== "" ||
    draft.targetKind !== "" ||
    draft.targetId !== "" ||
    draft.source !== "" ||
    draft.action !== "" ||
    draft.origin !== "" ||
    !draft.includeExpired;

  return (
    <div className="flex flex-wrap items-end gap-3" data-testid="policy-filters">
      <OptionSelect
        label={t("settings.policy.filters.type")}
        value={draft.type}
        options={vocabulary?.types ?? []}
        onSelect={(type) => set({ type })}
      />
      <OptionSelect
        label={t("settings.policy.filters.targetKind")}
        value={draft.targetKind}
        options={vocabulary?.targetKinds ?? []}
        onSelect={(targetKind) => set({ targetKind, targetId: "" })}
      />
      {showTargetId && (
        <div className="min-w-56">
          <p className="mb-1 text-xs uppercase tracking-wide opacity-70">{t("settings.policy.filters.targetId")}</p>
          {/* The picker lists roster agents (slug + name) and can only emit a
              real identity; the filter still travels as a daemon param. */}
          <AgentTagPicker value={draft.targetId} onChange={(targetId) => set({ targetId })} controller={controller} live={live} />
        </div>
      )}
      <OptionSelect
        label={t("settings.policy.filters.source")}
        value={draft.source}
        options={vocabulary?.sources ?? []}
        onSelect={(source) => set({ source })}
      />
      <OptionSelect
        label={t("settings.policy.filters.action")}
        value={draft.action}
        options={vocabulary?.actions ?? []}
        onSelect={(action) => set({ action })}
      />
      <OptionSelect
        label={t("settings.policy.filters.origin")}
        value={draft.origin}
        options={originOptions}
        onSelect={(origin) => set({ origin })}
      />
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.filters.expired")}</span>
        <MultiToggle
          options={[
            { value: "hide", label: t("settings.policy.filters.hideExpired") },
            { value: "show", label: t("settings.policy.filters.showExpired") },
          ]}
          value={draft.includeExpired ? "show" : "hide"}
          onChange={(value) => set({ includeExpired: value === "show" })}
          size="sm"
          aria-label={t("settings.policy.filters.expired")}
        />
      </label>
      <Button variant="ghost" size="sm" onClick={onClear} disabled={!isFiltering}>
        {t("settings.policy.filters.clear")}
      </Button>
    </div>
  );
}
