import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the verification gates section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "gates",
  label: "Verification gates",
  group: "work",
  order: 10,
  status: "ready",
  movedTo: "/work/all/guardrails",
  frame: "bare",
  fill: true,
  summary: "Commands that check work before it is merged, and the merge rule.",
  render: lazyConfigRender(() => import("../../settings/gates/GatesPanel").then((m) => ({ default: m.GatesSection }))),
});
