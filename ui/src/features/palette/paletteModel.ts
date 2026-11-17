import { pathTo, workPath, WORK_TABS, type WorkComponent, type WorkLoop, type WorkTab } from "../work/workModel";

/** One thing the Ctrl K box can take a person to. Pure data: the box only navigates. */
export type PaletteGroup = "components" | "loops" | "pages";

export interface PaletteItem {
  key: string;
  group: PaletteGroup;
  label: string;
  /** A small tag after the label: the tab a component result opens, or a loop's component. */
  chip?: string;
  /** A loop's status, shown at the right. */
  status?: string;
  to: string;
}

export interface PalettePage { key: string; label: string; to: string }

export const GROUP_ORDER: readonly PaletteGroup[] = ["components", "loops", "pages"];
const PER_GROUP = 6;

/** How well `text` answers `query`: 0 = no, higher = better (prefix beats word start beats inside). */
export function score(text: string, query: string): number {
  const hay = text.toLowerCase();
  const needle = query.trim().toLowerCase();
  if (needle === "") return 1;
  const at = hay.indexOf(needle);
  if (at < 0) return 0;
  if (at === 0) return 4;
  return /[\s›/_-]/.test(hay[at - 1] ?? "") ? 3 : 2;
}

/**
 * Build the ranked result list. A component result is "aeon › ui"; when the query also names a tab
 * ("ui mem"), the result opens that tab and says so in its chip. Nothing is invented: every item
 * is a component, loop or page the daemon or the app already has.
 */
export function buildItems(
  query: string,
  components: readonly WorkComponent[],
  loops: readonly WorkLoop[],
  pages: readonly PalettePage[],
  tabLabel: (tab: WorkTab) => string,
  startLabel: (name: string) => string = (name) => `Start a loop in ${name}`,
): PaletteItem[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const tabWord = words.length > 1 ? words[words.length - 1]! : "";
  const tab = WORK_TABS.find((id) => tabWord !== "" && tabLabel(id).toLowerCase().startsWith(tabWord));
  const nameQuery = tab ? words.slice(0, -1).join(" ") : words.join(" ");

  const ranked: { item: PaletteItem; score: number }[] = [];
  for (const component of components) {
    const label = pathTo(components, component.id).map((c) => c.name).join(" › ");
    const s = score(label, nameQuery);
    if (s > 0) ranked.push({ score: s, item: { key: `c-${component.id}`, group: "components", label, chip: tab ? tabLabel(tab) : undefined, to: workPath(component.id, tab ?? "overview") } });
  }
  // A single word that is a tab name ("mem") offers that tab on every component, below name matches.
  const solo = words.length === 1 ? WORK_TABS.find((id) => tabLabel(id).toLowerCase().startsWith(words[0]!)) : undefined;
  if (solo) {
    for (const component of components) {
      const label = pathTo(components, component.id).map((c) => c.name).join(" › ");
      ranked.push({ score: 1, item: { key: `c-${component.id}-${solo}`, group: "components", label, chip: tabLabel(solo), to: workPath(component.id, solo) } });
    }
  }
  for (const loop of loops) {
    const s = Math.max(score(loop.slug, nameQuery), score(loop.title ?? "", nameQuery));
    if (s > 0 && nameQuery !== "") {
      const owner = components.find((c) => c.root === loop.componentRoot);
      ranked.push({ score: s, item: { key: `l-${loop.id}`, group: "loops", label: loop.slug, chip: owner?.name, status: loop.status, to: `/loops/${encodeURIComponent(loop.id)}` } });
    }
  }
  for (const page of pages) {
    const s = score(page.label, query);
    if (s > 0) ranked.push({ score: s, item: { key: `p-${page.key}`, group: "pages", label: page.label, to: page.to } });
  }

  // "Start a loop in <component>" is offered only once the query names a component, never for the whole list.
  if (nameQuery !== "" && /^(start|loop|new)/.test(nameQuery) === false) {
    for (const component of components) {
      const s = score(component.name, nameQuery);
      if (s > 0) ranked.push({ score: 0.5, item: { key: `s-${component.id}`, group: "pages", label: startLabel(component.name), to: `/?component=${encodeURIComponent(component.root)}` } });
    }
  }

  const out: PaletteItem[] = [];
  for (const group of GROUP_ORDER) {
    ranked
      .filter((r) => r.item.group === group)
      .sort((a, b) => b.score - a.score || a.item.label.localeCompare(b.item.label))
      .slice(0, PER_GROUP)
      .forEach((r) => out.push(r.item));
  }
  return out;
}
