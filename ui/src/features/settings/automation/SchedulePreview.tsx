import { useI18n } from "@cjlapao/ui-kit";
import type { PreviewInstant } from "./automationModel";
import type { PreviewState } from "./automationRpc";

/**
 * SchedulePreview — the live `automation.preview` answer, rendered verbatim
 * (AN11: the daemon parsed the cron, decided DST and the window, and wrote
 * the notes; this renders them).
 *
 * Each instant carries the daemon's own `kind` word — `normal` or a skip —
 * and the UI shows the kind, never an interpretation of it. The daemon's
 * refusal while the field is mid-edit arrives as `error` and sits under the
 * field the person is typing in, in the daemon's words, because client-side
 * cron parsing is NOT done here and a half-typed line deserves the truth.
 */
export function SchedulePreview({ preview }: { preview: PreviewState }) {
  const { t } = useI18n();
  if (preview.error !== undefined) {
    return (
      <p className="text-xs text-rose-500" data-testid="preview-refusal">
        {preview.error}
      </p>
    );
  }
  if (preview.data === undefined) {
    // Two literal t() keys, never a computed one — the i18n guard scans
    // literals and a dynamic key would hide a catalog gap from it.
    return preview.pending ? (
      <p className="text-xs opacity-60" data-testid="preview-none">{t("settings.automation.preview.pending")}</p>
    ) : (
      <p className="text-xs opacity-60" data-testid="preview-none">{t("settings.automation.preview.none")}</p>
    );
  }
  return (
    <div className="space-y-1" data-testid="schedule-preview">
      {preview.data.instants.map((instant, index) => (
        <InstantLine key={index} instant={instant} />
      ))}
    </div>
  );
}

function InstantLine({ instant }: { instant: PreviewInstant }) {
  // The wall text and the kind are the daemon's; "next" framing is the only
  // word this component adds, and it frames, it never restates.
  return (
    <p className="font-mono text-xs" data-testid={`preview-instant-${instant.kind}`}>
      {instant.wall} <span className="opacity-60">({instant.at})</span>{" "}
      <span className="opacity-80">— {instant.kind}</span>
    </p>
  );
}
