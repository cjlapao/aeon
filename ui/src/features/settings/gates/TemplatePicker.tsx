import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill, Toggle } from "../../../components/kit";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import { useTemplates } from "./gatesRpc";

/**
 * The template picker, as the content of a side panel: `verify.templates` answered by stack, each
 * row the daemon's own name/command/requirement/timeout — the picker selects IDs, nothing else, and
 * the applied gates are whatever `verify.applyTemplate` answers. A collision refusal (all-or-nothing,
 * nothing half-applied) arrives through the parent's error and renders verbatim, with the selection
 * kept so the person can drop the clashing template and try again.
 */
type TemplateRow = RpcMethods["verify.templates"]["result"]["templates"][number];

export interface TemplatePickerProps {
  controller: LiveController;
  live: LiveState;
  busy: boolean;
  canWrite: boolean;
  onApply: (templateIds: string[]) => void;
  onClose: () => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function TemplatePicker({ controller, live, busy, canWrite, onApply, onClose }: TemplatePickerProps) {
  const { t } = useI18n();
  const query = useTemplates(controller, live);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());

  const templates = query.data?.templates ?? [];
  const stacks = useMemo(() => {
    const grouped = new Map<string, TemplateRow[]>();
    for (const template of templates) {
      const list = grouped.get(template.stack) ?? [];
      list.push(template);
      grouped.set(template.stack, list);
    }
    return grouped;
  }, [templates]);

  const toggle = (id: string) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <div className="space-y-8" data-testid="gates-templates">
      <p className="text-sm opacity-80">{t("settings.gates.templates.subtitle")}</p>
      {query.error && <Alert intent="danger" title={t("settings.gates.templates.failed")} description={query.error} data-testid="gates-templates-failed" />}
      {!query.error && !query.loading && templates.length === 0 && <p className="text-sm opacity-70" data-testid="gates-templates-empty">{t("settings.gates.templates.empty")}</p>}
      {[...stacks.entries()].map(([stack, rows]) => (
        <section key={stack} className="space-y-3" data-testid={`gates-templates-stack-${stack}`}>
          <h3 className={H}>{stack}</h3>
          <ul className="space-y-3">
            {rows.map((template) => (
              <li key={template.id} className="space-y-1" data-testid={`gates-template-${template.id}`}>
                <Toggle size="sm" aria-label={template.name} label={template.name} checked={selected.has(template.id)} disabled={busy || !canWrite} onChange={() => toggle(template.id)} />
                <div className="flex flex-wrap items-center gap-2 pl-1">
                  <code className="text-xs opacity-70">{template.command}</code>
                  <Pill size="sm" tone="slate">{t(`settings.gates.word.${template.requirement}`)}</Pill>
                  <span className="text-xs opacity-70">{t("settings.gates.row.timeout", { seconds: template.timeoutS })}</span>
                </div>
              </li>
            ))}
          </ul>
        </section>
      ))}
      <div className="flex gap-2">
        <Button size="sm" onClick={() => onApply([...selected])} disabled={busy || !canWrite || selected.size === 0} data-testid="gates-templates-apply">{t("settings.gates.templates.apply", { count: selected.size })}</Button>
        <Button size="sm" variant="ghost" onClick={onClose} disabled={busy} data-testid="gates-templates-close">{t("settings.gates.templates.close")}</Button>
      </div>
    </div>
  );
}
