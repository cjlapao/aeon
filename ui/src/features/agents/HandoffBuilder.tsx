import { useEffect, useRef, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, IconButton, Input, Pill, Toggle } from "../../components/kit";
import { handoffJson, handoffRows, type HandoffRow } from "./agentsModel";

// The handoff builder: what an agent must report when it finishes, one field per line — its name,
// what it is for, and whether the agent may leave it out. It replaces a JSON box nobody wants to
// type: the row still stores the object the daemon reads ({"summary": "…", "links?": "…"}, a "?" on
// the name marking the field optional), but the person never sees the question mark.
//
// The rows are held here, not derived from the JSON each time, so a line that has no name yet (just
// added) stays on screen while it is being typed; the parent hears the JSON of the named lines.

export interface HandoffBuilderProps {
  /** The row's `handoff` column: a JSON object, "{}" or "". */
  value: string;
  /** The new column value, as the JSON the daemon stores. */
  onChange: (json: string) => void;
  disabled?: boolean;
  testId?: string;
}

/** Name, purpose, the optional switch, the remove button: a line is a grid so no control is ever pushed out of view. */
const GRID = "grid grid-cols-[9rem_minmax(0,1fr)_4.5rem_2rem] items-start gap-2";
const HEAD = "px-1 text-xs uppercase tracking-widest opacity-60";

/** The fields the shipped agents most often report, offered as one-click starts. The daemon still checks whatever is saved. */
interface SuggestedField {
  name: string;
  purposeKey: string;
  optional: boolean;
}
const SUGGESTED: readonly SuggestedField[] = [
  { name: "summary", purposeKey: "agents.handoff.suggestSummary", optional: false },
  { name: "tests_added", purposeKey: "agents.handoff.suggestTests", optional: true },
  { name: "open_questions", purposeKey: "agents.handoff.suggestQuestions", optional: true },
];

interface KeyedRow extends HandoffRow {
  key: number;
}

export function HandoffBuilder({ value, onChange, disabled = false, testId = "agent-handoff-builder" }: HandoffBuilderProps) {
  const { t } = useI18n();
  const next = useRef(0);
  const keyed = (rows: HandoffRow[]): KeyedRow[] => {
    const out: KeyedRow[] = [];
    for (const row of rows) out.push({ ...row, key: next.current++ });
    return out;
  };
  const [rows, setRows] = useState<KeyedRow[]>(() => keyed(handoffRows(value)));

  // A change that came from outside (a reload, a revert) replaces the lines; one this builder made
  // itself is already on screen.
  useEffect(() => {
    if (value.trim() === "" && handoffJson(rows) === "{}") return;
    if (handoffJson(rows) !== normalized(value)) setRows(keyed(handoffRows(value)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  const commit = (nextRows: KeyedRow[]) => {
    setRows(nextRows);
    onChange(handoffJson(nextRows));
  };
  const edit = (key: number, change: Partial<HandoffRow>) => {
    const out: KeyedRow[] = [];
    for (const row of rows) out.push(row.key === key ? { ...row, ...change } : row);
    commit(out);
  };
  const remove = (key: number) => {
    const out: KeyedRow[] = [];
    for (const row of rows) if (row.key !== key) out.push(row);
    commit(out);
  };
  const add = () => commit([...rows, { key: next.current++, name: "", purpose: "", optional: false }]);

  const taken = new Set<string>();
  for (const row of rows) taken.add(row.name.trim());
  const offered: SuggestedField[] = [];
  for (const field of SUGGESTED) if (!taken.has(field.name)) offered.push(field);
  const addSuggested = (field: SuggestedField) =>
    commit([...rows.filter((row) => row.name.trim() !== "" || row.purpose.trim() !== ""), { key: next.current++, name: field.name, purpose: t(field.purposeKey), optional: field.optional }]);

  return (
    <div className="space-y-3" data-testid={testId}>
      {rows.length === 0 && <p className="text-sm opacity-60" data-testid="agent-handoff-empty">{t("agents.handoff.none")}</p>}
      {rows.length > 0 && (
        <div className={GRID} aria-hidden="true">
          <span className={HEAD}>{t("agents.handoff.name")}</span>
          <span className={HEAD}>{t("agents.handoff.purpose")}</span>
          <span className={HEAD}>{t("agents.handoff.optional")}</span>
          <span />
        </div>
      )}
      {rows.map((row, index) => (
        <div key={row.key} className={GRID} data-testid={`agent-handoff-row-${index}`}>
          <Input
            variant="ghost"
            size="md"
            className="font-mono text-xs"
            value={row.name}
            disabled={disabled}
            placeholder={t("agents.handoff.namePlaceholder")}
            aria-label={t("agents.handoff.name")}
            onChange={(event) => edit(row.key, { name: event.target.value })}
            data-testid={`agent-handoff-name-${index}`}
          />
          <Input
            variant="ghost"
            size="md"
            value={row.purpose}
            disabled={disabled}
            placeholder={t("agents.handoff.purposePlaceholder")}
            aria-label={t("agents.handoff.purpose")}
            onChange={(event) => edit(row.key, { purpose: event.target.value })}
            data-testid={`agent-handoff-purpose-${index}`}
          />
          <div className="flex h-10 items-center justify-center" data-testid={`agent-handoff-optional-${index}`}>
            <Toggle size="sm" aria-label={t("agents.handoff.optional")} checked={row.optional} disabled={disabled} onChange={(event) => edit(row.key, { optional: event.target.checked })} />
          </div>
          <div className="flex h-10 items-center">
            <IconButton icon="Close" size="xs" variant="ghost" color="rose" srLabel={t("agents.handoff.remove")} tooltip={t("agents.handoff.remove")} disabled={disabled} onClick={() => remove(row.key)} data-testid={`agent-handoff-remove-${index}`} />
          </div>
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="ghost" disabled={disabled} onClick={add} data-testid="agent-handoff-add">
          {t("agents.handoff.add")}
        </Button>
        {offered.length > 0 && <span className="text-xs opacity-60">{t("agents.handoff.suggested")}</span>}
        {offered.map((field) => (
          <Pill key={field.name} size="sm" variant="outline" className="cursor-pointer" onClick={() => !disabled && addSuggested(field)} data-testid={`agent-handoff-suggest-${field.name}`}>
            {field.name}{field.optional ? " ·" : ""}
          </Pill>
        ))}
      </div>
    </div>
  );
}

/** A column value as the builder would write it back: "" and "{}" are the same, an object is in the builder's own key order. */
function normalized(value: string): string {
  return handoffJson(handoffRows(value));
}
