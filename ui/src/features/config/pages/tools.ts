import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/**
 * S72 T2 (CF3): the tools catalog section, moved from Settings unchanged —
 * including the deliberate difference T2 step 1 names: ToolsSection receives
 * NO `canWrite` today, so the picker passes only what SettingsScreen passed.
 * Q2: titled Panel root → "bare".
 */
registerConfigPage({
  id: "tools",
  label: "Tools",
  group: "access",
  order: 40,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "The registered tool catalog — a read-only view.",
  render: lazyConfigRender(
    () => import("../../settings/tools/ToolsSection").then((m) => ({ default: m.ToolsSection })),
    (ctx) => ({ controller: ctx.controller, live: ctx.live }),
  ),
});
