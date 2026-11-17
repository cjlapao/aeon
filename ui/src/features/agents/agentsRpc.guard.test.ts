import { describe, expect, it } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  AGENT_AVATAR_MOODS,
  AGENT_AVATAR_TYPES,
  AGENT_CHANGE_OPS,
  AGENT_DEFAULT_PAGE_SIZE,
  AGENT_PAGE_SIZES,
  AGENT_READ_ONLY_FIELDS,
  AGENT_SORTS,
  AGENT_WRITABLE_FIELDS,
  AGENT_WRITABLE_KEYS,
  INTERNAL_DELETE_REFUSAL,
  INTERNAL_EDIT_REFUSAL,
  actorKindTone,
} from "./agentsModel";

// The door guard for the agent management page — the family's shape
// (`blueprintsRpc.guard.test.ts`, `memoryRpc.guard.test.ts` and the rest):
// the census of RPC calls in this page's OWN files equals the doors the daemon
// registers for it, nothing else is named, no paging key is invented, no socket,
// no fetch, no browser storage; and the page's two cross-language claims — that
// it offers only the sorts and sizes the daemon publishes, and that the sentences
// it shows on a read-only row are the daemon's — are compared against the Go
// source rather than believed.
//
// Not vacuous, and it says so: `agents.frobnicate` IS caught and is NOT allowed;
// a synthetic `fet`+`ch(` IS caught while `refetch` is not; a planted
// `"offset":` key IS caught; a planted `.sort(` IS caught; the key scan sees a
// synthetic catalog gap; and both catalogs are checked against each other.

const FEATURE_DIR = __dirname;
const CONTRACT = join(__dirname, "../../proto/contract.ts");
const EN = join(__dirname, "../../lib/i18n/en-agents.json");
const PT = join(__dirname, "../../lib/i18n/pt-agents.json");
// The daemon side of the pins: the two Go files and the two migrations whose
// literals this page repeats — by law, not by taste (AR5, CU6, PG1).
const AGENT_MGR_GO = join(__dirname, "../../../../internal/agentmgr/agentmgr.go");
const AGENT_PAGE_GO = join(__dirname, "../../../../internal/store/agent_page.go");
const AGENT_ROW_SQL = join(__dirname, "../../../../internal/store/migrations/20261030130000_agent.sql");
const AGENT_CHANGE_SQL = join(__dirname, "../../../../internal/store/migrations/20261106181435_agent_change.sql");

/**
 * The ten doors the page may open (AR7: the verb set the page uses is the set
 * that already exists — nothing here asks for a new one). `agent.list` is NOT
 * among them (AR8): it is a bench roster verb, and no page file may call it.
 */
const ALLOWED = new Set([
  "agent.rows",
  "agent.changes",
  "agent.view",
  "agent.create",
  "agent.edit",
  "agent.delete",
  "agent.duplicate",
  "agent.export",
  "agent.import",
  "agent.migrate",
  // The closed tech vocabulary and the tool kits the forms offer; read-only, and the daemon owns the lists.
  "handlers.list",
  // The tool catalog behind "choose individual tools"; read-only.
  "tools.list",
]);

/** The guard has flipped to its full claim: the page opens each of these twelve and NOTHING ELSE. */
const REACHED = [...ALLOWED];

/**
 * The page's own files, as a CLOSED LITERAL LIST (CU8: never a glob — a glob
 * lets a new file walk in unexamined). The refactor of 2026-11-08 consolidated
 * the feature to these six; every one is scanned, so the census covers the
 * whole surface, panel bodies and detail renderer included.
 */
const PRODUCT_FILES = [
  "AgentsScreen.tsx",
  "AgentsTable.tsx",
  "AgentDetail.tsx",
  "AgentVerbs.tsx",
  "agentsRows.ts",
  "agentsLedger.ts",
  "AgentsActivity.tsx",
  "AgentPanel.tsx",
  "AgentCreateBody.tsx",
  "ToolsPicker.tsx",
  "AgentImportBody.tsx",
  "usePanelWrites.ts",
  "AgentFields.tsx",
  "AgentChanges.tsx",
  "agentsModel.ts",
];

const CANARY_METHOD = "agents.frobnicate";

/** Strip line and block comments: a guard reads code, not prose about code. */
function stripComments(src: string): string {
  return src.replace(/\/\/[^\n]*/g, "").replace(/\/\*[\s\S]*?\*\//g, "");
}

function productSources(): Array<{ file: string; src: string }> {
  const out: Array<{ file: string; src: string }> = [];
  for (const name of PRODUCT_FILES) {
    const path = join(FEATURE_DIR, name);
    if (existsSync(path)) out.push({ file: name, src: readFileSync(path, "utf8") });
  }
  return out;
}

/**
 * The census: a literal method name passed to a door of the bridge.
 *
 * `rpcWrite` is the agent panel's own write helper (in AgentPanel.tsx) and is
 * in this list on purpose: a page may route a write through one function of its
 * own, but it may not thereby hide one from the guard. The name is the reason
 * the helper is called that and not `post` — a name this scan cannot see would
 * be a door this scan does not check.
 */
function extractRpcMethods(src: string): string[] {
  const found: string[] = [];
  const re = /\b(?:rpc|rpcTyped|rpcWrite|useRpcQuery)\s*\(\s*["']([^"']+)["']/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    if (match[1] !== undefined) found.push(match[1]);
  }
  return found;
}

function registeredMethods(): Set<string> {
  const src = readFileSync(CONTRACT, "utf8");
  const names = new Set<string>();
  const re = /^ {2}"([a-zA-Z.]+)": \{ params:/gm;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    if (match[1] !== undefined) names.add(match[1]);
  }
  return names;
}

function lookup(catalog: unknown, key: string): string | undefined {
  let node: unknown = catalog;
  for (const part of key.split(".")) {
    if (typeof node !== "object" || node === null) return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === "string" ? node : undefined;
}

/** One namespace of a catalog, or undefined when a file has none. */
function section(doc: unknown, name: string): unknown {
  return typeof doc === "object" && doc !== null ? (doc as Record<string, unknown>)[name] : undefined;
}

function keySet(node: unknown, prefix = ""): string[] {
  const out: string[] = [];
  if (typeof node !== "object" || node === null) return out;
  for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
    const path = prefix === "" ? key : `${prefix}.${key}`;
    out.push(path);
    out.push(...keySet(value, path));
  }
  return out;
}

describe("agentsRpc guard (S73 CU8)", () => {
  it("runs over the real page source (not vacuous)", () => {
    const sources = productSources();
    // The closed list is the whole feature: every named file is here and read.
    expect(sources.map((s) => s.file).sort(), "a product file is missing from the folder").toEqual(
      [...PRODUCT_FILES].sort(),
    );
    expect(productSources().some((s) => extractRpcMethods(s.src).length > 0)).toBe(true);
  });

  it("calls only the agent doors this page owns, all of them registered", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(100);
    const all = productSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length, "the page calls no method at all — the census is reading nothing").toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S73 set: ${offenders.join(", ")}`).toEqual([]);
    const unregistered = all.filter((m) => !registered.has(m));
    expect(unregistered, `methods the contract does not register: ${unregistered.join(", ")}`).toEqual([]);
  });

  it("opens every one of the ten doors it owns, and no other", () => {
    const reached = new Set(productSources().flatMap(({ src }) => extractRpcMethods(src)));
    expect([...reached].sort(), "the page stopped calling a door it owns").toEqual(
      [...REACHED].sort(),
    );
    // T6's own door must be there, or the page is not the page.
    expect(reached.has("agent.rows"), "AgentsScreen must read agent.rows").toBe(true);
    // And the census is a set equality, so a name appearing nowhere in this list
    // (a roster verb, or a foreign plane's write) fails here as well as above.
    expect(reached.has("agent.list"), "AR8: this page does not read the bench roster").toBe(false);
  });

  it("invents no paging key beyond PG1's five", () => {
    // The keys a paged read may carry are `page`, `pageSize`, `sort`, `dir`,
    // `q`. Anything else would be a control the page grew and the daemon
    // refuses — which is PG1's own rule, checked where the control is built.
    const invented = /\b["']?(limit|offset|cursor|perPage|sortBy|orderDirection|count|from|size)["']?\s*:/;
    const offenders = productSources()
      .map(({ file, src }) => ({ file, hit: invented.test(stripComments(src)) }))
      .filter((entry) => entry.hit)
      .map((entry) => entry.file);
    expect(offenders, `files naming a paging key the daemon does not take: ${offenders.join(", ")}`).toEqual([]);
    // Teeth: the shape is really caught, and the five legal keys are not.
    expect(invented.test('const p = { "offset": 20 };')).toBe(true);
    expect(invented.test("rpcTyped(\"agent.rows\", { page: 1, pageSize: 20, sort: \"name\", dir: \"asc\", q: \"\" })")).toBe(false);
  });

  it("sorts, filters and counts nothing in the browser (PG2's UI half)", () => {
    const local = /\.(sort|filter|slice|splice|reduce|localeCompare)\s*\(/;
    const arithmetic = /Math\.(ceil|floor|round)\s*\(/;
    for (const { file, src } of productSources()) {
      const code = stripComments(src);
      expect(local.test(code), `${file} re-orders or re-slices what the daemon answered`).toBe(false);
      expect(arithmetic.test(code), `${file} computes a page count from a total`).toBe(false);
    }
    // Teeth, on both halves.
    expect(local.test("const rows = data.agents.sort((a, b) => a.name.localeCompare(b.name));")).toBe(true);
    expect(arithmetic.test("const pages = Math.ceil(total / pageSize);")).toBe(true);
  });

  it("never opens a socket, a fetch or the browser's storage", () => {
    const fetchCall = new RegExp("\\bfe" + "tch\\s*\\(");
    for (const { file, src } of productSources()) {
      const code = stripComments(src);
      expect(fetchCall.test(code), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(code), `${file} opens a raw request`).toBe(false);
      expect(/\blocalStorage\b/.test(code), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(code), `${file} uses sessionStorage`).toBe(false);
      expect(/\binvoke\s*\(/.test(code), `${file} invokes a Tauri command`).toBe(false);
      expect(/\bWebSocket\b/.test(code), `${file} opens a socket`).toBe(false);
    }
    // Teeth: the ban catches the thing it names and spares the word that only
    // looks like it.
    expect(fetchCall.test("const r = await fet" + 'ch("/agents");')).toBe(true);
    expect(fetchCall.test("queries.forEach((q) => q.refet" + "ch());")).toBe(false);
    expect(/\blocalStorage\b/.test('const x = "local" + "Storage";')).toBe(false);
  });

  it("has teeth (positive control): a synthetic agents.frobnicate is caught and refused", () => {
    const canary = `const x = useRpcQuery("${CANARY_METHOD}", {}, controller, live);`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("speaks no namespace but its own", () => {
    for (const { file, src } of productSources()) {
      const strays = [...stripComments(src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)]
        .map((m) => m[1] as string)
        .filter((key) => !key.startsWith("agents."));
      expect(strays, `${file} uses foreign keys: ${strays.join(", ")}`).toEqual([]);
    }
  });
});

/**
 * The two cross-language pins. A UI that offers a sort the daemon does not
 * resolve is a header that earns −32602, and a UI that paraphrases a refusal is
 * a second law. Both claims are checked against the Go file that owns them.
 */
describe("agents page against the daemon's own words (PG1, AR5, CU6)", () => {
  const pageGo = readFileSync(AGENT_PAGE_GO, "utf8");
  const mgrGo = readFileSync(AGENT_MGR_GO, "utf8");
  const rowSql = readFileSync(AGENT_ROW_SQL, "utf8");
  const changeSql = readFileSync(AGENT_CHANGE_SQL, "utf8");

  it("offers exactly the sorts `AgentRowBounds.Sorts` resolves", () => {
    const block = /Sorts: map\[string\]string\{([\s\S]*?)\n\t\},/.exec(pageGo);
    const body = block?.[1] ?? "";
    const goSorts = [...body.matchAll(/^\s*"([a-zA-Z]+)":/gm)].map((m) => m[1] as string).sort();
    expect(goSorts.length, "the Go sort map did not parse — the pin would be vacuous").toBeGreaterThan(5);
    expect([...AGENT_SORTS].sort()).toEqual(goSorts);
  });

  it("offers sizes the daemon answers without rewriting, and its default", () => {
    const maxSize = Number(/MaxSize:\s*(\d+)/.exec(pageGo)?.[1]);
    const defaultSize = Number(/DefaultSize:\s*(\d+)/.exec(pageGo)?.[1]);
    expect(maxSize).toBeGreaterThan(0);
    expect(defaultSize).toBeGreaterThan(0);
    expect(AGENT_DEFAULT_PAGE_SIZE).toBe(defaultSize);
    for (const size of AGENT_PAGE_SIZES) {
      expect(size, `${size} is past the daemon's clamp of ${maxSize}`).toBeLessThanOrEqual(maxSize);
    }
    // The kit's own footer seeds its page-size selector with these three, so the
    // page's list and the control's are one list and not two opinions.
    expect([...AGENT_PAGE_SIZES].sort((a, b) => a - b)).toEqual([20, 50, 100]);
  });

  /** One method's whole generated block, `params` and `result` together. */
  function contractBlock(method: string): string {
    const src = readFileSync(CONTRACT, "utf8");
    return new RegExp(`^ {2}"${method}": \\{[\\s\\S]*?\\n {2}\\} \\};`, "m").exec(src)?.[0] ?? "";
  }

  /** One arm of a method block, from its opening line to the block's end. */
  /**
   * One arm of a method block. The cut is the arm's own closing line — two spaces
   * and a brace — so reading `params` cannot pick up what `result` answered.
   */
  function contractArm(method: string, arm: "params" | "result"): string {
    const block = contractBlock(method);
    const start = block.indexOf(`${arm}: {`);
    if (start === -1) return "";
    const rest = block.slice(start);
    const end = rest.indexOf("\n  }");
    return end === -1 ? rest : rest.slice(0, end);
  }

  /** The keys written at one depth — the depth says whose keys they are. */
  function keysAt(text: string, indent: number): string[] {
    return [...text.matchAll(new RegExp(`^ {${indent}}"([a-zA-Z]+)":`, "gm"))].map((m) => m[1] as string);
  }

  /** The `a.<Field> = fields.<Field>` lines `Edit` performs. */
  function editedFields(): string[] {
    return [...mgrGo.matchAll(/^\ta\.([A-Za-z]+) = fields\.([A-Za-z]+)$/gm)].map(
      (m) => m[1] as string,
    );
  }

  /** One `CHECK (<col> IN ('a','b'))` list out of a migration. */
  function checkList(sql: string, column: string): string[] {
    const line = new RegExp(`${column}[^)]*?IN \\(([^)]*)\\)`).exec(sql);
    return [...(line?.[1] ?? "").matchAll(/'([^']*)'/g)].map((m) => m[1] as string);
  }

  const lowerFirst = (word: string): string => word.slice(0, 1).toLowerCase() + word.slice(1);

  it("writes back exactly the eleven content fields `Edit` assigns (AR5)", () => {
    const goFields = editedFields().map(lowerFirst).sort();
    expect(goFields.length, "the assignment scan parsed nothing — the pin would be vacuous").toBeGreaterThan(5);
    expect(goFields, "the page's eleven fields are not the door's eleven").toEqual(
      [...AGENT_WRITABLE_KEYS].sort(),
    );
    // The same ten, counted where the form is built: a spec list that grew an eleventh entry
    // (`version`, `temperature`) would post a value the door silently drops.
    expect(AGENT_WRITABLE_FIELDS.map((spec) => lowerFirst(spec.key)).sort()).toEqual(goFields);
    // Teeth: an eleventh assignment is what this test is looking for, and it sees one.
    expect([..."\ta.Version = fields.Version".matchAll(/^\ta\.([A-Za-z]+) = fields\.([A-Za-z]+)$/gm)].length).toBe(1);
  });

  it("shows every key `agent.view` answers, as an input or as a fact — never as nothing", () => {
    // AR5's census from the contract's own side: the twenty-four keys the read
    // answers are the ten the door writes plus the fourteen this page prints and
    // refuses to edit. A key in none of the three lists is a key a reader would
    // never learn exists, which is the half of the hazard that stays invisible.
    const answered = keysAt(contractArm("agent.view", "result"), 4).sort();
    expect(answered.length).toBeGreaterThan(20);
    const shown = AGENT_READ_ONLY_FIELDS.map((field) => String(field.key)).sort();
    const writable = [...AGENT_WRITABLE_KEYS].map(String).sort();
    const overlap = writable.filter((key) => shown.includes(key));
    expect(overlap, `a key is both edited and shown as immutable: ${overlap.join(", ")}`).toEqual([]);
    const missing = answered.filter((key) => !writable.includes(key) && !shown.includes(key));
    expect(missing, `keys the page neither edits nor shows: ${missing.join(", ")}`).toEqual([]);
    const extra = [...writable, ...shown].filter((key) => !answered.includes(key));
    expect(extra, `keys the page names that the read does not answer: ${extra.join(", ")}`).toEqual([]);
    // The door's own ask is `{slug, fields}`, and the blob is the same twenty-four:
    // the row goes back whole, and the page cannot claim a narrower ask is safe.
    const editAsk = contractArm("agent.edit", "params");
    expect(keysAt(editAsk, 4).sort()).toEqual(["fields", "slug"]);
    expect(keysAt(editAsk, 6).sort(), "`agent.edit`'s fields blob is not the whole row").toEqual(answered);
  });

  it("offers the avatar words the database CHECKs admit, and no others", () => {
    const types = checkList(rowSql, "avatar_type");
    const moods = checkList(rowSql, "avatar_mood");
    expect(types.length, "the CHECK scan parsed nothing — the pin would be vacuous").toBeGreaterThan(1);
    expect([...AGENT_AVATAR_TYPES]).toEqual(types);
    expect([...AGENT_AVATAR_MOODS]).toEqual(moods);
    // The empty word is in both: a card may carry no avatar, and the select has
    // to be able to put it back rather than being stuck with one.
    expect(types).toContain("");
    expect(moods).toContain("");
  });

  it("tones the ledger's own op and actor words, not a vocabulary of its own", () => {
    const ops = checkList(changeSql, "op");
    expect(ops.length, "the ledger op CHECK parsed nothing — the pin would be vacuous").toBeGreaterThan(3);
    expect([...AGENT_CHANGE_OPS]).toEqual(ops);
    // `migrate` is the one op whose row may name no agent, and the page's only
    // NULL-slug rendering is the sentence for that op's table.
    expect(ops).toContain("migrate");

    const kinds = checkList(changeSql, "actor_kind");
    expect(kinds.length).toBeGreaterThan(1);
    const tones = kinds.map((kind) => actorKindTone(kind));
    expect(new Set(tones).size, `two actor kinds share a tone: ${kinds.join(", ")}`).toBe(kinds.length);
    // An unknown kind is not silently one of the three: it reads as its own tone.
    expect(tones).not.toContain(actorKindTone("ghost"));
    expect(actorKindTone(undefined)).toBe(actorKindTone("ghost"));
  });

  it("quotes the daemon's read-only sentences byte for byte", () => {
    expect(mgrGo, "the edit refusal moved and the page still quotes it").toContain(INTERNAL_EDIT_REFUSAL);
    expect(mgrGo, "the delete refusal moved and the page still quotes it").toContain(INTERNAL_DELETE_REFUSAL);
    // Teeth: a paraphrase is NOT the sentence, and the pin sees the difference.
    expect(mgrGo).not.toContain("refused: %s is internal — internal agents refuse edits");
  });
});

describe("agents i18n — honest across both languages", () => {
  const enDoc = JSON.parse(readFileSync(EN, "utf8")) as unknown;
  const ptDoc = JSON.parse(readFileSync(PT, "utf8")) as unknown;

  it("en and pt carry exactly the same key set under agents", () => {
    const enKeys = keySet(section(enDoc, "agents"), "agents").sort();
    const ptKeys = keySet(section(ptDoc, "agents"), "agents").sort();
    expect(enKeys.length).toBeGreaterThan(20);
    expect(ptKeys).toEqual(enKeys);
  });

  it("every key the page composes rides BOTH catalogs", () => {
    const used = new Set<string>();
    for (const { src } of productSources()) {
      for (const match of stripComments(src).matchAll(/\bt\(\s*"(agents\.[a-zA-Z0-9._]+)"/g)) {
        used.add(match[1] as string);
      }
      // Keys built through a table (the verb labels) are checked by their values
      // too, because that is where a silent half-translation hides.
      for (const match of stripComments(src).matchAll(/"(agents\.[a-zA-Z0-9._]+)"/g)) {
        used.add(match[1] as string);
      }
    }
    expect(used.size).toBeGreaterThan(15);
    const missing: string[] = [];
    for (const key of used) {
      if (lookup(enDoc, key) === undefined) missing.push(`en:${key}`);
      if (lookup(ptDoc, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `catalog gaps: ${missing.join(", ")}`).toEqual([]);
  });

  it("the Portuguese actually translates — no copied multi-word English", () => {
    const copied = keySet(section(enDoc, "agents"), "agents").filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // "daemon", "agent" and "slug" legitimately ride into Portuguese; a whole
      // sentence never rides unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal key and refuses a synthetic gap", () => {
    const synthetic = 'const a = t("agents.brand.new.key");';
    expect(/\bt\(\s*"agents\./.test(synthetic)).toBe(true);
    expect(lookup(enDoc, "agents.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "agents.brand.new.key")).toBeUndefined();
  });
});
