import { useI18n } from "@cjlapao/ui-kit";
import { Button, SearchBar, Select } from "../../../components/kit";
import { toolsFilterIsEmpty, emptyToolsFilter, type FilterOptions, type ToolsFilterState } from "./toolsModel";

// The Tools tab's action bar. Every control here chooses a VALUE the daemon offered and sends it
// as a PARAM — the matching happens on the daemon side and the rows shown are its answer. The
// option lists are the daemon's too (`tools.vocabulary`): until it answers, the selects are empty
// and disabled — the loading state, rather than a guess typed in this file.

export interface ToolFiltersProps {
  value: ToolsFilterState;
  onChange: (next: ToolsFilterState) => void;
  options: FilterOptions;
  loading: boolean;
}

function Pick({ label, value, values, disabled, onPick }: { label: string; value: string; values: string[]; disabled: boolean; onPick: (v: string) => void }) {
  const { t } = useI18n();
  return (
    <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label={label} value={value} disabled={disabled || values.length === 0} onChange={(e) => onPick(e.target.value)}>
      <option value="">{t("settings.tools.filters.anyOf", { what: label })}</option>
      {values.map((v) => <option key={v} value={v}>{v}</option>)}
    </Select></div>
  );
}

export function ToolFilters({ value, onChange, options, loading }: ToolFiltersProps) {
  const { t } = useI18n();
  const set = (patch: Partial<ToolsFilterState>) => onChange({ ...value, ...patch });

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="tools-filters">
      <label className="block w-full max-w-sm">
        <span className="sr-only">{t("settings.tools.filters.search")}</span>
        <SearchBar placeholder={t("settings.tools.filters.searchPlaceholder")} debounceMs={200} initialValue={value.q} onSearch={(q) => set({ q })} onClear={() => set({ q: "" })} />
      </label>
      <Pick label={t("settings.tools.filters.family")} value={value.family} values={options.families} disabled={loading} onPick={(family) => set({ family })} />
      <Pick label={t("settings.tools.filters.source")} value={value.source} values={options.sources} disabled={loading} onPick={(source) => set({ source })} />
      <Pick label={t("settings.tools.filters.capability")} value={value.capability} values={options.capabilities} disabled={loading} onPick={(capability) => set({ capability })} />
      <Pick label={t("settings.tools.filters.access")} value={value.access} values={options.accesses} disabled={loading} onPick={(access) => set({ access })} />
      <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label={t("settings.tools.filters.available")} value={value.available} disabled={loading} onChange={(e) => set({ available: e.target.value })}>
        <option value="">{t("settings.tools.filters.anyOf", { what: t("settings.tools.filters.available") })}</option>
        <option value="true">{t("settings.tools.filters.available")}: {t("settings.tools.filters.availableYes")}</option>
        <option value="false">{t("settings.tools.filters.available")}: {t("settings.tools.filters.availableNo")}</option>
      </Select></div>
      {!toolsFilterIsEmpty(value) && <Button size="sm" variant="ghost" onClick={() => onChange(emptyToolsFilter())}>{t("settings.tools.filters.clear")}</Button>}
    </div>
  );
}
