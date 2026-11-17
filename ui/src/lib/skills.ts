// The Agent Skills catalog, in the app (S65).
//
// # What this file is
//
// Types and calls for the daemon's `skills.list · get · validate ·
// install · remove` — the catalog of Agent Skills the app discovers from
// two roots: the operator's own `~/.agents/skills` (READ-ONLY here, always)
// and Aeon's managed folder where installs land. This is the THIRD meaning
// of "skill" in this project — the SKILL.md format, not the A2A card's
// skills nor the queue's sizing skills (internal/skills' package comment
// keeps the three apart; this file mirrors the ruling so the wire and the
// screen agree).
//
// # What the page shows, and what it never does
//
// The slice's promise at the UI level: **open the app and see what the
// agents will have available, validated, without anything being injected
// into a chat.** The validation shown IS validation done — the same
// verdicts the daemon's doors carry. Nothing here executes a skill, and
// nothing here writes to the user root: `install` targets the managed
// folder only, and `remove` refuses a name that lives only in the user's
// own folder. The install form takes a GitHub locator or a local folder
// and sends it to the DAEMON, which fetches, validates before placement,
// and lands it — the browser never touches the disk or the network.

import { useCallback, useEffect, useMemo, useState } from "react";
import { onEvent, rpc, toRpcError, type RpcError } from "./bridge";
import type { SubscribeResult } from "./protocol";
import type { ChatAttachment } from "../components/agent-chat/ComposerAttachments";

/** The validator's three-state verdict — a fourth status is a Go change,
 * never a string here (P31: invalid is visible, never silently dropped). */
export const SKILL_STATUSES = ["valid", "warn", "invalid"] as const;
export type SkillStatus = (typeof SKILL_STATUSES)[number];

/** The two roots — user (the operator's folder, never writable here) and
 * managed (Aeon's install target). */
export const SKILL_SOURCES = ["user", "managed"] as const;
export type SkillSource = (typeof SKILL_SOURCES)[number];

export interface SkillIssue {
  field: string;
  detail: string;
  severity: "error" | "warning";
}

export interface SkillFiles {
  scripts: number;
  references: number;
  assets: number;
  other: number;
}

/** One discovered skill — internal/skills' `Skill` marshalled verbatim.
 * `name` is the FOLDER name (the identity the spec binds); a differing
 * frontmatter name is an issue, not a rename. */
export interface SkillView {
  name: string;
  frontmatterName?: string;
  description?: string;
  license?: string;
  compatibility?: string;
  allowedTools?: string;
  metadata?: Record<string, string>;
  /** Extension fields from other agents (Claude Code's set and friends):
   * kept, displayed, never invalidated (compatibility means exactly that). */
  unknown?: Record<string, string>;
  status: SkillStatus;
  issues?: SkillIssue[];
  source: SkillSource;
  root: string;
  path: string;
  files: SkillFiles;
  bodyBytes: number;
  bodyLines: number;
  totalBytes: number;
  shadowed?: boolean;
  shadowedBy?: string;
  scannedAt: string;
}

export interface SkillSummary {
  valid: number;
  warn: number;
  invalid: number;
  shadowed: number;
}

export interface SkillListResult {
  skills: SkillView[];
  userRoot: string;
  managedRoot: string;
  scannedAt: string;
  summary: SkillSummary;
  /**
   * S66: the watcher's honest state, carried by EVERY scan answer. True
   * with no watchError means dropped-in folders are noticed without a
   * restart; false means the answer came from a scan the filesystem
   * cannot refresh, and watchError names why (three-state honesty —
   * absence of the field on an old daemon means "this daemon predates
   * watching", which renders as the same spoken sentence).
   */
  watching?: boolean;
  watchError?: string;
}

export interface SkillGetResult {
  skill: SkillView;
  body: string;
  /** Honest at the daemon's cap: absent means the body arrived complete. */
  truncated?: boolean;
}

export interface SkillValidateResult {
  skills: SkillView[];
  summary: SkillSummary;
  scannedAt: string;
}

export interface SkillInstallResult {
  skill: SkillView;
  replaced?: boolean;
}

export interface SkillRemoveResult {
  name: string;
  path: string;
  revealed?: boolean;
}

export interface SkillFilter {
  status?: string;
  source?: string;
  query?: string;
  limit?: number;
}

/** `skills.list` — the page-mount question's door. */
export function listSkills(filter: SkillFilter = {}): Promise<SkillListResult> {
  const params: Record<string, unknown> = {};
  if (filter.status) params.status = filter.status;
  if (filter.source) params.source = filter.source;
  if (filter.query) params.query = filter.query;
  if (filter.limit) params.limit = filter.limit;
  return rpc<SkillListResult>("skills.list", params);
}

/** One skill in full — the detail pane's door. */
export function getSkill(name: string): Promise<SkillGetResult> {
  return rpc<SkillGetResult>("skills.get", { name });
}

/** Re-read the disk and re-judge. This IS the freshness door (there is no
 * separate refresh): the operator edited SKILL.md with an editor open, the
 * click re-walks, ScannedAt says when. */
export function validateSkills(name?: string): Promise<SkillValidateResult> {
  const params: Record<string, unknown> = {};
  if (name) params.name = name;
  return rpc<SkillValidateResult>("skills.validate", params);
}

/** Install into the MANAGED root from a local folder or a GitHub locator.
 * The daemon stages, validates and places; nothing in a skill is ever
 * executed — scripts are listed, never run. */
export function installSkill(request: {
  source: string;
  ref?: string;
  subpath?: string;
  force?: boolean;
}): Promise<SkillInstallResult> {
  const params: Record<string, unknown> = { source: request.source };
  if (request.ref) params.ref = request.ref;
  if (request.subpath) params.subpath = request.subpath;
  if (request.force) params.force = true;
  return rpc<SkillInstallResult>("skills.install", params);
}

/** Remove a managed skill. A name that lives only in the user root
 * refuses with the sentence saying so — never a silent no-op. */
export function removeSkill(name: string): Promise<SkillRemoveResult> {
  return rpc<SkillRemoveResult>("skills.remove", { name });
}

/**
 * The catalog for one screen: read whenever `connected` holds and on every
 * explicit reload — and, since S66, the moment the daemon's watcher says
 * the world moved. A `skills.changed` event (folders added, removed, or
 * re-shaped under a watched root) triggers one re-read: no polling, the
 * filesystem told the daemon and the daemon tells this screen. `revalidate`
 * remains the manual freshness door for a daemon with no watcher at all.
 */
export function useSkills(connected: boolean, filter: SkillFilter = {}) {
  const [result, setResult] = useState<SkillListResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  const statusKey = filter.status ?? "";
  const sourceKey = filter.source ?? "";
  const queryKey = filter.query ?? "";

  const read = useCallback(async () => {
    try {
      const res = await listSkills({
        status: statusKey || undefined,
        source: sourceKey || undefined,
        query: queryKey || undefined,
      });
      // Go marshals a nil slice as null; a page must not crash on an
      // empty catalog (the asyncworkers lesson, carried).
      setResult({ ...res, skills: res.skills ?? [] });
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [statusKey, sourceKey, queryKey]);

  useEffect(() => {
    if (!connected) return;
    void read();
  }, [connected, read]);

  // S66: the watcher speaks, so this screen listens. One subscription to
  // `skills.changed` and every announced diff re-reads the catalog — the
  // same door a chat page uses to watch its loop, aimed at the catalog.
  // A failure here (an old daemon with no subscribe) leaves the manual
  // reload working, so it is silent by design: absence of events is
  // absence of news, never an error state.
  useEffect(() => {
    if (!connected) return;
    let disposed = false;
    let unlisten: (() => void) | undefined;
    let subId = "";
    void (async () => {
      try {
        const stop = await onEvent((payload) => {
          const ev = payload as { kind?: unknown; subscriptionId?: unknown };
          if (ev.kind !== "skills.changed" || ev.subscriptionId !== subId) return;
          void read();
        });
        if (disposed) {
          stop();
          return;
        }
        unlisten = stop;
        const sub = await rpc<SubscribeResult>("subscribe", { filter: { kinds: ["skills.changed"] }, after: 0 });
        subId = sub.subscriptionId;
      } catch {
        // No events is not an error; the buttons still answer.
      }
    })();
    return () => {
      disposed = true;
      unlisten?.();
    };
  }, [connected, read]);

  const revalidate = useCallback(async () => {
    setBusy(true);
    try {
      await validateSkills();
      await read();
      setError(null);
    } catch (e) {
      setError(toRpcError(e));
    } finally {
      setBusy(false);
    }
  }, [read]);

  return { result, error, busy, reload: read, revalidate } as const;
}

/* ------------------------------------------------------------------ *
 * S67 — the chat picker: `/` and the plus menu list the catalog.
 *
 * WHY the logic lives HERE and not in each host: the picker rows, the
 * chips, and the instruction sentence are ONE truth. Three hosts (the bench's
 * two views and the loop workspace) spread what this returns; none of
 * them owns a sentence of its own, so no host can drift from the others
 * and the tests exercise the actual behavior, not a per-host copy.
 *
 * WHAT picking means (S67's ruling, S68's body): the catalog ALREADY
 * rides every step as an offer (S66 — names, descriptions, paths, framed
 * as DATA). Choosing a skill SHARPENS that offer into an instruction —
 * but the instruction is the DAEMON's words, rendered from the riding
 * NAMES at its door into the turn's system half. The send carries only
 * the operator's pure words plus a `skills` field of names; the chips
 * above the composer state exactly what will ride, they clear on send
 * with the text, and the receipt under the posted bubble names what
 * actually rode. Nothing is injected silently and nothing decorates the
 * person's message.
 *
 * The panel/chip shapes are structural copies of `ChatPanel`/
 * `ChatPanelItem`/`ComposerChip` (this lib must not import the
 * component — the dependency arrow points the other way).
 * ------------------------------------------------------------------ */

/** Structural twin of `ComposerChip` (components/agent-chat). */
export interface SkillPickerChip {
  id: string;
  label: string;
  title?: string;
  onRemove?: () => void;
}

/** Structural twin of `ChatPanelItem` — disabled rows render, never select. */
export interface SkillPickerRow {
  id: string;
  label: string;
  description?: string;
  disabled?: boolean;
  onSelect?: () => void;
}

/** Structural twin of `ChatPanel`. */
export interface SkillPickerPanel {
  id: string;
  title: string;
  description?: string;
  emptyText: string;
  items: readonly SkillPickerRow[];
}

/** The three states the picker answer may take — an unread catalog is
 * never shown as an empty one (P31). */
function pickerEmptyText(skills: readonly SkillView[], error: RpcError | null, loaded: boolean, query: string): string {
  if (!loaded && error) return `The catalog could not be read: ${error.message}`;
  if (skills.length === 0) return "No skills installed — drop a folder into either skills root, or install one from Test bench → Agent Skills.";
  return query ? `No skill matches “${query}”.` : "No skills to offer.";
}

/** The sentence the daemon renders for one chosen skill (S68 B11: ONE
 * template, both sides) — the chip previews it as its tooltip, and the
 * daemon speaks it into the turn's system half. The UI never puts it on
 * the wire; the NAMES do. */
export function skillInstructionLine(name: string, path: string): string {
  return `use the ${name} skill — read ${path}/SKILL.md before answering.`;
}

/** Valid and warn ride the picker (a warning is a thin description, not a
 * broken skill — the same line S66's daemon offer draws); invalid and
 * shadowed are LISTED but disabled, because visibility without a false
 * promise is the catalog's whole honesty story. */
export function skillIsPickable(skill: SkillView): boolean {
  return (skill.status === "valid" || skill.status === "warn") && !skill.shadowed;
}

/**
 * The catalog wired to a composer: rows for the `/` picker (and the plus
 * menu's skills action), the chosen set as chips, and the instruction-sentence preview.
 * `connected` gates the read exactly as `useSkills` does.
 */
export function useSkillPicker(connected: boolean) {
  const { result, error } = useSkills(connected);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string[]>([]);

  const skills = useMemo(() => result?.skills ?? [], [result]);
  const loaded = result !== null;

  const openFor = useCallback((nextQuery: string) => { setQuery(nextQuery); setOpen(true); }, []);
  const close = useCallback(() => setOpen(false), []);
  const clear = useCallback(() => setSelected([]), []);
  const toggle = useCallback((name: string) => setSelected((names) =>
    names.includes(name) ? names.filter((n) => n !== name) : [...names, name]), []);

  const rows = useMemo<SkillPickerRow[]>(() => {
    const q = query.trim().toLowerCase();
    return skills
      .filter((s) => !q || `${s.name} ${s.description ?? ""}`.toLowerCase().includes(q))
      .map((s) => {
        const pickable = skillIsPickable(s);
        const note = s.shadowed ? "not selectable — shadowed; the other root’s copy is the one that would run"
          : s.status === "invalid" ? "not selectable — the validator refused it" : undefined;
        return {
          id: s.name,
          label: s.name,
          description: note ?? (s.status === "warn" && s.description ? `${s.description} (warnings)` : s.description),
          disabled: !pickable,
          onSelect: pickable ? () => toggle(s.name) : undefined,
        };
      });
  }, [skills, query, toggle]);

  const chips = useMemo<SkillPickerChip[]>(() => selected
    .map((name) => skills.find((s) => s.name === name))
    .filter((s): s is SkillView => s !== undefined && skillIsPickable(s))
    .map((s) => ({ id: s.name, label: s.name, title: skillInstructionLine(s.name, s.path), onRemove: () => toggle(s.name) })),
  [selected, skills, toggle]);

  const panel = useMemo<SkillPickerPanel | null>(() => open ? {
    id: "skills",
    title: "Skills",
    description: "Chosen skills ride the send — the model is told to read each chosen SKILL.md.",
    emptyText: pickerEmptyText(skills, error, loaded, query),
    items: rows,
  } : null, [open, rows, skills, error, loaded, query]);

  /** composer `onSpecialKeyChange`: only `/` is ours; anything else (or
   * null) closes us — a second trigger replacing the token means the
   * operator moved on. */
  const onSpecialKeyChange = useCallback((request: { key: string; query: string } | null) => {
    if (request && request.key === "/") openFor(request.query);
    else close();
  }, [openFor, close]);

  /** composer `onAddAction`: the plus menu's skills row opens the same
   * picker with an empty filter. Other actions are not ours. */
  const onAddAction = useCallback((action: string) => {
    if (action === "skills") openFor("");
  }, [openFor]);

  /** The names that would ride THIS send, in selection order, pickable
   * only (a skill that turned invalid or shadowed since it was picked
   * rides nothing — and the daemon's door would skip it with a spoken
   * report anyway; the chip row already hides it). */
  const chosenNames = useCallback((): string[] =>
    selected.filter((name) => {
      const s = skills.find((k) => k.name === name);
      return s !== undefined && skillIsPickable(s);
    }), [selected, skills]);

  /** Host send-wrappers, so the ATTACHMENT rule lives HERE and nowhere
   * else (a host that attached by hand is a host that drifts). S68 (B3/B7)
   * moved what choosing MEANS from the text to the wire: the operator's
   * words are sent PURE — what they typed is what they said, byte for byte
   * — and the chosen skills ride as NAMES in the request's `skills` field.
   * The daemon validates the names at its door and renders the canonical
   * instruction lines into the turn's SYSTEM half; the chat window keeps
   * only the words, with the receipt chips as the honest trace.
   * Await the send: chips clear only when it resolved — a failed send
   * keeps its chips, because losing what you typed to a socket error is
   * not acceptable. */
  const steerWith = useCallback(<R extends { text: string }>(send: (request: R) => void | Promise<void>) =>
    async (request: R) => {
      const names = chosenNames();
      await send(names.length > 0 ? ({ ...request, skills: names } as R) : request);
      if (names.length > 0) clear();
    }, [chosenNames, clear]);

  const messageWith = useCallback((send: (text: string, route?: string, skills?: readonly string[], files?: readonly ChatAttachment[]) => void | Promise<void>) =>
    async (text: string, route?: string, files?: readonly ChatAttachment[]) => {
      const names = chosenNames();
      // S69: the composer's draft FILES forward through here too — a
      // wrapper that drops them is a wrapper that loses the person's
      // attachment. The PUT rule itself lives at the send's owner
      // (useChatActions), exactly as the NAME rule lives here.
      await send(text, route, names.length > 0 ? names : undefined, files);
      if (names.length > 0) clear();
    }, [chosenNames, clear]);

  return { open, openFor, close, clear, toggle, rows, chips, panel, chosenNames, steerWith, messageWith, selected, onSpecialKeyChange, onAddAction } as const;
}
