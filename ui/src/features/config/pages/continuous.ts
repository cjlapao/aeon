import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the continuous-work scheduler section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "continuous",
  label: "Continuous work",
  group: "work",
  order: 60,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "Lets agents pick up ready work on their own, inside the hours and limits you set.",
  render: lazyConfigRender(() => import("../../settings/continuous/ContinuousSection").then((m) => ({ default: m.ContinuousSection }))),
});
