import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the platform-status section (SettingsScreen's SystemSection), moved unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "platform-status",
  label: "Platform status",
  group: "platform",
  order: 10,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "What the daemon has indexed about your code, and what keeping it costs.",
  render: lazyConfigRender(() => import("../../settings/system/SystemSection").then((m) => ({ default: m.SystemSection }))),
});
