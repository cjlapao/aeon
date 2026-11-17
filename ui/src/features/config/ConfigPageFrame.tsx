import type { ReactNode } from "react";
import { CONFIG_GROUPS, type RegisteredConfigPage } from "./lib/configRegistry";

/**
 * S72 CF6: the one page frame every Configuration page renders inside.
 *
 * The page label as `h1`, the group label as a quiet breadcrumb line, then
 * the page. A section that already renders its own title panel registers
 * `frame: "bare"` and gets no `h1` here — R1's double-header is solved by
 * the frame standing down, never by editing the section.
 *
 * A `fill` page (ConfigPageDef.fill) changes the frame's own geometry: the
 * section becomes the height chain the layout started (fixed viewport height,
 * `min-h-0` so it can shrink below content) — the header keeps what it needs
 * and the page gets exactly the rest, no page-level scroll.
 */
export function ConfigPageFrame({ def, children }: { def: RegisteredConfigPage; children: ReactNode }) {
  const groupLabel = CONFIG_GROUPS.find((group) => group.id === def.group)?.label;
  const header = (
    <div className={def.fill ? "shrink-0" : undefined}>
      {groupLabel && <p className="aeon-accent-text text-xs uppercase tracking-[.2em]">{groupLabel}</p>}
      {def.frame === "titled" && <h1 className="mt-1 text-3xl font-semibold">{def.label}</h1>}
      {def.summary && <p className="mt-2 text-sm text-slate-600 dark:text-slate-400">{def.summary}</p>}
    </div>
  );
  if (def.fill) {
    return (
      <section
        className="flex min-h-0 min-w-0 flex-1 flex-col gap-5 bg-transparent p-4 md:p-7"
        data-testid="config-page-frame"
        data-page={def.id}
      >
        {header}
        <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      </section>
    );
  }
  return (
    <section className="space-y-5 min-w-0 p-4 md:p-7 bg-transparent" data-testid="config-page-frame" data-page={def.id}>
      {header}
      {children}
    </section>
  );
}
