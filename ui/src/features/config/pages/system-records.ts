import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the System records (seed) panel, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "system-records",
  label: "System records",
  group: "general",
  order: 30,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "What the app ships as rows, and whether this machine's copy is current.",
  render: lazyConfigRender(() => import("../../settings/seed/SystemRecordsPanel").then((m) => ({ default: m.SystemRecordsPanel }))),
});
