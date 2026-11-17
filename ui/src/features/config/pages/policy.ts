import { createElement } from "react";
import { registerConfigPage } from "../lib/configRegistry";
import { lazyConfigRender } from "../lib/lazyConfigRender";

const banner = lazyConfigRender(() => import("../PolicyDefaultBanner").then((m) => ({ default: m.PolicyDefaultBanner })));
const section = lazyConfigRender(() => import("../../settings/policy/PolicySection").then((m) => ({ default: m.PolicySection })));

/**
 * S72 T2 (CF3): the policy section, moved from Settings unchanged. Q2: titled Panel root → "bare". Above it, one
 * line says what happens to a call no rule covers (the permissions.unmatched default).
 */
registerConfigPage({
  id: "policy",
  label: "Policy",
  group: "access",
  order: 20,
  status: "ready",
  frame: "bare",
  fill: true,
  summary: "The rules the daemon decides command verdicts by.",
  render: (ctx) => createElement("div", { className: "flex min-h-0 flex-1 flex-col gap-3" }, banner(ctx), section(ctx)),
});
