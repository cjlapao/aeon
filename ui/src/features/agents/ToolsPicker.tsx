import { useI18n } from "@cjlapao/ui-kit";
import { Picker, type PickerItem } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { useRpcQuery } from "../../lib/useRpcQuery";
import type { RpcMethods } from "../../proto/contract";

// The tools picker: what an agent may ask for, chosen from ONE searchable list with the groups
// selectable.
//
// About seventy tools is too many to tick one by one, so the list is grouped. The groups the daemon
// curates come first — its kits (`handlers.list` → `toolKits`): "Read the code", "Change files",
// "Run commands"… — and every other tool is grouped under the family the daemon files it in
// (`tools.list`). A group's header is a checkbox that takes the whole group in or out, and the
// search narrows what the header acts on, so "grep" then the header picks every match. The picker
// holds no tool name of its own: kits and tools both come from the daemon, so it cannot drift from
// the registry. A tool in the list is permission to ASK; the rules written with the agent decide the
// call, and the picker says so.

export type ToolKit = RpcMethods["handlers.list"]["result"]["toolKits"][number];
type CatalogTool = RpcMethods["tools.list"]["result"]["tools"][number];

export interface ToolsPickerProps {
  /** The chosen tool names. */
  value: readonly string[];
  onChange: (next: string[]) => void;
  /** The daemon's kits, from `handlers.list`. */
  kits: readonly ToolKit[];
  controller: LiveController;
  live: LiveState;
  disabled?: boolean;
  /** An agent imported without a signature: every tool asks for approval first, not just commands. */
  asksEverything?: boolean;
  testId?: string;
}

/**
 * One tool's row: its name, and its description on as many lines as it takes (three, with the whole
 * text on hover). The row has a width of its own on purpose: the picker sizes its popup to its widest
 * row, and a tool's description is a paragraph — left on one line it would stretch the popup past
 * the page. `description` still carries the whole text on the item, so the search reads it.
 */
function toolBody(name: string, description: string) {
  return (
    <div className="w-[26rem] max-w-full whitespace-normal" data-tools-row="">
      <p className="text-sm">{name}</p>
      <p className="mt-0.5 line-clamp-3 break-words text-xs opacity-75" title={description}>{description}</p>
    </div>
  );
}

/** How far a kit reaches, as a tag on its tools: the three that need a second look. */
const EFFECT_TONE: Record<string, "amber" | "red"> = { changes: "amber", runs: "red", reaches: "amber" };

/**
 * The picker's items, in the order its group headers must appear: each kit's tools together, then
 * the rest of the catalog grouped by family. The picker draws a header where the group changes, so
 * a group's tools must be next to each other.
 */
function toolItems(kits: readonly ToolKit[], tools: readonly CatalogTool[], effectWord: (effect: string) => string, asksFirst: string): PickerItem[] {
  const byName = new Map<string, CatalogTool>();
  for (const tool of tools) if (tool.available) byName.set(tool.name, tool);

  const items: PickerItem[] = [];
  const placed = new Set<string>();
  for (const kit of kits) {
    const tone = EFFECT_TONE[kit.effect];
    for (const name of kit.tools) {
      const tool = byName.get(name);
      if (tool === undefined || placed.has(name)) continue;
      placed.add(name);
      const tags = [];
      if (tone !== undefined) tags.push({ label: effectWord(kit.effect), tone });
      if (name === "bash") tags.push({ label: asksFirst, tone: "red" as const });
      items.push({ id: name, title: name, textValue: name, body: toolBody(name, tool.description), description: tool.description, group: kit.label, tags });
    }
  }
  // The rest, a family at a time: families in the order the daemon first names them.
  const families: string[] = [];
  const inFamily = new Map<string, CatalogTool[]>();
  for (const tool of byName.values()) {
    if (placed.has(tool.name)) continue;
    const list = inFamily.get(tool.family);
    if (list === undefined) {
      families.push(tool.family);
      inFamily.set(tool.family, [tool]);
    } else {
      list.push(tool);
    }
  }
  for (const family of families) {
    for (const tool of inFamily.get(family) ?? []) {
      const tags = tool.name === "bash" ? [{ label: asksFirst, tone: "red" as const }] : [];
      items.push({ id: tool.name, title: tool.name, textValue: tool.name, body: toolBody(tool.name, tool.description), description: tool.description, group: family, tags });
    }
  }
  return items;
}

export function ToolsPicker({ value, onChange, kits, controller, live, disabled = false, asksEverything = false, testId = "agent-tools-picker" }: ToolsPickerProps) {
  const { t } = useI18n();
  const catalog = useRpcQuery("tools.list", {}, controller, live);
  const loading = catalog.loading && catalog.data === undefined;
  const items = toolItems(kits, catalog.data?.tools ?? [], (effect) => t(`agents.tools.effect.${effect}`), t("agents.tools.asksFirst"));
  const hasBash = value.includes("bash");

  return (
    <div className="space-y-3" data-testid={testId}>
      <Picker
        multi
        groupSelectable
        items={items}
        loading={loading}
        selectedIds={[...value]}
        onMultiChange={(ids) => onChange(ids)}
        placeholder={t("agents.tools.placeholder")}
        searchPlaceholder={t("agents.tools.search")}
        emptyMessage={t("agents.tools.empty")}
        loadingMessage={t("agents.tools.loading")}
        maxPillsShown={4}
        disabled={disabled}
      />

      {catalog.error !== undefined && (
        <p className="text-sm text-red-400" data-testid="agent-tools-failed">{t("agents.tools.failed")} {catalog.error}</p>
      )}
      {asksEverything && (
        <p className="rounded-lg border border-amber-400/40 p-2 text-xs" data-testid="agent-tools-asks-everything">{t("agents.tools.asksEverything")}</p>
      )}
      {!asksEverything && hasBash && (
        <p className="rounded-lg border border-amber-400/40 p-2 text-xs" data-testid="agent-tools-bash-asks">{t("agents.tools.bashAsks")}</p>
      )}
      <p className="text-xs opacity-75" data-testid="agent-tools-summary">
        {value.length === 0 ? t("agents.tools.none") : t("agents.tools.selected", { total: value.length })} {t("agents.tools.policy")}
      </p>
    </div>
  );
}
