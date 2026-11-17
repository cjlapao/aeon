import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

/** Skills: the Agent Skills the daemon found, each checked against the specification. */
registerConfigPage({
  id: "skills",
  label: "Skills",
  group: "access",
  order: 60,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "Agent Skills the daemon found, validated.",
  render: lazyConfigRender(() => import("../../settings/skills/SkillsSection").then((m) => ({ default: m.SkillsSection }))),
});
