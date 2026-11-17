import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the triage section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "triage",
  label: "Triage",
  group: "work",
  order: 40,
  status: "ready",
  movedTo: "/work/all/guardrails",
  frame: "bare",
  fill: true,
  summary: "Bug claims: check them, decide them, hand them off.",
  render: lazyConfigRender(() => import("../../settings/triage/TriageSection").then((m) => ({ default: m.TriageSection }))),
});
