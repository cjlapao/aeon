import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** S72 T2 (CF3): the MCP section, moved from Settings unchanged. Q2: titled Panel root → "bare". */
registerConfigPage({
  id: "mcp",
  label: "MCP servers",
  group: "access",
  order: 30,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "Server records, grants and status, authored through the human door.",
  render: lazyConfigRender(() => import("../../settings/mcp/McpSection").then((m) => ({ default: m.McpSection }))),
});
