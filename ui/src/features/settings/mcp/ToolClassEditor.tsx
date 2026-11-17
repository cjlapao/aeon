import { useI18n } from "@cjlapao/ui-kit";
import { Button, Input, Pill, Select } from "../../../components/kit";
import type { McpVocabulary, ToolDraft } from "./mcpModel";

/**
 * One tool row inside the server form (MG: classes are declared, never
 * assumed). The rerun and effect selects carry NO default — an empty class
 * is the real UNCLASSIFIED state (MC6: the tool is refused until its
 * classes are declared), and no affordance ever preselects a "safe" value.
 */
export interface ToolClassEditorProps {
  tool: ToolDraft;
  vocabulary: McpVocabulary | undefined;
  disabled: boolean;
  onChange: (tool: ToolDraft) => void;
  onRemove: () => void;
  /** Exactly one of the two classes is set — the row is marked until both are (or neither). */
  incomplete?: boolean;
}

export function ToolClassEditor({ tool, vocabulary, disabled, onChange, onRemove, incomplete = false }: ToolClassEditorProps) {
  const { t } = useI18n();
  const unclassified = tool.rerun === "" || tool.effect === "";
  return (
    <div
      className={`flex flex-wrap items-center gap-2 ${incomplete ? "rounded-md ring-2 ring-amber-500/70 ring-offset-2 ring-offset-transparent" : ""}`}
      data-testid="tool-class-row"
      data-incomplete={incomplete || undefined}
    >
      <div className="min-w-40 flex-1">
      <Input
        variant="ghost"
        aria-label={t("settings.mcp.form.tool.name")}
        value={tool.name}
        disabled={disabled}
        onChange={(event) => onChange({ ...tool, name: event.target.value })}
      />
      </div>
      <Select
        variant="ghost"
        size="md"
        aria-label={t("settings.mcp.form.tool.rerun")}
        value={tool.rerun}
        disabled={disabled}
        onChange={(event) => onChange({ ...tool, rerun: event.target.value })}
      >
        {/* The empty option IS the unclassified state — no default class. */}
        <option value="">{t("settings.mcp.form.tool.unclassifiedOption")}</option>
        {(vocabulary?.rerun ?? []).map((value) => (
          <option key={value} value={value}>
            {value}
          </option>
        ))}
      </Select>
      <Select
        variant="ghost"
        size="md"
        aria-label={t("settings.mcp.form.tool.effect")}
        value={tool.effect}
        disabled={disabled}
        onChange={(event) => onChange({ ...tool, effect: event.target.value })}
      >
        <option value="">{t("settings.mcp.form.tool.unclassifiedOption")}</option>
        {(vocabulary?.effect ?? []).map((value) => (
          <option key={value} value={value}>
            {value}
          </option>
        ))}
      </Select>
      {unclassified && (
        <Pill tone="amber" data-testid="tool-unclassified-chip">
          {t("settings.mcp.tool.unclassified")}
        </Pill>
      )}
      <Button size="xs" variant="ghost" disabled={disabled} onClick={onRemove}>
        {t("settings.mcp.form.tool.remove")}
      </Button>
    </div>
  );
}
