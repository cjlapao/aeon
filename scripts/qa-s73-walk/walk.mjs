// S73 T8 — the browser walk of `/config/agents`, the row's verbs, the ledger tab,
// and the roads the CLI owns.
//
// Drives the SYSTEM Chrome (channel: "chrome", the S72 T4 choice) against
// `../qa-s72-walk/bridge.mjs` — the plain-browser stand-in for the Tauri shell —
// with the REAL daemon on the other side, on a throwaway `XDG_STATE_HOME` that is
// never the real store. The desktop shell is not buildable in this tree this
// session (the same named gap S72 recorded), so the walk runs through the stubbed
// shell and `walk-notes` say what that cannot show.
//
// Every line this prints is an OBSERVATION, not a claim: `STEP <n> — <what> ::
// <what the page or the daemon actually said>`. A step that cannot run says so in
// its own line rather than being skipped quietly, and the exit code is 0 only
// when no step reported `FAIL`.
//
// Run: see run.sh. Three shells, in order — daemon, bridge, this.
//   daemon:  XDG_STATE_HOME=<scratch>/state LOCALRPC_ENDPOINT_PATH=<scratch>/run/rpc.sock aeon serve
//   bridge:  node ../qa-s72-walk/bridge.mjs --sock <rpc.sock> --dist ../../ui/dist --port 8731
//   walk:    PW=<repo>/node_modules/playwright-core AEON=<the binary> WALK_URL=http://127.0.0.1:8731/ \
//              node walk.mjs            # steps 1–8, with the CLI roads inline
//   walk 2:  PW=… WALK_URL=… node walk.mjs --disconnect   # step 9, daemon down
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { initScript } from "../qa-s72-walk/tauri-stub.mjs";

const require = createRequire(import.meta.url);
const PW = process.env.PW ?? resolve(import.meta.dirname, "../../../node_modules/playwright-core");
const { chromium } = require(PW);
const BASE = process.env.WALK_URL ?? "http://127.0.0.1:8731/";
const AEON = process.env.AEON ?? resolve(import.meta.dirname, "../../.s73-scratch/bin/aeon");
const SOCK = process.env.LOCALRPC_ENDPOINT_PATH ?? "";
const STATE = process.env.XDG_STATE_HOME ?? "";
const DISCONNECT = process.argv.includes("--disconnect");

const log = (...a) => process.stderr.write(a.join(" ") + "\n");
let failures = 0;
const step = (n, what, observed) => {
  const bad = /^FAIL/.test(String(observed));
  if (bad) failures += 1;
  log(`STEP ${n} — ${what} :: ${observed}${bad ? " <<< FAIL" : ""}`);
};

/** The CLI as a person runs it: the same binary, the same socket, the same store. */
function cli(...args) {
  try {
    const out = execFileSync(AEON, args, {
      env: { ...process.env, XDG_STATE_HOME: STATE, LOCALRPC_ENDPOINT_PATH: SOCK },
      encoding: "utf8",
      timeout: 120000,
    });
    return out.replace(/\s+/g, " ").trim();
  } catch (cause) {
    // A refusal is an answer. The walk prints the daemon's sentence, not a stack.
    const text = `${String(cause.stdout ?? "")}${String(cause.stderr ?? "")}`.replace(/\s+/g, " ").trim();
    return text === "" ? `FAILED to run: ${cause.message}` : text;
  }
}

/**
 * The same ask the panel makes, made directly through the shell command the web
 * layer uses, so a question about WHAT TRAVELS is answered from the wire rather
 * than from what a component happened to render. `raw` is the rejection the web
 * layer receives, verbatim.
 */
const rawAsk = (p, method, params) =>
  p
    .evaluate(
      async ([m, pr]) => {
        try {
          const answer = await window.__TAURI_INTERNALS__.invoke("rpc_call", { method: m, params: pr });
          return `answered: ${String(answer).slice(0, 200)}`;
        } catch (cause) {
          return `rejected with ${typeof cause}: ${String(cause).slice(0, 300)}`;
        }
      },
      [method, params],
    )
    .catch((cause) => `FAIL the probe itself: ${cause.message}`);

const text = (p, sel) => p.$eval(sel, (el) => (el.textContent ?? "").replace(/\s+/g, " ").trim()).catch(() => null);
const value = (p, sel) => p.$eval(sel, (el) => String(el.value ?? "")).catch(() => null);
const count = (p, sel) => p.$$eval(sel, (els) => els.length).catch(() => 0);
async function click(p, sel, ms = 6000) {
  const el = await p.$(sel);
  if (!el) return false;
  await el.click({ timeout: ms });
  await p.waitForTimeout(450);
  return true;
}
/** The row's own menu, then the item inside it, by the label a person reads. */
async function rowVerb(p, slug, label) {
  if (!(await click(p, `[data-testid="agents-row-menu-${slug}"]`))) {
    step("harness", `open the row menu for ${slug}`, `FAIL no menu (the row is not on the screen the walk is looking at)`);
    return null;
  }
  const items = await p.$$('[role="menu"] [role="menuitem"]');
  for (const item of items) {
    const name = await item.evaluate((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
    const disabled = await item.evaluate((el) => el.getAttribute("aria-disabled") === "true" || el.disabled === true);
    if (name.startsWith(label)) {
      if (disabled) return { name, disabled: true };
      await item.click();
      await p.waitForTimeout(600);
      return { name, disabled: false };
    }
  }
  await p.keyboard.press("Escape");
  return { name: `no item beginning "${label}"`, disabled: null };
}
const closePanel = async (p) => {
  await click(p, 'button[aria-label="Close panel"]');
  // A dirty draft asks before it goes; the walk always discards.
  const discard = await p.$('[data-testid="agents-discard-ask"]');
  if (discard) await click(p, '[data-testid="agents-discard-ask"] button:has-text("Discard the draft")');
  await p.waitForTimeout(350);
};
const reloadToAgents = async (p) => {
  // A real reload: the page owns its window (sort, page, term) in its own state,
  // and a person arriving at the page gets the daemon's default order — which is
  // what step 2.1 is about. A hash-navigation to the same URL would not remount.
  await p.goto(BASE + "#/config/agents", { waitUntil: "networkidle" });
  await p.reload({ waitUntil: "networkidle" });
  await p.waitForSelector('[data-testid="agents-table"]', { timeout: 20000 });
  await p.waitForFunction(() => document.querySelectorAll('[data-testid^="agents-cell-slug-"]').length > 0, undefined, { timeout: 20000 });
};

/**
 * Find one row the way a person does: type into the page's search, which the
 * daemon answers (`q` goes on the wire; the page filters nothing locally). Rows
 * the walk created are not on page 1 of the default window, and pretending they
 * were would have the walk fail on its own arithmetic rather than on the app.
 */
async function findRow(p, term) {
  await p.fill('[data-testid="agents-search"] input', term);
  const seen = await p
    .waitForFunction((t) => !!document.querySelector(`[data-testid="agents-cell-slug-${t}"]`), term, { timeout: 15000 })
    .then(() => true)
    .catch(() => false);
  return seen;
}

const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
await page.addInitScript(initScript);

if (DISCONNECT) {
  // Step 9 — the daemon is down. What the page shows, and whether anything a
  // person could press to change the store is actually pressed.
  await page.goto(BASE + "#/config/agents", { waitUntil: "networkidle" });
  await page.waitForTimeout(4000);
  const body = await page.evaluate(() => document.body.innerText.replace(/\s+/g, " ").slice(0, 600));
  const needsConn = await count(page, '[data-testid="tab-needs-connection"]');
  step("2.9", "the page with the daemon down", `${needsConn > 0 ? "the shell's own not-connected state (tab-needs-connection) is on screen" : "no tab-needs-connection testid"}; page says: ${body.slice(0, 200)}`);
  const bar = await page.evaluate(() => {
    const out = [];
    for (const id of ["agents-create", "agents-import", "agents-migrate"]) {
      const el = document.querySelector(`[data-testid="${id}"]`);
      out.push(`${id}=${el ? (el.disabled ? "disabled" : "ENABLED") : "absent"}`);
    }
    return out.join(" ");
  });
  step("2.9", "every write control the page could press", /ENABLED/.test(bar) ? `FAIL ${bar}` : `${bar} — the screen's own action bar never renders at all, which is a stronger state than disabled controls`);
  step("2.9", "a row menu, if any row is on screen", await count(page, '[data-testid^="agents-row-menu-"]') === 0 ? "no rows offered (the window never loaded)" : "rows present — see the next step for their menus");
  await browser.close();
  log(`WALK DISCONNECT DONE failures=${failures}`);
  process.exit(failures === 0 ? 0 : 1);
}

// ---------------------------------------------------------------- step 2.1
await reloadToAgents(page);
const paint = await page.evaluate(() => {
  const root = document.querySelector('[data-testid="agents-table"]');
  const spans = [...root.querySelectorAll("span")].map((s) => (s.textContent ?? "").trim());
  const ths = [...root.querySelectorAll("thead th")].map((th) => (th.textContent ?? "").replace(/\s+/g, " ").trim());
  const frame = document.querySelector('[data-testid="config-page-frame"]');
  return {
    showing: spans.find((t) => /results$/.test(t)) ?? null,
    pageOf: spans.find((t) => /^Page /.test(t)) ?? null,
    rows: root.querySelectorAll("tbody tr").length,
    heads: ths,
    firstSlugs: [...root.querySelectorAll('[data-testid^="agents-cell-slug-"]')].slice(0, 3).map((td) => td.textContent ?? ""),
    frameW: frame ? Math.round(frame.getBoundingClientRect().width) : null,
    docScrollW: (document.scrollingElement ?? document.body).scrollWidth,
    winW: window.innerWidth,
    internalPills: [...root.querySelectorAll("tbody tr")].slice(0, 3).map((tr) => (tr.textContent ?? "").includes("first-party") ? "first-party" : "yours"),
  };
});
step("2.1", "first paint of /config/agents", `${paint.rows} rows, ${paint.showing}, ${paint.pageOf}, first slugs ${JSON.stringify(paint.firstSlugs)}`);
step("2.1", "the columns as shipped, and the shell's width", `heads ${JSON.stringify(paint.heads)}; frame ${paint.frameW}px in a ${paint.winW}px window, document scrollWidth ${paint.docScrollW}${paint.docScrollW <= paint.winW ? " (no sideways page scroll)" : " <<< sideways page scroll"}`);
step("2.1", "the same window's rows are stamped as first-party or yours", JSON.stringify(paint.internalPills));

// ---------------------------------------------------------------- step 2.2
const firstSlugBefore = paint.firstSlugs[0] ?? "";
await click(page, 'thead th:has-text("Updated") button[aria-label*="sort" i], thead th:has-text("Updated") button');
const sorted = await page.evaluate(() => {
  const th = [...document.querySelectorAll("thead th")].find((h) => (h.textContent ?? "").includes("Updated"));
  const cells = [...document.querySelectorAll('[data-testid^="agents-cell-slug-"]')];
  return { ariaSort: th?.getAttribute("aria-sort") ?? null, first: cells[0]?.textContent ?? null };
});
step("2.2", "sort by updatedAt (the header\u2019s first press)", `aria-sort=${sorted.ariaSort}; first row now "${sorted.first}" (was "${firstSlugBefore}")${sorted.ariaSort === "descending" || sorted.ariaSort === "ascending" ? "" : " <<< the header did not take the sort"}`);
await click(page, '[data-testid="agents-table"] button:has-text("Next")');
step("2.2", "page 2 of the same window", (await text(page, '[data-testid="agents-table"]'))?.match(/Page \d+ of \d+/)?.[0] ?? "no Page x of y after Next");
await page.fill('[data-testid="agents-search"] input', "zzzz-nothing-matches-this");
await page.waitForTimeout(1400);
step("2.2", 'search "zzzz-nothing-matches-this"',
  (await text(page, '[data-testid="agents-empty-search"]')) ??
  (await text(page, '[data-testid="agents-empty-none"]')) ??
  (await text(page, '[data-testid="agents-empty-past-end"]')) ??
  `FAIL no empty state; rows on screen: ${await count(page, '[data-testid^="agents-cell-slug-"]')}`);
await page.fill('[data-testid="agents-search"] input', "%");
await page.waitForTimeout(1400);
const likeRows = await count(page, '[data-testid^="agents-cell-slug-"]');
step("2.2", 'search "%" (the LIKE metacharacter)', `${likeRows} rows back — ${likeRows > 1 ? "the daemon matched everything, so a bare % is a wildcard on this door and the page showed its answer unchanged" : "the daemon treated it literally"}`);
await page.fill('[data-testid="agents-search"] input', "");
await page.waitForTimeout(1400);

// ---------------------------------------------------------------- step 2.3
await click(page, '[data-testid="agents-create"]');
await page.waitForSelector('[data-testid="agent-panel-create"]', { timeout: 8000 });
await page.fill("#agent-field-name", "Walk Probe");
await page.fill("#agent-field-description", "Laid by the S73 walk, and deleted by it");
await page.fill("#agent-field-functionality", "read, list");
await click(page, '[data-testid="agent-panel-create-run"]', 12000);
await page.waitForSelector('[data-testid="agent-panel-created-slug"]', { timeout: 15000 });
const createdSlug = (await text(page, '[data-testid="agent-panel-created-slug"]')) ?? "";
step("2.3", "New agent → agent.create", `the panel says: ${createdSlug} (the slug is the daemon's, not the page's)`);
await closePanel(page);

// The hazard, walked: a duplicate of a first-party row carries real content
// (the seeded prompts are thousands of characters), so a nine-field form that
// posted only its own fields would erase it in a row that still looked fine.
const contentSource = paint.firstSlugs.find((s) => s && s !== "walk-probe") ?? "brief-author";
const dup = await rowVerb(page, contentSource, "Duplicate");
step("2.3", "the row menu offers Duplicate on a first-party row", dup ? `"${dup.name}" (disabled: ${dup.disabled})` : "FAIL no menu for this row");
await page.waitForSelector('[data-testid="agent-duplicate-panel"]', { timeout: 8000 });
await page.fill('[data-testid="agent-duplicate-name"]', "Walk Copy");
await click(page, '[data-testid="agent-duplicate-run"]', 15000);
await page.waitForSelector('[data-testid="agent-duplicate-slug"]', { timeout: 15000 });
const dupText = (await text(page, '[data-testid="agent-duplicate-slug"]')) ?? "";
const copySlug = dupText.match(/\b[a-z][a-z0-9]*(?:-[a-z0-9]+)+\b/)?.[0] ?? "walk-copy";
step("2.3", "Duplicate a first-party row (AM1: a copy is editable, the original is not)", `the panel says: ${dupText}`);
await closePanel(page);

const copyRow = "walk-copy";
await reloadToAgents(page);
const before = await (async () => {
  if (!(await findRow(page, copyRow))) return null;
  if (!(await rowVerb(page, copyRow, "Edit"))) return null;
  await page.waitForSelector('[data-testid="agent-panel-edit"]', { timeout: 8000 });
  const out = {
    prompt: await value(page, "#agent-field-prompt"),
    metadata: await value(page, "#agent-field-metadata"),
    version: await text(page, '[data-testid="agent-readonly-version"]'),
  };
  out.promptLength = (out.prompt ?? "").length;
  await page.fill("#agent-field-description", "The description alone was edited, by the walk.");
  step("2.3", "the changed line names what changed", (await text(page, '[data-testid="agent-panel-changed"]')) ?? "FAIL no changed line");
  await click(page, '[data-testid="agent-panel-save"]', 20000);
  await page.waitForSelector('[data-testid="agent-panel-edited"]', { timeout: 20000 });
  step("2.3", "Save → agent.edit", await text(page, '[data-testid="agent-panel-edited"]') ?? "FAIL no edited answer");
  await closePanel(page);
  return out;
})();
step("2.3", "the copy's prompt as loaded before the edit", `${before?.promptLength ?? "FAIL could not open the copy"} characters of prompt in the textarea`);
if (before) {
  await reloadToAgents(page);
  await findRow(page, copyRow);
  await rowVerb(page, copyRow, "Edit");
  await page.waitForSelector('[data-testid="agent-panel-edit"]', { timeout: 8000 });
  const afterPrompt = await value(page, "#agent-field-prompt");
  const afterMeta = await value(page, "#agent-field-metadata");
  const afterDesc = await value(page, "#agent-field-description");
  const kept = (afterPrompt ?? "").length === before.promptLength && afterMeta === before.metadata;
  step("2.3", "AR5, walked: reopen after editing the description alone", `${(afterPrompt ?? "").length}/${before.promptLength} characters of prompt, metadata ${afterMeta === before.metadata ? "unchanged" : "CHANGED"}, description "${afterDesc}" — ${kept ? "the nine unrendered keys survived" : "FAIL content was lost"}`);
  const factRows = await page.$$eval(
    '[data-testid^="agent-readonly-"]',
    (els) => els.filter((el) => !/-(reason|immutable)$/.test(el.getAttribute("data-testid") ?? "")).length,
  );
  step("2.3", "the read-only half of the same row", `${factRows} facts shown as rows, and the reason: ${(await text(page, '[data-testid="agent-readonly-reason"]'))?.slice(0, 110) ?? "none"}`);
  await closePanel(page);
}

// ---------------------------------------------------------------- step 2.4
const dir = mkdtempSync(join(tmpdir(), "s73-walk-"));
const cardPath = join(dir, `${copyRow}.card.json`);
await findRow(page, copyRow);
const openForExport = await rowVerb(page, copyRow, "Export");
await page.waitForSelector('[data-testid="agent-export-panel"]', { timeout: openForExport ? 8000 : 1 }).catch(() => null);
await page.fill('[data-testid="agent-export-path"]', cardPath);
await click(page, '[data-testid="agent-export-run"]', 15000);
await page.waitForSelector('[data-testid="agent-export-answer-line"]', { timeout: 15000 });
step("2.4", "Export to a path → agent.export", await text(page, '[data-testid="agent-export-answer-line"]') ?? "FAIL no answer line");
step("2.4", "the file the daemon wrote", (() => {
  try {
    const card = JSON.parse(readFileSync(cardPath, "utf8"));
    return `${Object.keys(card).length} keys, version ${card.version}, prompt ${String(card.prompt ?? "").length} chars`;
  } catch (cause) {
    return `FAIL nothing readable at ${cardPath}: ${cause.message}`;
  }
})());
await closePanel(page);

// Import, three ways: the refusal while the seatbelt holds, then created, no-op
// and updated once a person has opened it. The setting is the daemon's own
// (S44's first production setting) and the CLI is its client.
await click(page, '[data-testid="agents-import"]');
await page.waitForSelector('[data-testid="agent-panel-import"]', { timeout: 8000 });
// The import panel takes a path, so the row it will touch is a fact of the file,
// not of the window — no search needed here.
//
// `agent.export` writes a FLAT card (eleven keys, `cmd/aeon/agent_management.go`'s
// `exportCard`); `agent.import` unmarshals a `SignedAgentCard` envelope and
// refuses one whose `card` body is absent (`agentmgr/helpers.go:38`). So
// importing the export's OWN file is the first observation — the round trip the
// two panels invite does not exist (G-3) — and the walk then lays the envelope
// the door asks for, under a fresh slug, so AM3's three words get walked for
// real rather than quoted from a test: created, no-op, updated.
const tryImport = async (path) => {
  await page.fill('[data-testid="agent-import-path"]', path);
  await click(page, '[data-testid="agent-panel-import-run"]', 20000);
  await page.waitForTimeout(1400);
  const word = await text(page, '[data-testid="agent-import-word"]');
  if (word !== null) return `word: ${word}`;
  const errorLine = await text(page, '[data-testid="agent-panel-error"]');
  return errorLine === null ? "FAIL nothing said" : errorLine.slice(0, 260);
};
step("2.6", "Import the export's own file (the round trip the two panels invite)", await tryImport(cardPath));
step("2.6", "what the shell handed the web layer for that refusal", await rawAsk(page, "agent.import", { path: cardPath }));

const freshCard = join(dir, "walk-imported.card.json");
const freshEnvelope = join(dir, "walk-imported.envelope.json");
const v99Envelope = join(dir, "walk-imported.v99.envelope.json");
try {
  const card = JSON.parse(readFileSync(cardPath, "utf8"));
  card.slug = "walk-imported";
  card.name = "Walk Imported";
  writeFileSync(freshCard, JSON.stringify(card), { mode: 0o600 });
  writeFileSync(freshEnvelope, JSON.stringify({ card }), { mode: 0o600 });
  card.version = "99";
  writeFileSync(v99Envelope, JSON.stringify({ card }), { mode: 0o600 });
} catch (cause) {
  step("2.6", "laying the envelope the door asks for", `FAIL ${cause.message}`);
}
step("2.6", "the envelope for a new slug, seatbelt still closed (S44's setting)", await tryImport(freshEnvelope));
step("2.6", "aeon settings set agents.allowUnsignedImport", cli("settings", "set", "agents.allowUnsignedImport", "true").slice(0, 180));
step("2.6", "the same envelope with the seatbelt open — AM3's first word", await tryImport(freshEnvelope));
step("2.6", "…the same file again — AM3's second word", await tryImport(freshEnvelope));
step("2.6", "…and the same card at version 99 — AM3's third word", await tryImport(v99Envelope));
step("2.6", "what the panel says about the consent review the CLI prints", (await text(page, '[data-testid="agent-panel-import-consent"]'))?.slice(0, 160) ?? "FAIL nothing");
step("2.6", "aeon settings set agents.allowUnsignedImport false", cli("settings", "set", "agents.allowUnsignedImport", "false").slice(0, 120));

// ---------------------------------------------------------------- step 2.5
await reloadToAgents(page);
await click(page, '[data-testid="agents-create"]');
await page.waitForSelector('[data-testid="agent-panel-create"]', { timeout: 8000 });
await page.fill("#agent-field-name", "Walk Victim");
await page.fill("#agent-field-description", "To be deleted by the walk");
await page.fill("#agent-field-functionality", "read");
await click(page, '[data-testid="agent-panel-create-run"]', 12000);
await page.waitForSelector('[data-testid="agent-panel-created-slug"]', { timeout: 15000 });
step("2.5", "a second created row to delete", await text(page, '[data-testid="agent-panel-created-slug"]') ?? "FAIL");
await closePanel(page);

await rowVerb(page, "walk-victim", "Delete");
const dialog = await page.waitForSelector('[role="alertdialog"]', { timeout: 8000 }).then(() => true).catch(() => false);
step("2.5", "Delete asks for a typed confirmation", dialog ? `the alertdialog says: ${(await text(page, '[role="alertdialog"]'))?.slice(0, 150)}` : "FAIL no typed confirmation");
await page.fill('[role="alertdialog"] input', "walk-victi");
const wrongDisabled = await page.$eval('[role="alertdialog"] button:has-text("Delete")', (el) => el.disabled === true || el.getAttribute("aria-disabled") === "true").catch(() => null);
step("2.5", "the wrong slug typed", wrongDisabled === true ? "the confirm button is disabled, and nothing reached the daemon" : `FAIL the button answered to a wrong slug (disabled=${wrongDisabled})`);
await page.fill('[role="alertdialog"] input', "walk-victim");
await click(page, '[role="alertdialog"] button:has-text("Delete")', 15000);
await page.waitForTimeout(1500);
step("2.5", "the right slug typed", (await text(page, '[data-testid="agents-delete-done"]')) ?? `FAIL no success line; still on screen: ${await count(page, '[data-testid="agents-cell-slug-walk-victim"]')}`);
step("2.5", "the row left because the re-read said so", `${await count(page, '[data-testid="agents-cell-slug-walk-victim"]')} cell(s) for walk-victim`);

// The AM4 refusal: a policy row that points at the copy's slug, laid through
// the human door's own client, then read as the person sees it.
step("2.5", "aeon policy add --target agent:walk-copy (a real reference)", cli("policy", "add", "--type", "command_access", "--id", "s73-walk-ref", "--target", "agent:walk-copy", "--action", "deny", "--command", "walk-probe-command").slice(0, 200));
await reloadToAgents(page);
await findRow(page, copyRow);
await rowVerb(page, copyRow, "Delete");
await page.waitForSelector('[role="alertdialog"]', { timeout: 8000 });
await page.fill('[role="alertdialog"] input', copyRow);
await click(page, '[role="alertdialog"] button:has-text("Delete")', 15000);
await page.waitForTimeout(1600);
const refusal = await text(page, '[data-testid="agents-write-error"]');
step("2.5", "delete of a referenced agent (AM4, verbatim)", refusal ? `${refusal.slice(0, 220)}; row still on screen: ${await count(page, '[data-testid="agents-cell-slug-walk-copy"]')}` : "FAIL no refusal shown");
step("2.5", "aeon policy remove --id s73-walk-ref", cli("policy", "remove", "--id", "s73-walk-ref").slice(0, 140));

// ---------------------------------------------------------------- step 2.6–8 (the ledger)
await reloadToAgents(page);
await findRow(page, copyRow);
const openForLedger = await rowVerb(page, copyRow, "Edit");
await page.waitForSelector('[data-testid="agent-panel-edit"]', { timeout: openForLedger ? 8000 : 1 }).catch(() => null);
await click(page, '[role="tab"]:has-text("History"), button:has-text("History")');
await page.waitForSelector('[data-testid="agent-history"]', { timeout: openForLedger ? 15000 : 1 }).catch(() => null);
const scoped = await page.$$eval('[data-testid="agent-history"] [data-testid$="-summary"]', (els) => els.map((el) => el.textContent ?? ""));
step("2.6", "History of the copy, scoped to it", `${scoped.length} entries: ${JSON.stringify(scoped.slice(0, 6))}`);
const actors = await page.$$eval('[data-testid="agent-history"] [data-testid$="-actor"]', (els) => els.map((el) => el.textContent ?? ""));
step("2.6", "who the ledger says made them", JSON.stringify(actors.slice(0, 6)));
const chips = await count(page, '[data-testid$="-proposal"]');
step("2.6", "the `proposal <id>` chip", `${chips} chip(s) — ${chips === 0 ? "no road in this tree mints a proposal-bearing summary (T4's deviation 15, and the store's own comment at agent_audited.go:178 says so); the tab parses one when the summary carries it, and no production summary does" : "parsed from a summary the daemon wrote"}`);
step("2.7", "aeon agent import <dir> on the walk scratch directory (it holds exported CARD files, not a package: no agent.md, no signed card.json)", cli("agent", "import", dir).slice(0, 260));
step("2.7", "…and what it left in the ledger", cli("agent", "list").slice(0, 120));
await click(page, '[data-testid="agent-history-scope"] [role="radio"]:has-text("Every agent")');
await page.waitForTimeout(1400);
step("2.6", "the toggle without the slug", `${await count(page, '[data-testid="agent-history"] [data-testid$="-summary"]')} entries; every-agent subjects: ${JSON.stringify((await page.$$eval('[data-testid="agent-history"] [data-testid$="-subject"]', (els) => [...new Set(els.map((el) => el.textContent ?? ""))])).slice(0, 4))}`);
await closePanel(page);
await findRow(page, copyRow);

await click(page, '[data-testid="agents-migrate"]', 8000);
await page.waitForSelector('[data-testid="agent-migrate-modal"]', { timeout: 8000 });
step("2.8", "what the migrate confirm says before it runs", ((await text(page, '[data-testid="agent-migrate-body"]')) ?? "").slice(0, 200));
await click(page, '[data-testid="agent-migrate-run"]', 25000);
step("2.8", "Migrate (path 6) → agent.migrate", await text(page, '[data-testid="agent-migrate-answer"]') ?? "FAIL no answer");
// The modal stays open to show the answer, and its overlay holds the screen —
// closing it is what a person does next, and what the walk must do to go on.
await click(page, '[data-testid="agent-migrate-close"]', 6000);
await findRow(page, copyRow);
const openForMigrate = await rowVerb(page, copyRow, "Edit");
await page.waitForSelector('[data-testid="agent-panel-edit"]', { timeout: openForMigrate ? 8000 : 1 }).catch(() => null);
await click(page, '[role="tab"]:has-text("History"), button:has-text("History")');
await page.waitForSelector('[data-testid="agent-history"]', { timeout: openForMigrate ? 15000 : 1 }).catch(() => null);
await click(page, '[data-testid="agent-history-scope"] [role="radio"]:has-text("Every agent")');
await page.waitForTimeout(1600);
const migrateRow = await page.$$eval('[data-testid="agent-history"] [data-testid$="-summary"]', (els) => els.map((el) => el.textContent ?? "").filter((t) => t.startsWith("migrate")));
const migrateActor = await page.$$eval('[data-testid="agent-history"] li', (els) => els.map((el) => `${el.textContent ?? ""}`.replace(/\s+/g, " ")).filter((t) => t.includes("migrate derived")).slice(0, 1));
step("2.8", "the migrate row, unfiltered (AR1's NULL slug, AR6's only view of it)", `${JSON.stringify(migrateRow.slice(0, 2))} — stamped ${migrateActor[0]?.match(/(user|cli|system):[^ ]*/)?.[0] ?? "no actor read"} (the person, not system)`);
await closePanel(page);

// ---------------------------------------------------------------- step 2.10
await page.goto(BASE + "#/bench/agent-management", { waitUntil: "networkidle" });
await page.waitForTimeout(2500);
const bench = await page.evaluate(() => {
  const root = document.querySelector('[data-testid="bench-agent-management"], [data-testid="agent-management-section"], main') ?? document.body;
  return {
    text: (root.textContent ?? "").replace(/\s+/g, " ").slice(0, 260),
    lists: document.querySelectorAll('[data-testid^="agent-list"], table').length,
    panels: document.querySelectorAll('[data-testid*="agent-"]').length,
  };
});
step("2.10", "the Test bench's agent-management page is still the raw panels", `${bench.lists} list(s), ${bench.panels} agent testid(s); says: ${bench.text.slice(0, 200)}`);

await browser.close();
log(`WALK DONE failures=${failures} tmp=${dir}`);
process.exit(failures === 0 ? 0 : 1);
