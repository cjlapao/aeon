import { useI18n } from "@cjlapao/ui-kit";
import { Button, Panel } from "../../../components/kit";

// DiffView.tsx — renders the daemon's diff, exactly.
//
// `blueprint.diff` answers with a unified-diff STRING plus its two labels and an
// `equal` flag (T7). This component adds no computation to that text: it does
// not recompute the diff, re-order hunks, or work out what changed — the daemon
// compared two `blueprint_revision` rows and this file draws those bytes with
// the marker characters it was given. Reimplementing the comparison in the
// browser would put a second, weaker diff engine between a person and the row
// history they are deciding against.

export interface DiffViewProps {
  aLabel: string;
  bLabel: string;
  diff: string;
  equal: boolean;
}

/** Presentation only: which colour a line of the daemon's text takes, decided
 *  by its first character. Nothing is added, dropped, or re-parsed. */
export function diffLineKind(line: string): "add" | "remove" | "hunk" | "header" | "context" {
  if (line.startsWith("@@")) return "hunk";
  if (line.startsWith("+++") || line.startsWith("---")) return "header";
  if (line.startsWith("+")) return "add";
  if (line.startsWith("-")) return "remove";
  return "context";
}

const LINE_CLASS: Record<string, string> = {
  add: "text-emerald-300 bg-emerald-400/5",
  remove: "text-rose-300 bg-rose-400/5",
  hunk: "text-sky-300",
  header: "text-slate-400",
  context: "text-slate-300",
};

export function DiffView({ aLabel, bLabel, diff, equal }: DiffViewProps) {
  const { t } = useI18n();
  const lines = diff.length > 0 ? diff.replace(/\n$/, "").split("\n") : [];
  return (
    <div data-testid="diff-view" className="space-y-2">
      <div className="text-xs text-slate-400">
        {aLabel} <span aria-hidden="true">→</span> {bLabel}
      </div>
      {equal ? (
        <div data-testid="diff-equal" className="rounded-lg border border-white/10 bg-white/5 px-3 py-2 text-sm text-slate-300">
          {t("blueprints.diff.equal")}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-white/10 bg-black/30 p-3">
          <pre className="font-mono text-xs leading-5">
            {lines.map((line, index) => (
              <div key={index} data-diff={diffLineKind(line)} className={LINE_CLASS[diffLineKind(line)]}>
                {line === "" ? " " : line}
              </div>
            ))}
          </pre>
        </div>
      )}
    </div>
  );
}

/** The two-revision picker's answer, wrapped in the panel the revisions tab
 *  draws it in. Kept here so the labels and the "nothing changed" wording stay
 *  with the thing that renders them. */
export function DiffPanel(props: DiffViewProps & { onClose?: () => void }) {
  const { t } = useI18n();
  const { onClose } = props;
  return (
    <Panel title={t("blueprints.diff.title")}>
      <div className="space-y-2">
        <DiffView {...props} />
        {onClose && (
          <Button size="sm" variant="ghost" onClick={onClose}>
            {t("blueprints.diff.close")}
          </Button>
        )}
      </div>
    </Panel>
  );
}
