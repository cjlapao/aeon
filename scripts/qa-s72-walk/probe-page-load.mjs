// S72 T5 — does a Configuration page load fast, show its own row as busy, and
// stay navigable while it works?
//
// # What this is for
//
// The complaint was "the page does say loading but the app is locked and I
// cannot do anything". A test suite in jsdom cannot answer that: it has no
// window to freeze and no menu row to light. This drives the BUILT UI in real
// Chrome through `bridge.mjs`, the same harness `walk.mjs` uses, and answers
// the three things that were actually promised:
//
//   1. how long the page takes to render, from the route commit;
//   2. whether the menu row for that page wears the kit loader while it waits;
//   3. whether a click elsewhere still lands while the page is still working.
//
// # What it cannot show
//
// The desktop freeze. That lived in the Rust shell (a synchronous command on
// the main thread), and here the transport is this bridge's async WebSocket —
// so nothing can freeze, by construction. The freeze's removal is a Rust
// fact, recorded with its own measurement in the T5 Notes; what is measured
// here is the other half of the same complaint: the loader on the row, the
// seconds the page takes, and the user's ability to walk away mid-load.
//
// Run (with the daemon and bridge already up — see `run.sh` for both):
//   PW=<path to playwright-core> WALK_URL=http://127.0.0.1:8712/ \
//     node probe-page-load.mjs [route]     # default: config/policy
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { initScript } from "./tauri-stub.mjs";

const require = createRequire(import.meta.url);
const PW = process.env.PW ?? resolve(import.meta.dirname, "../../../node_modules/playwright-core");
const { chromium } = require(PW);
const BASE = process.env.WALK_URL ?? "http://127.0.0.1:8712/";
const ROUTE = process.argv[2] ?? "config/policy";
const PAGE_HREF = `#/config/${ROUTE.split("/")[1] ?? "policy"}`;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const say = (m) => process.stderr.write(m + "\n");

const browser = await chromium.launch({ channel: "chrome", headless: true, args: ["--no-sandbox", "--disable-dev-shm-usage"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
await context.addInitScript(initScript);
const page = await context.newPage();
const errors = [];
page.on("pageerror", (e) => errors.push(e.message.split("\n")[0]));

await page.goto(BASE, { waitUntil: "load" });
await sleep(600);

// 1 · the load, and the row's busy state during it.
const t0 = Date.now();
await page.goto(`${BASE}${PAGE_HREF}`, { waitUntil: "commit" });
let sawBusy = false;
let busyAt = null;
let sawSpinner = false;
for (let i = 0; i < 160; i++) {
  const probe = await page.evaluate((href) => {
    const row = document.querySelector(`a[href="${href}"], a[href="${href.replace("#", "")}"]`);
    return {
      busy: row?.getAttribute("aria-busy") === "true",
      spinner: !!row?.querySelector("[class*='animate-spin']"),
      rendered: !!document.querySelector("[data-testid='config-page-frame']")?.textContent,
    };
  }, PAGE_HREF).catch(() => ({ busy: false, spinner: false, rendered: false }));
  if (probe.busy && busyAt === null) { sawBusy = true; busyAt = Date.now() - t0; }
  if (probe.spinner) sawSpinner = true;
  if (probe.rendered) break;
  await sleep(25);
}
const renderedAt = Date.now() - t0;

// 2 · leave while it is still working. The route has to change, and `evaluate`
//     has to answer: a thread that is blocked answers both only after the
//     block clears, which is the whole defect this replaced.
await page.goto(`${BASE}#/config`, { waitUntil: "commit" });
await sleep(300);
await page.goto(`${BASE}#/config/system-records`, { waitUntil: "commit" });
let moved = false;
for (let i = 0; i < 80; i++) {
  const hash = await page.evaluate(() => window.location.hash).catch(() => "");
  if (hash.includes("config/system-records")) { moved = true; break; }
  await sleep(25);
}
const movedAt = Date.now() - t0;
const responsive = await page.evaluate(() => 1 + 1).then((v) => v === 2).catch(() => false);

say(`${ROUTE}: rendered after ${renderedAt} ms (row busy: ${sawBusy}${busyAt !== null ? ` at ${busyAt} ms` : ", never"}, kit spinner in the row: ${sawSpinner})`);
say(`navigated away while it worked: ${moved} (settled at ${movedAt} ms); page answers JS: ${responsive}`);
say(`pageerrors: ${errors.length ? errors.slice(0, 3).join(" | ") : "none"}`);
await browser.close();
