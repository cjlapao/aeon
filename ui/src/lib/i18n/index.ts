import enCore from "./en.json";
import ptCore from "./pt.json";
import enGates from "./en-gates.json";
import ptGates from "./pt-gates.json";
import enEnvironments from "./en-environments.json";
import ptEnvironments from "./pt-environments.json";
import enAutomation from "./en-automation.json";
import ptAutomation from "./pt-automation.json";
import enRoadmap from "./en-roadmap.json";
import ptRoadmap from "./pt-roadmap.json";
import enTriage from "./en-triage.json";
import ptTriage from "./pt-triage.json";
import enMemory from "./en-memory.json";
import ptMemory from "./pt-memory.json";
import enHistory from "./en-history.json";
import ptHistory from "./pt-history.json";
import enSystem from "./en-system.json";
import ptSystem from "./pt-system.json";
import enMergeQueue from "./en-mergequeue.json";
import ptMergeQueue from "./pt-mergequeue.json";
import enContinuous from "./en-continuous.json";
import ptContinuous from "./pt-continuous.json";
import enAgents from "./en-agents.json";
import enWork from "./en-work.json";
import ptWork from "./pt-work.json";
import ptAgents from "./pt-agents.json";

type Catalog = { readonly [key: string]: unknown };

function isPlainObject(value: unknown): value is Catalog {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Deep-merge catalog fragments into one catalog. Fragments are authored so
 * their namespaces never touch; a leaf collision simply lets the later
 * fragment win, which is an authoring bug the en/pt parity tests catch.
 *
 * The split itself is SIZE1: the core catalogs passed 1,000 lines when
 * S57 T11 added `settings.gates.*` and `loops.dod.*`, so exactly those two
 * namespaces — one concern, one feature folder's worth of strings — moved
 * into `*-gates.json` in the same change (S57 T11 Notes records it).
 */
function mergeCatalog(base: Catalog, extra: Catalog): Catalog {
  const out: Record<string, unknown> = { ...base };
  for (const [key, value] of Object.entries(extra)) {
    const held = out[key];
    out[key] = isPlainObject(held) && isPlainObject(value) ? mergeCatalog(held, value) : value;
  }
  return out;
}

const enFragments = [enGates, enEnvironments, enAutomation, enRoadmap, enTriage, enMemory, enHistory, enMergeQueue, enSystem, enContinuous, enAgents, enWork];
const ptFragments = [ptGates, ptEnvironments, ptAutomation, ptRoadmap, ptTriage, ptMemory, ptHistory, ptMergeQueue, ptSystem, ptContinuous, ptAgents, ptWork];

const en = enFragments.reduce<Catalog>((acc, fragment) => mergeCatalog(acc, fragment as Catalog), enCore as Catalog);
const pt = ptFragments.reduce<Catalog>((acc, fragment) => mergeCatalog(acc, fragment as Catalog), ptCore as Catalog);

/**
 * Aeon's own message catalogs, layered under `@cjlapao/ui-kit`'s
 * `I18nProvider`. The kit ships its own built-in catalogs for its
 * components' internal strings (button labels, calendar names, under a
 * `kit.*` namespace) for English, French, Spanish, German, and Portuguese —
 * those are automatic and need nothing from this app. This map is only
 * Aeon's own content, under its own namespace (`home.*` today), merged
 * alongside the kit's built-ins by the same provider.
 *
 * Only `en` and `pt` exist today, on purpose (the request this shipped
 * from: "just to have the system in place"). `SUPPORTED_LOCALES` is the
 * list a language switcher should offer — deliberately narrower than the
 * kit's own `CURATED_LOCALES`, which also lists `fr`/`es`/`de`: picking one
 * of those would translate every kit component correctly while leaving
 * every one of Aeon's own screens in English, which is a confusing half
 * measure to offer before Aeon has catalogs of its own for them.
 */
export const APP_LOCALES = { en, pt } as const;

export const SUPPORTED_LOCALES = Object.keys(APP_LOCALES) as Array<keyof typeof APP_LOCALES>;

export const LOCALE_LABELS: Record<(typeof SUPPORTED_LOCALES)[number], string> = {
  en: "English",
  pt: "Português",
};
