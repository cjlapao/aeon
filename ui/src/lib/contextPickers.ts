// Context-aware composer pickers (S77 host integration).
//
// # What this file is
//
// The panel LOGIC for the triggers a host composer delegates: `@` targets
// the SELECTED component's files, `//` the loops that live there, `$` the
// agents, and `commands` stays honestly empty until a daemon door offers
// any. The rule the operator asked for, made structural: **a panel with no
// context says so and names the missing context** — typing `@` before a
// component is chosen shows the gate sentence, not a lie and not silence.
// Once a component IS chosen, `@` is scoped to it: the changed files from
// `workspace.git.status` plus a debounced `search.query` over the code
// corpus, both capped, and a failed read renders its own sentence (P31:
// an unreadable workspace is never an empty one).
//
// # Why HERE and not in each host
//
// Same ruling as S67 drew for skills: rows, sentences and the token-insert
// arithmetic are ONE truth; hosts spread what this returns and none owns a
// sentence of its own. The panel/chip/request shapes are STRUCTURAL twins
// of the composer's (the dependency arrow points lib ← component, never
// the other way), and the insert itself stays the host's act: this lib
// computes the replacement string, the host owns its draft.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { rpc, toRpcError } from "./bridge";

/** Structural twin of `ChatSpecialKeyRequest` (components/agent-chat). */
export interface ContextSpecialRequest {
  key: string;
  query: string;
  start: number;
  end: number;
}

/** Structural twin of `ChatPanelItem` — disabled rows render, never select. */
export interface ContextPickerRow {
  id: string;
  label: string;
  description?: string;
  disabled?: boolean;
  onSelect?: () => void;
}

/** Structural twin of `ChatPanel`. */
export interface ContextPickerPanel {
  id: string;
  title: string;
  description?: string;
  emptyText: string;
  items: readonly ContextPickerRow[];
}

/** The loop rows this picker can offer — the fields of a `loop.list` row
 * it reads, structurally (the host already holds them; no second fetch). */
export interface ContextLoopRow {
  id: string;
  slug: string;
  title?: string;
  status: string;
  componentRoot: string;
}

/** The panel copy. English defaults keep the lib self-sufficient (the
 * skills precedent); a host passes translated slots and every sentence is
 * then the host's dictionary's, not a second copy of the truth here. */
export interface ContextPickerCopy {
  filesTitle?: string;
  filesGate?: string;
  filesEmpty?: string;
  filesSearching?: string;
  loopsTitle?: string;
  loopsEmpty?: string;
  agentsTitle?: string;
  agentsEmpty?: string;
  commandsTitle?: string;
  commandsEmpty?: string;
}

const DEFAULTS: Required<ContextPickerCopy> = {
  filesTitle: "Files in this component",
  filesGate: "Choose a component first — @ then targets its files.",
  filesEmpty: "No changed files, and no search yet — type to search this component’s code.",
  filesSearching: "Searching this component’s code…",
  loopsTitle: "Loops",
  loopsEmpty: "No loops to open yet.",
  agentsTitle: "Agents",
  agentsEmpty: "No agents registered.",
  commandsTitle: "Commands",
  commandsEmpty: "Nothing answers commands yet — command pickers arrive with their daemon door.",
};

const SEARCH_DEBOUNCE_MS = 250;
const MAX_ROWS = 50;

/** Splice the trigger token out and put `replacement` where it stood.
 * Pure — the host applies it to ITS draft, so no picker can corrupt text
 * it does not own. `replacement` already carries the trigger when the
 * insertion should keep it (`@src/a.ts` stays an `@` mention on the wire
 * of words the model reads). */
export function applyTokenInsert(text: string, request: ContextSpecialRequest, replacement: string): string {
  return `${text.slice(0, request.start)}${replacement}${text.slice(request.end)}`;
}

/** Compose the hosts' picker handlers into the composer's ONE
 * `onSpecialKeyChange`: every picker sees every request, each opens for
 * its own keys and closes for the rest — so typing `@` closes the skills
 * panel and typing `/` closes the files panel, with no cross-knowledge. */
export function mergeSpecialKeyHandlers(handlers: Array<(request: ContextSpecialRequest | null) => void>): (request: ContextSpecialRequest | null) => void {
  return (request) => { for (const handler of handlers) handler(request); };
}

/** Compose the plus-menu actions the same way. */
export function mergeAddActionHandlers(handlers: Array<(action: string) => void>): (action: string) => void {
  return (action) => { for (const handler of handlers) handler(action); };
}

/** The workspace answers one `@` token may offer. Changed files ride in
 * from git status; a typed query adds the code-corpus search, and a
 * failed read is its own row of prose, never an empty list. */
interface FilesState {
  changed: string[];
  hits: Array<{ source: string; lineStart: number; lineEnd: number }>;
  keywordOnly: boolean;
  loading: boolean;
  error: string;
}

const closedFiles: FilesState = { changed: [], hits: [], keywordOnly: false, loading: false, error: "" };

export function useContextPickers({ connected, componentRoot, loops, onOpenLoop, onInsert, copy = {} }: {
  connected: boolean;
  /** The SELECTED component — "" until the host has one; every `@` answer is scoped to it. */
  componentRoot: string;
  loops: readonly ContextLoopRow[];
  /** Picking a `//` row navigates: the loop is the context, not a token. */
  onOpenLoop: (loopId: string) => void;
  /** Inserting a `@`/`$` pick: the host splices into ITS draft. */
  onInsert: (request: ContextSpecialRequest, replacement: string) => void;
  copy?: ContextPickerCopy;
}) {
  const words = { ...DEFAULTS, ...copy };
  const [openKey, setOpenKey] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const active = useRef<ContextSpecialRequest | null>(null);
  const [files, setFiles] = useState<FilesState>(closedFiles);
  const [agents, setAgents] = useState<Array<{ name: string; provenance: string; state?: string }>>([]);
  const agentsLoaded = useRef(false);
  const agentsError = useRef("");
  // Stale-guard: a slow search must not overwrite a newer one, and a
  // closed panel drops every in-flight answer on the floor.
  const generation = useRef(0);

  const close = useCallback(() => {
    generation.current += 1;
    active.current = null;
    setOpenKey(null);
  }, []);

  const openFor = useCallback((request: ContextSpecialRequest | null, forcedKey?: string) => {
    const key = forcedKey ?? request?.key ?? null;
    if (!key) { close(); return; }
    active.current = request;
    // A forced open from the plus menu carries no live token: a `$`/files
    // row then inserts at the caret-less end is impossible — so a forced
    // open still lists, and rows only INSERT when a token is live (they
    // always navigate or toggle otherwise).
    setQuery(request?.query ?? "");
    generation.current += 1;
    setOpenKey(key);
  }, [close]);

  // --- `@` — the selected component's files --------------------------------
  useEffect(() => {
    if (openKey !== "@" || !componentRoot) { setFiles((current) => (current === closedFiles ? current : { ...closedFiles })); return; }
    const generationAt = generation.current;
    let cancelled = false;
    setFiles({ ...closedFiles, loading: true });
    void rpc<{ changed?: Array<{ path: string } > }>("workspace.git.status", { componentRoot }).then((result) => {
      if (cancelled || generationAt !== generation.current) return;
      setFiles((f) => ({ ...f, changed: (result.changed ?? []).map((entry) => entry.path), loading: false }));
    }).catch((cause: unknown) => {
      if (cancelled || generationAt !== generation.current) return;
      setFiles((f) => ({ ...f, loading: false, error: toRpcError(cause).message }));
    });
    return () => { cancelled = true; };
  }, [openKey, componentRoot]);

  useEffect(() => {
    if (openKey !== "@" || !componentRoot || query.trim().length < 2) return;
    const generationAt = generation.current;
    let cancelled = false;
    const timer = setTimeout(() => {
      void rpc<{ hits?: Array<{ source: string; lineStart: number; lineEnd: number }>; keywordOnly?: boolean }>("search.query", {
        componentRoot, corpus: "code", query: query.trim(),
      }).then((result) => {
        if (cancelled || generationAt !== generation.current) return;
        setFiles((f) => ({ ...f, hits: result.hits ?? [], keywordOnly: Boolean(result.keywordOnly) }));
      }).catch((cause: unknown) => {
        if (cancelled || generationAt !== generation.current) return;
        setFiles((f) => ({ ...f, error: toRpcError(cause).message }));
      });
    }, SEARCH_DEBOUNCE_MS);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [openKey, componentRoot, query]);

  // --- `$` — the registered agents, read once, lazily -----------------------
  const loadAgents = useCallback(() => {
    if (!connected || agentsLoaded.current) return;
    void rpc<{ agents?: Array<{ name: string; provenance: string }> }>("agent.list", {}).then((result) => {
      agentsLoaded.current = true;
      setAgents(result.agents ?? []);
    }).catch((cause: unknown) => { agentsError.current = toRpcError(cause).message; agentsLoaded.current = true; setAgents([]); });
  }, [connected]);

  // --- rows, one memo per panel ---------------------------------------------
  const insertLive = useCallback((request: ContextSpecialRequest, replacement: string) => {
    onInsert(request, replacement);
    close();
  }, [onInsert, close]);

  const fileRows = useMemo<ContextPickerRow[]>(() => {
    const q = query.trim().toLowerCase();
    const rows: ContextPickerRow[] = [];
    const seen = new Set<string>();
    const request = active.current;
    const add = (path: string, description?: string) => {
      if (seen.has(path) || rows.length >= MAX_ROWS) return;
      seen.add(path);
      rows.push({
        id: `file:${path}`,
        label: path,
        description,
        onSelect: request ? () => insertLive(request, `${request.key}${path} `) : undefined,
      });
    };
    for (const hit of files.hits) add(hit.source, q ? `match at line ${hit.lineStart}` : `indexed match (line ${hit.lineStart})`);
    for (const path of files.changed) add(path, "changed in this workspace");
    if (q) return rows.filter((row) => row.label.toLowerCase().includes(q));
    return rows;
  }, [files, query, insertLive]);

  const loopRows = useMemo<ContextPickerRow[]>(() => {
    const q = query.trim().toLowerCase();
    const scoped = componentRoot ? loops.filter((loop) => loop.componentRoot === componentRoot) : loops;
    return scoped
      .filter((loop) => !q || `${loop.slug} ${loop.title ?? ""}`.toLowerCase().includes(q))
      .slice(0, MAX_ROWS)
      .map((loop) => ({
        id: `loop:${loop.id}`,
        label: loop.title || loop.slug,
        description: `${loop.status}${componentRoot ? "" : ` · ${loop.componentRoot}`}`,
        onSelect: () => { close(); onOpenLoop(loop.id); },
      }));
  }, [loops, componentRoot, query, onOpenLoop, close]);

  const agentRows = useMemo<ContextPickerRow[]>(() => {
    const q = query.trim().toLowerCase();
    const request = active.current;
    return agents
      .filter((agent) => !q || agent.name.toLowerCase().includes(q))
      .slice(0, MAX_ROWS)
      .map((agent) => ({
        id: `agent:${agent.name}`,
        label: agent.name,
        description: agent.provenance === "builtin" ? undefined : `${agent.provenance}${agent.state === "stopped" ? " · stopped" : ""}`,
        onSelect: request ? () => insertLive(request, `${request.key}${agent.name} `) : undefined,
      }));
  }, [agents, query, insertLive]);

  // --- the one panel this hook owns ------------------------------------------
  const panel = useMemo<ContextPickerPanel | null>(() => {
    if (!openKey) return null;
    if (openKey === "@") {
      if (!componentRoot) return { id: "files", title: words.filesTitle, emptyText: words.filesGate, items: [] };
      return {
        id: "files",
        title: words.filesTitle,
        description: files.keywordOnly ? "keyword-only match (no embedding route is answering)" : undefined,
        emptyText: files.error ? `The workspace could not be read: ${files.error}`
          : files.loading && files.changed.length === 0 ? words.filesSearching
          : fileRows.length === 0 ? words.filesEmpty : "",
        items: fileRows,
      };
    }
    if (openKey === "//") return { id: "loops", title: words.loopsTitle, emptyText: loopRows.length === 0 ? words.loopsEmpty : "", items: loopRows };
    if (openKey === "$") return {
      id: "agents",
      title: words.agentsTitle,
      emptyText: agentsError.current && agentsLoaded.current && agents.length === 0 ? `The agent list could not be read: ${agentsError.current}`
        : agentRows.length === 0 ? words.agentsEmpty : "",
      items: agentRows,
    };
    return { id: "commands", title: words.commandsTitle, emptyText: words.commandsEmpty, items: [] };
  }, [openKey, componentRoot, words, files, fileRows, loopRows, agents, agentRows]);

  /** composer `onSpecialKeyChange`: ours open, everything else closes. */
  const onSpecialKeyChange = useCallback((request: ContextSpecialRequest | null) => {
    if (!request) { close(); return; }
    if (request.key === "@" || request.key === "//" || request.key === "$") {
      if (request.key === "$") loadAgents();
      openFor(request);
      return;
    }
    if (request.key === "commands") { openFor(request); return; }
    close();
  }, [close, loadAgents, openFor]);

  /** composer `onAddAction`: the plus-menu rows open the same panels. A
   * forced open has no live token, so file/agent rows list without an
   * insert target (loops always navigate). */
  const onAddAction = useCallback((action: string) => {
    if (action === "loops") openFor(null, "//");
    else if (action === "agents") { loadAgents(); openFor(null, "$"); }
    else if (action === "commands") openFor(null, "commands");
  }, [loadAgents, openFor]);

  return { panel, close, openFor, onSpecialKeyChange, onAddAction, openKey } as const;
}
