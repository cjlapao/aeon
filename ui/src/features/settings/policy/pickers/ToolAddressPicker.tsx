import { useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Input } from "../../../../components/kit";
import { LiveController, type LiveState } from "../../../../lib/live";
import { useRpcQuery } from "../../../../lib/useRpcQuery";
import type { RpcMethods } from "../../../../proto/contract";

// The tool-address picker (S74 T3, TA11–TA12). The operator never types a tool
// from memory: the catalog (`tools.list`) supplies every tool with its address,
// and the pattern rows ("all read tools in workspace") are built from the same
// categories. What a pattern names is NOT computed here — `tools.matchPattern`
// answers, with the one matcher the gate uses, so the preview cannot disagree
// with the decision (TA7).

type Tool = RpcMethods["tools.list"]["result"]["tools"][number];

const fallbackController = new LiveController();

export interface ToolAddressPickerProps {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  controller?: LiveController;
  live?: LiveState;
}

interface Row { value: string; label: string }
interface Group { label: string; rows: Row[] }

/** The wildcard rows and the tool rows, grouped by category. */
export function buildGroups(tools: readonly Tool[], t: (key: string) => string): Group[] {
  const cats = [...new Set(tools.map((x) => x.category ?? x.family).filter(Boolean))].sort();
  const groups: Group[] = [{
    label: t("settings.policy.form.toolPicker.all"),
    rows: [
      { value: "*.*.*", label: t("settings.policy.form.toolPicker.allTools") },
      { value: "*.*.read", label: t("settings.policy.form.toolPicker.allRead") },
      { value: "*.*.write", label: t("settings.policy.form.toolPicker.allWrite") },
      // A three-segment pattern never reaches an MCP tool; these name them.
      ...(tools.some((x) => (x.address ?? "").split(".").length === 4)
        ? [{ value: "mcp.*.*.*", label: t("settings.policy.form.toolPicker.allMcp") }]
        : []),
    ],
  }];
  for (const cat of cats) {
    // A category row stands for its tools: an MCP tool's category is `mcp` but
    // its address has four segments, so its group offers only the tools.
    const system = tools.some((x) => (x.category ?? x.family) === cat && (x.address ?? "").split(".").length === 3);
    const rows: Row[] = system
      ? [
          { value: `${cat}.*.*`, label: `${cat}.*.*` },
          { value: `${cat}.*.read`, label: `${cat}.*.read` },
          { value: `${cat}.*.write`, label: `${cat}.*.write` },
        ]
      : [];
    for (const tool of tools) {
      if ((tool.category ?? tool.family) === cat && tool.address) {
        rows.push({ value: tool.address, label: `${tool.address}` });
      }
    }
    groups.push({ label: cat, rows });
  }
  return groups;
}

export function ToolAddressPicker({ value, onChange, disabled = false, controller = fallbackController, live }: ToolAddressPickerProps) {
  const { t } = useI18n();
  const effectiveLive = live ?? controller.getState();
  const { data, loading, error } = useRpcQuery("tools.list", {}, controller, effectiveLive);
  const groups = useMemo(() => buildGroups(data?.tools ?? [], t), [data, t]);

  return (
    <div className="space-y-2" data-testid="tool-address-picker">
      <select
        aria-label={t("settings.policy.form.toolPicker.label")}
        className="w-full rounded-md border border-slate-300 bg-transparent px-2 py-1 text-sm dark:border-slate-700"
        value=""
        disabled={disabled || loading || error !== undefined}
        onChange={(event) => event.target.value !== "" && onChange(event.target.value)}
      >
        <option value="">{loading ? t("settings.policy.form.toolPicker.loading") : t("settings.policy.form.toolPicker.choose")}</option>
        {groups.map((g) => (
          <optgroup key={g.label} label={g.label}>
            {g.rows.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
          </optgroup>
        ))}
      </select>
      {error !== undefined && (
        <p className="text-xs text-amber-600" data-testid="tool-picker-error">{t("settings.policy.form.toolPicker.catalogError")}: {error}</p>
      )}
      <Input
        aria-label="toolName"
        value={value}
        disabled={disabled}
        placeholder="workspace.*.read"
        onChange={(event) => onChange(event.target.value)}
      />
      <MatchPreview pattern={value} controller={controller} live={effectiveLive} />
    </div>
  );
}

/** What the pattern names today, debounced so a keystroke is not a call. */
function MatchPreview({ pattern, controller, live }: { pattern: string; controller: LiveController; live: LiveState }) {
  const { t } = useI18n();
  const [settled, setSettled] = useState(pattern);
  useEffect(() => {
    const id = setTimeout(() => setSettled(pattern), 250);
    return () => clearTimeout(id);
  }, [pattern]);
  if (settled.trim() === "") return null;
  return <MatchResult pattern={settled} stale={settled !== pattern} controller={controller} live={live} t={t} />;
}

function MatchResult({ pattern, stale, controller, live, t }: {
  pattern: string; stale: boolean; controller: LiveController; live: LiveState; t: (k: string, v?: Record<string, unknown>) => string;
}) {
  const { data, error } = useRpcQuery("tools.matchPattern", { pattern }, controller, live);
  if (error !== undefined) {
    // The boundary's own sentence — a refused pattern says why (TA6).
    return <p className="text-xs text-red-600" data-testid="tool-match-refused">{error}</p>;
  }
  if (!data || stale) return <p className="text-xs opacity-60">…</p>;
  if (data.count === 0) {
    return <p className="text-xs text-amber-600" data-testid="tool-match-none">{t("settings.policy.form.toolPicker.matchesNone")}</p>;
  }
  const shown = data.matches.slice(0, 6).join(", ");
  return (
    <p className="text-xs text-slate-500 dark:text-slate-400" data-testid="tool-match-count">
      {t("settings.policy.form.toolPicker.matches", { count: data.count })}: {shown}{data.count > 6 ? ", …" : ""}
    </p>
  );
}
