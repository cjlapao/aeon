import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the automation section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "automation",
  label: "Automation",
  group: "work",
  order: 30,
  status: "ready",
  movedTo: "/work/all/automation",
  frame: "bare",
  fill: true,
  summary: "What starts loops on its own: schedules, webhooks and events.",
  render: lazyConfigRender(() => import("../../settings/automation/AutomationSection").then((m) => ({ default: m.AutomationSection }))),
});
