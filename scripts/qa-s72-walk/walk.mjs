// S72 T4 — the browser walk of every Configuration page.
//
// Drives the SYSTEM Chrome (channel: "chrome", the earlier session's choice)
// against bridge.mjs — the plain-browser stand-in for the Tauri shell, with
// the REAL daemon on the other side (throwaway XDG_STATE_HOME, never the real
// store). The desktop shell is not buildable on this machine this session
// (the tree carries unrelated in-progress src-tauri edits), so the walk runs
// through the stubbed shell exactly as T4's step 1 permits, and Notes record
// what that cannot show.
//
// v2 (the crash-recovery session): the v1 walk clicked "the first control in
// the section root", which for most pages is a filter or a disabled reset —
// weak evidence. v2 names the real primary control per page (grep-measured
// data-testids) and fixes the stub's missing
// window.__TAURI_EVENT_PLUGIN_INTERNALS__, which made @tauri-apps/api's
// _unlisten throw `unregisterListener` on every page teardown — a harness
// artifact, never an app one (a real Tauri shell injects that object; see
// node_modules/@tauri-apps/api/mocks.js for the same stand-in).
// v2 also runs a REAL disconnected probe (--disconnect mode: daemon down,
// reload a page, expect the shell's not-connected state — acceptance 6).
//
// Run (three shells, or see run.sh):
//   daemon:  XDG_STATE_HOME=<scratch>/state LOCALRPC_ENDPOINT_PATH=<scratch>/run/rpc.sock aeon serve
//   bridge:  node bridge.mjs --sock <rpc.sock> --port 8712
//   walk:    PW=<path to playwright-core> node walk.mjs [--only <id>] [--keep-shots <dir>]
//   walk 2:  PW=... WALK_JSON=<scratch>/walk-disconnect.json node walk.mjs --disconnect
import { createRequire } from "node:module";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import { initScript } from "./tauri-stub.mjs";

const require = createRequire(import.meta.url);
const PW = process.env.PW ?? resolve(import.meta.dirname, "../../../.s72-scratch/pw/node_modules/playwright-core");
const { chromium } = require(PW);
const BASE = process.env.WALK_URL ?? "http://127.0.0.1:8712/";
const argv = process.argv.slice(2);
const only = argv.includes("--only") ? argv[argv.indexOf("--only") + 1] : null;
const shots = argv.includes("--keep-shots") ? resolve(argv[argv.indexOf("--keep-shots") + 1]) : null;
if (shots) mkdirSync(shots, { recursive: true });

// The shell stub: exactly the internals @tauri-apps/api/core.js uses, with
// invokes forwarded over the WebSocket to bridge.mjs — plus the event-plugin
// internals the real injected script provides (v2; see mocks.js for the shape).


const log = (...a) => process.stderr.write(a.join(" ") + "\n");

/** Click one named control, with a short timeout so a disabled control says so fast. */
async function click(p, sel, ms, okLine) {
  const t = await p.$(sel);
  if (!t) return `no control found for ${sel}`;
  await t.click({ timeout: ms ?? 5000 });
  await p.waitForTimeout(500);
  return okLine ?? `clicked ${sel}`;
}

/** One page of the walk: what loads, what its real primary action does, what breaks.
 * The selector is the page's OWN primary control, measured from the source
 * (data-testid grep), not "the first button". Writes land on the throwaway
 * store only. "read …" actions press nothing — the page offers no write. */
const WALK = [
  {
    id: "daemon-settings",
    planned: "flip the one declared setting (a real settings.set on the throwaway store)",
    settle: 900,
    act: async (p) => {
      // Dump the control's real DOM (the follow-up line for the no-write defect
      // needs the markup), press its first option, then read the whole row.
      const grpHtml = await p.$eval("[aria-label='agents.allowUnsignedImport']", (el) => el.outerHTML.replace(/\s+/g, " ").slice(0, 500)).catch(() => null);
      const t = await p.$("[aria-label='agents.allowUnsignedImport'] button");
      if (!t) return `no option buttons in the control group — group html: ${grpHtml}`;
      await t.click({ timeout: 6000 });
      await p.waitForTimeout(1500);
      const row = await p.$eval("[data-testid='daemon-setting-row']", (el) => el.innerText.replace(/\s+/g, " ").slice(0, 500)).catch(() => null);
      return `clicked the group's first option; group markup: ${String(grpHtml).slice(0, 260)} || row after: ${row}`;
    },
  },
  {
    id: "backups",
    planned: "press Run now (a real backup.run)",
    act: (p) => click(p, "[data-testid='backup-run-now']", 6000, "pressed Run now"),
  },
  {
    id: "system-records",
    planned: "expand the first set's records (seed.status on a fresh store answers ~6s — the settle is that long, measured)",
    settle: 9000,
    act: (p) => click(p, "[data-testid='seed-set-card']", 5000, "expanded the first set card"),
  },
  {
    id: "agents",
    planned: "create an agent through the create panel (a real agent.create; only `name` is required)",
    act: async (p) => {
      const name = await p.$("input[data-testid='am-create-name'], [data-testid='am-create-name'] input");
      const go = await p.$("[data-testid='am-create-run']");
      if (!name || !go) return "create panel inputs not found";
      await name.fill(`walk-agent-${Date.now() % 100000}`);
      await go.click({ timeout: 6000 });
      await p.waitForTimeout(1500);
      const panel = await p.$eval("[data-testid='am-create-panel']", (el) => el.innerText.replace(/\s+/g, " ").slice(0, 340)).catch(() => null);
      const list = await p.$eval("[data-testid='am-list-panel']", (el) => el.innerText.replace(/\s+/g, " ").slice(0, 200)).catch(() => null);
      return `filled the name field and pressed create — panel: ${panel} | list: ${list}`;
    },
  },
  {
    id: "policy",
    planned: "open the Add-rule form (the button is canWrite-gated — its clickability is the canWrite witness)",
    act: (p) => click(p, "[data-testid='policy-section'] button:has-text('Add')", 6000, "opened the Add form"),
  },
  {
    id: "mcp",
    planned: "open the Add-server form",
    act: (p) => click(p, "[data-testid='add-server']", 6000, "opened the Add-server form"),
  },
  {
    id: "tools",
    planned: "switch to the by-agent tab (the tab strip is the kit's Tabs — role=tab)",
    act: (p) => click(p, "[role='tab']:has-text('By agent')", 5000, "switched to the by-agent tab"),
  },
  {
    id: "gates",
    planned: "open the component picker (the page gates its controls behind one; a fresh store may offer zero)",
    act: async (p) => {
      const empty = await p.$("[data-testid='gates-pick-empty']");
      if (empty) return "picker is empty — a fresh store has no component root to pick";
      const pick = await p.$("[data-testid='gates-picker']");
      if (!pick) return "no picker found";
      await pick.click({ timeout: 5000 });
      await p.waitForTimeout(400);
      const opt = await p.$$("select[name] option, [role='option']");
      await p.keyboard.press("Escape");
      return `opened the picker (${opt.length} options visible)`;
    },
  },
  {
    id: "environments",
    planned: "open the create-environment form",
    act: (p) => click(p, "[data-testid='environments-create']", 6000, "opened the environment form"),
  },
  {
    id: "automation",
    planned: "open the create-schedule form",
    act: (p) => click(p, "[data-testid='create-schedule']", 6000, "opened the schedule form"),
  },
  {
    id: "triage",
    planned: "open the component-root picker (the board answers one root at a time; a fresh store may offer zero)",
    act: async (p) => {
      const needs = await p.$("[data-testid='triage-needs-root']");
      if (needs) return "board asks for a component root first — no self-file control on the page";
      const root = await p.$("select[data-testid='triage-filter-root'], [data-testid='triage-filter-root'] select, [data-testid='triage-filter-root']");
      if (!root) return "no root control found";
      await root.click({ timeout: 5000 });
      await p.waitForTimeout(400);
      await p.keyboard.press("Escape");
      return "opened the root picker";
    },
  },
  {
    id: "memory",
    planned: "toggle show-retracted (a board page — no add form exists: agents add, a person retracts)",
    act: (p) => click(p, "[data-testid='memory-filter-retracted-label']", 4000, "toggled show-retracted"),
  },
  {
    id: "continuous",
    planned: "read the scheduler status (pressing nothing — enable/pause carry seatbelts this walk must not pull)",
    settle: 1500,
    act: async (p) => {
      const t = await p.textContent("[data-testid='continuous-section']").catch(() => null);
      const s = (t ?? "").trim().slice(0, 240);
      return `read the scheduler status — "${s}"`;
    },
  },
  {
    id: "platform-status",
    planned: "read the platform blocks and open the activity tab",
    act: (p) => click(p, "[data-testid='tab-activity']", 5000, "opened the activity tab"),
  },
];

async function walkPage(browser, page, entry, results) {
  const errors = [];
  const onPageError = (e) => errors.push(`pageerror: ${e.message}`);
  const onConsole = (m) => { if (m.type() === "error") errors.push(`console: ${m.text().slice(0, 220)}`); };
  page.on("pageerror", onPageError);
  page.on("console", onConsole);
  await page.goto(`${BASE}#/config/${entry.id}`, { waitUntil: "networkidle" }).catch((e) => errors.push(`goto: ${e.message}`));
  await page.waitForTimeout(entry.settle ?? 700); // the lazy chunk + the first RPC round trip
  const probe = async () => page.evaluate(() => {
    const frame = document.querySelector("[data-testid='config-page-frame']");
    const alert = document.querySelector("[data-testid='tab-needs-connection']");
    const boundary = document.querySelector("[data-testid='tab-boundary']");
    const missing = document.querySelector("[data-testid='config-page-missing']");
    const section = frame?.querySelector("[data-testid$='-section'], [data-testid$='-panel'], [data-testid='seed-records']");
    return {
      framed: !!frame, sectionRoot: section?.getAttribute("data-testid") ?? null,
      notConnected: !!alert, boundary: !!boundary, missing: !!missing,
      headings: [...(frame?.querySelectorAll("h1,h2") ?? [])].map((h) => `${h.tagName}: ${h.textContent?.trim().slice(0, 48)}`),
      bodyText: (frame?.textContent ?? "").trim().slice(0, 300),
    };
  });
  let state = await probe();
  const before = JSON.parse(JSON.stringify(state));
  let actionResult = "not attempted";
  try {
    actionResult = await entry.act(page);
    await page.waitForTimeout(400);
    state = await probe();
  } catch (e) { actionResult = `action failed: ${e.message.split("\n")[0]}`; }
  if (shots) await page.screenshot({ path: resolve(shots, `config-${entry.id}.png`), fullPage: false }).catch(() => {});
  page.off("pageerror", onPageError);
  page.off("console", onConsole);
  results.push({ id: entry.id, planned: entry.planned, before, after: state, action: actionResult, errors: errors.slice(0, 6) });
  log(`${entry.id.padEnd(17)} framed=${state.framed} root=${state.sectionRoot} changed=${before.bodyText !== state.bodyText} errors=${errors.length}`);
}

if (argv.includes("--disconnect")) {
  // Acceptance 6 with the daemon REALLY down: the shell's not-connected state,
  // never an empty or zero page.
  const errors = [];
  const browser0 = await chromium.launch({ channel: "chrome", headless: true, args: ["--no-sandbox", "--disable-dev-shm-usage"] });
  const context0 = await browser0.newContext({ viewport: { width: 1440, height: 900 } });
  await context0.addInitScript(initScript);
  const dpage = await context0.newPage();
  dpage.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  await dpage.goto(`${BASE}#/config/policy`, { waitUntil: "load" }).catch((e) => errors.push(`goto: ${e.message}`));
  let d = null;
  for (let i = 0; i < 40; i++) {
    d = await dpage.evaluate(() => ({
      alert: !!document.querySelector("[data-testid='tab-needs-connection']"),
      framed: !!document.querySelector("[data-testid='config-page-frame']"),
      boundary: !!document.querySelector("[data-testid='tab-boundary']"),
      bodyText: (document.querySelector("[data-testid='config-page-frame']")?.textContent ?? document.body.textContent ?? "").trim().slice(0, 200),
    }));
    if (d?.alert) break;
    await dpage.waitForTimeout(250);
  }
  if (shots) await dpage.screenshot({ path: resolve(shots, "config-disconnected.png") }).catch(() => {});
  writeFileSync(process.env.WALK_JSON ?? "walk-disconnect.json", JSON.stringify({ disconnectWalk: d, errors: errors.slice(0, 6) }, null, 2));
  log(`disconnect walk: ${JSON.stringify(d)}`);
  await browser0.close();
  process.exit(0);
}

const browser = await chromium.launch({ channel: "chrome", headless: true, args: ["--no-sandbox", "--disable-dev-shm-usage"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
await context.addInitScript(initScript);
const page = await context.newPage();
const results = [];

log("=== connected walk ===");
for (const entry of WALK) {
  if (only && entry.id !== only) continue;
  await walkPage(browser, page, entry, results);
}

log("=== narrow viewport + focus ===");
await page.setViewportSize({ width: 900, height: 800 });
await page.goto(`${BASE}#/config`, { waitUntil: "networkidle" });
await page.waitForTimeout(500);
const narrow = await page.evaluate(() => {
  const asides = [...document.querySelectorAll("aside")];
  const panel = asides.find((a) => a.textContent?.includes("Daemon settings"));
  const rail = asides[0];
  return { railWidth: rail?.getBoundingClientRect().width ?? 0, panelVisible: !!panel, overflowX: document.documentElement.scrollWidth > document.documentElement.clientWidth };
});
const tabTrail = [];
for (let i = 0; i < 8; i++) {
  await page.keyboard.press("Tab");
  tabTrail.push(await page.evaluate(() => document.activeElement?.getAttribute("data-testid") ?? document.activeElement?.tagName ?? "-"));
}
log(`narrow: ${JSON.stringify(narrow)} focus: ${tabTrail.join(" → ")}`);

writeFileSync(process.env.WALK_JSON ?? "walk-results.json", JSON.stringify({ results, narrow, tabTrail }, null, 2));
log("wrote walk-results.json");
await browser.close();
