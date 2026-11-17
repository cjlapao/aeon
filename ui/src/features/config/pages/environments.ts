import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the execution-environment section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "environments",
  label: "Environments",
  group: "work",
  order: 20,
  status: "ready",
  movedTo: "/work/all/guardrails",
  frame: "bare",
  fill: true,
  summary: "What loop commands run in, and where it applies.",
  render: lazyConfigRender(() => import("../../settings/environments/EnvironmentsSection").then((m) => ({ default: m.EnvironmentsSection }))),
});
