import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the daemon-settings section, moved from Settings unchanged.
 *  Q2: its root is a titled Panel → frame "bare". */
registerConfigPage({
  id: "daemon-settings",
  label: "Daemon settings",
  group: "general",
  order: 10,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "The values the daemon runs with: what each does, and where its value comes from.",
  render: lazyConfigRender(() => import("../../settings/DaemonSettingsPanel").then((m) => ({ default: m.DaemonSettingsPanel }))),
});
