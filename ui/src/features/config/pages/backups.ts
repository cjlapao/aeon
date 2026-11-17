import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the backups section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "backups",
  label: "Backups",
  group: "general",
  order: 20,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "Is there a recent copy of the database, and could it be restored?",
  render: lazyConfigRender(() => import("../../settings/backups/BackupsSection").then((m) => ({ default: m.BackupsSection }))),
});
