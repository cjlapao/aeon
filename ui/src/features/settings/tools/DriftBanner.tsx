import { useI18n } from "@cjlapao/ui-kit";
import { Alert, CollapsiblePanel, Pill } from "../../../components/kit";
import { driftTone, type DriftSummary } from "./toolsModel";

// The drift line above the catalog: said out loud, never repaired. When the daemon's drift list is
// empty the banner is QUIET — one muted line naming the catalog version. When it is not, every entry
// is listed with the tool's name, the state the daemon sent and the FIELD NAMES it reported.
// There is no control: `tools.status` reports drift and repairing it is `aeon seed apply`, a
// deliberate act on the CLI, never a side effect of looking.

export interface DriftBannerProps {
  summary: DriftSummary;
  loading: boolean;
  error?: string;
}

export function DriftBanner({ summary, loading, error }: DriftBannerProps) {
  const { t } = useI18n();

  if (loading) return <p className="shrink-0 text-xs opacity-60" data-testid="tools-drift-loading">{t("settings.tools.drift.loading")}</p>;
  if (error) return <Alert className="shrink-0" intent="warning" title={t("settings.tools.drift.unreadable")} description={error} data-testid="tools-drift-error" />;
  if (summary.kind === "unknown") return <p className="shrink-0 text-xs opacity-60" data-testid="tools-drift-unknown">{t("settings.tools.drift.unknown")}</p>;
  if (summary.kind === "current") {
    return <p className="shrink-0 text-xs opacity-60" data-testid="tools-drift-current">{t("settings.tools.drift.current", { version: summary.version, set: summary.set })}</p>;
  }
  return (
    <CollapsiblePanel
      className="shrink-0"
      tone="amber"
      data-testid="tools-drift"
      title={t("settings.tools.drift.title")}
      subtitle={`${summary.lines.map((l) => l.name).join(", ")} · ${t("settings.tools.drift.subtitle", { set: summary.set, state: summary.state })}`}
      defaultExpanded={false}
    >
      <ul className="space-y-1">
        {summary.lines.map((line) => (
          <li key={line.name} className="flex flex-wrap items-center gap-2 text-sm" data-testid={`tools-drift-row-${line.name}`}>
            <span className="font-medium">{line.name}</span>
            <Pill tone={driftTone(line.state)} size="sm">{line.state}</Pill>
            {line.fields.length > 0 && (
              <span className="text-xs opacity-70" data-testid={`tools-drift-fields-${line.name}`}>{t("settings.tools.drift.fields", { fields: line.fields.join(", ") })}</span>
            )}
            {/* The caption is chosen by the state string the daemon sent, so this file never holds the
                drift vocabulary; a state nobody translated falls back to the plain state word. */}
            <span className="text-xs opacity-70">{t(`settings.tools.drift.states.${line.state}`, { name: line.name, fields: line.fields.join(", ") })}</span>
          </li>
        ))}
      </ul>
      <p className="mt-2 text-xs opacity-70" data-testid="tools-drift-note">{t("settings.tools.drift.note")}</p>
    </CollapsiblePanel>
  );
}
