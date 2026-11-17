/**
 * Aeon's own icon set — the SVG files in `src/icons/`, parsed once at load.
 *
 * Ported from plai-client-app's AppIcon registry (src/components/icons/),
 * and changed where Aeon's icons differ from that set:
 *
 * - **The files are the source.** plai keeps inner markup as strings in a TS
 *   map; here a designer drops an `.svg` into `src/icons/` exactly as drawn,
 *   and it is read with Vite's `?raw` import. Adding one is: add the file,
 *   add its name to ICON_NAMES (the test fails until both agree).
 * - **Each icon keeps its own frame.** plai forces one 24×24, 1.75px-stroke
 *   frame; these are drawn on 16×16 with 1px strokes and mix strokes and
 *   fills, so the root's viewBox and paint attributes are kept per icon.
 * - **Sanitised on the way in.** An SVG is markup rendered with
 *   innerHTML, so scripts, event handlers, foreignObject and `class`
 *   attributes (Figma exports carry hashed ones) are stripped.
 *
 * Size with `size` (default 14, the drawn size), colour with `text-*`
 * classes — every path paints with `currentColor`.
 */

export const ICON_NAMES = [
  "arrow-down",
  "bash",
  "branch",
  "check",
  "chevron-expanded",
  "chevron-hover",
  "copy",
  "cross",
  "edit",
  "panel",
  "pause",
  "play",
  "read",
  "reset",
  "shield",
  "skip",
  "stop",
  "think",
  "user",
] as const;

export type IconName = (typeof ICON_NAMES)[number];

export interface IconDefinition {
  viewBox: string;
  /** Root paint attributes worth keeping: fill, stroke, stroke-width, line caps/joins. */
  attrs: Record<string, string>;
  /** Sanitised inner markup. */
  body: string;
}

const KEPT_ROOT_ATTRS = ["fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin"];

/** Parses one SVG file's text into what AppIcon renders. Exported for the test. */
export function parseSvg(raw: string): IconDefinition | null {
  const root = /<svg\b([^>]*)>([\s\S]*?)<\/svg>/i.exec(raw);
  if (!root) return null;
  const rootAttrs: Record<string, string> = {};
  for (const m of (root[1] ?? "").matchAll(/([\w:-]+)\s*=\s*"([^"]*)"/g)) rootAttrs[m[1]!] = m[2]!;
  const attrs: Record<string, string> = {};
  for (const key of KEPT_ROOT_ATTRS) if (rootAttrs[key] !== undefined) attrs[key] = rootAttrs[key]!;
  const body = (root[2] ?? "")
    .replace(/<script\b[\s\S]*?<\/script>/gi, "")
    .replace(/<foreignObject\b[\s\S]*?<\/foreignObject>/gi, "")
    .replace(/\son[a-z]+\s*=\s*"[^"]*"/gi, "")
    .replace(/\sclass\s*=\s*"[^"]*"/gi, "")
    .replace(/\s+/g, " ")
    .trim();
  return { viewBox: rootAttrs.viewBox ?? "0 0 16 16", attrs, body };
}

const SOURCES = import.meta.glob("../../icons/*.svg", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

/** Every file in `src/icons/`, by file name without `.svg`. */
export const ICON_FILES: Record<string, IconDefinition> = Object.fromEntries(
  Object.entries(SOURCES)
    .map(([path, raw]) => [path.replace(/^.*\//, "").replace(/\.svg$/, ""), parseSvg(raw)] as const)
    .filter((entry): entry is readonly [string, IconDefinition] => entry[1] !== null),
);

export const ICONS = ICON_FILES as Record<IconName, IconDefinition>;
