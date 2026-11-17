import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the memory section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "memory",
  label: "Memory",
  group: "work",
  order: 50,
  status: "ready",
  movedTo: "/work/all/knowledge",
  frame: "bare",
  fill: true,
  summary: "What the daemon remembers, and how it connects.",
  render: lazyConfigRender(() => import("../../settings/memory/MemorySection").then((m) => ({ default: m.MemorySection }))),
});
