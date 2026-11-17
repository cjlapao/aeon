import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

/**
 * S72 T2 (R3): the source-scan trio the section guard tests re-pin onto
 * after the mounts moved from `SettingsScreen` to the Configuration pages.
 * Same law, new site: every section mounts EXACTLY ONCE in the product —
 * through one `features/config/pages/<id>.ts` file, registered by exactly
 * one `registerAll.ts` line — and nowhere in Settings anymore.
 */
const PAGES_DIR = join(__dirname, "../src/features/config/pages");
const SCREEN = join(__dirname, "../src/features/settings/SettingsScreen.tsx");

/** How many config page files select this section component (`m.<Component>`). */
export function configMountCount(component: string): number {
  let total = 0;
  for (const file of readdirSync(PAGES_DIR)) {
    if (!file.endsWith(".ts")) continue;
    const src = readFileSync(join(PAGES_DIR, file), "utf8");
    total += (src.match(new RegExp(`\\bm\\.${component}\\b`, "g")) ?? []).length;
  }
  return total;
}

/** True when registerAll.ts imports `./<pageId>` exactly once. */
export function configPageRegisteredOnce(pageId: string): boolean {
  const src = readFileSync(join(PAGES_DIR, "registerAll.ts"), "utf8");
  return (src.match(new RegExp(`import "\\./${pageId}"`, "g")) ?? []).length === 1;
}

/** True when SettingsScreen still renders this component (it must not). */
export function settingsScreenMounts(component: string): boolean {
  return readFileSync(SCREEN, "utf8").includes(`<${component}`);
}
