import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** Variables & secrets: the values the app holds for processes and agents, encrypted, agent access off by default. */
registerConfigPage({
  id: "variables",
  label: "Variables & secrets",
  group: "access",
  order: 25,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "Environment variables and secrets for MCP servers, environments and agents.",
  render: lazyConfigRender(() => import("../../settings/variables/VariablesSection").then((m) => ({ default: m.VariablesSection }))),
});
