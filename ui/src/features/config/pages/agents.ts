import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

// `/config/agents` is the agent CRUD page — an action bar, a table whose page,
// order and filter the daemon decides, and the honest states between them.
// It is the feature's ONLY surface: the Test bench's legacy raw-verb door
// (`agent-management`) was retired 2026-11-08, and the feature's files hold the
// product screen and nothing else.
//
// `frame: "bare"` is load-bearing here: the list and the agent's own page each draw their own
// title (the page of one agent has a way back instead of a heading), so the frame keeps only
// the group line and stands down for the rest.
//
// `fill: true` takes the page out of the shell's page-level scroll: the layout,
// the frame and the screen's own flex column hand the viewport height down to
// the table, which scrolls its rows under the sticky header. Resize takes care
// of itself — the height is CSS all the way down, never a measured number.
registerConfigPage({
  id: "agents",
  label: "Agents",
  group: "access",
  order: 10,
  status: "ready",
  frame: "bare",
  fill: true,
  render: lazyConfigRender(() => import("../../agents/AgentsScreen").then((m) => ({ default: m.AgentsScreen }))),
});
