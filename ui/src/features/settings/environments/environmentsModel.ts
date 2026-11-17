// environmentsModel.ts — S59 T11: the pure logic the Environments panel renders with.
//
// Every word this panel says about an environment — its backend, its
// capabilities, its modes, its honesty line, its refusals — arrives from the
// daemon and is rendered as sent. Nothing here holds a status string, a
// backend name, a deny word or an egress mode: those are the daemon's closed
// sets, served by `environment.vocabulary`, and the grep guard in
// `test/environmentsRpc.guard.test.ts` fails the build on a copy of one.
//
// What lives here is presentation arithmetic over daemon data: which
// capabilities a backend declares (a set difference over the vocabulary's own
// lists), which limits a row carries, what changed between two recorded
// specs, and the fields the generated contract does not yet describe because
// no conformance fixture carried them (`downgradeNote`, `lastError`) — read
// defensively, never invented.

import type { RpcMethods } from "../../../proto/contract";

// --- the shapes, named from the generated contract ---------------------------

export type Vocabulary = RpcMethods["environment.vocabulary"]["result"];
export type KnownLimit = Vocabulary["knownLimits"][number];
export type BackendEntry = Vocabulary["backends"][number];
export type EnvironmentRow = RpcMethods["environment.list"]["result"][number] & {
  /**
   * The archive door's flag, widened the way S57 T11 widened the gate row: an
   * archived row is only listed when the person asks for archived rows, and the
   * Go view carries the member `omitempty` (`EnvironmentView.ArchivedAt`), so
   * no LIST fixture holds it — while `environment.archive` answers with it and
   * the row's archive/restore controls depend on knowing which one to offer.
   */
  archivedAt?: string;
};
export type EnvironmentDetail = RpcMethods["environment.show"]["result"];
export type SpecView = EnvironmentRow["spec"];
export type BindingView = EnvironmentRow["bindings"][number];

/**
 * A row as the WRITE doors answer it. `environment.create · update · archive ·
 * restore` carry the row WITHOUT the bindings list (only the two reads do), so
 * a row shown straight from a write answer has none until the list is asked
 * again — the panel renders the answer it was given rather than holding on to
 * an older answer's bindings, and never fills the gap itself.
 */
export type DisplayRow = Omit<EnvironmentRow, "bindings"> & { bindings?: readonly BindingView[] };
export type ChangeRow = RpcMethods["environment.changes"]["result"][number];
export type ProvisionRow = RpcMethods["environment.provisions"]["result"][number];
export type EffectiveAnswer = RpcMethods["environment.effective"]["result"];
export type SecretRefRow = RpcMethods["secretref.list"]["result"][number];
export type SecretRefCheck = RpcMethods["secretref.check"]["result"];
export type BindAnswer = RpcMethods["environment.bind"]["result"];
export type ComponentEntry = RpcMethods["component.list"]["result"]["components"][number];
export type ComponentDetail = RpcMethods["component.show"]["result"];

/**
 * The two audit subjects the changes door takes as PARAM VALUES. They are not
 * vocabulary the panel displays (the panel renders `subjectKind` from the
 * answer, never from here), and they are not invented: they are the bytes
 * `internal/protocol/conformance/environment/changes-one-write.json` and the
 * secret-reference audit rows carry on the wire.
 */
export const SUBJECT_ENVIRONMENT = "environment";
export const SUBJECT_SECRET_REF = "secretref";

/**
 * The two bind scopes, likewise PARAM VALUES only — `environment.bind` takes
 * `scope` and the wire's own words for it are these (`conformance/environment/
 * bind-dry-run-statement.json` sends `component`; the unbind/read views answer
 * the same pair in `scopeKind`). Every scope word the panel SHOWS comes from an
 * answer, never from these two.
 */
export const SCOPE_COMPONENT = "component";
export const SCOPE_WORKSPACE = "workspace";

// --- capability arithmetic (all of it over the vocabulary's own lists) -------

/** The backends in the vocabulary's own order, weakest first (EV16's order). */
export function backends(vocab: Vocabulary | undefined): readonly BackendEntry[] {
  return vocab?.backends ?? [];
}

/** One backend's entry, matched on the name the daemon sent. */
export function backendOf(vocab: Vocabulary | undefined, name: string): BackendEntry | undefined {
  return backends(vocab).find((entry) => entry.name === name);
}

/**
 * Every capability name the platform can enforce: the union of what the
 * backends declare, in strength order, first-seen. This is the closed set the
 * chips are drawn from — the UI never types one of its names.
 */
export function capabilityUniverse(vocab: Vocabulary | undefined): string[] {
  const seen: string[] = [];
  for (const entry of backends(vocab)) {
    for (const capability of entry.capabilities ?? []) {
      if (!seen.includes(capability)) seen.push(capability);
    }
  }
  return seen;
}

/**
 * The row's capability chips: one chip per capability the platform can
 * enforce, marked enforced when the row's own backend declares it. An ✗ is
 * therefore a fact about the backend the daemon published, not a judgement
 * this file makes about a name it recognises.
 */
export function capabilityChips(
  vocab: Vocabulary | undefined,
  backendName: string,
): Array<{ capability: string; enforced: boolean }> {
  const declared = backendOf(vocab, backendName)?.capabilities ?? [];
  return capabilityUniverse(vocab).map((capability) => ({
    capability,
    enforced: declared.includes(capability),
  }));
}

/** The strongest strength the platform publishes — the ceiling the chips read against. */
export function maxStrength(vocab: Vocabulary | undefined): number {
  return backends(vocab).reduce((max, entry) => Math.max(max, entry.strength), 0);
}

/** Whether a backend declares fewer capabilities than the ceiling backend does. */
export function backendIsPartial(vocab: Vocabulary | undefined, backendName: string): boolean {
  const declared = backendOf(vocab, backendName)?.capabilities?.length ?? 0;
  const ceiling = backends(vocab).reduce((max, entry) => Math.max(max, entry.capabilities?.length ?? 0), 0);
  return ceiling > 0 && declared < ceiling;
}

/** The closed sets the form builds its selects from — the daemon's, in its order. */
export const defaultAllowlist = (vocab: Vocabulary | undefined): string[] => vocab?.defaultAllowlist ?? [];
export const egressModes = (vocab: Vocabulary | undefined): string[] => vocab?.egressModes ?? [];
export const homeModes = (vocab: Vocabulary | undefined): string[] => vocab?.homeModes ?? [];
export const pullPolicies = (vocab: Vocabulary | undefined): string[] => vocab?.pullPolicies ?? [];
export const sourceKinds = (vocab: Vocabulary | undefined): string[] => vocab?.sourceKinds ?? [];
export const honestyLine = (vocab: Vocabulary | undefined): string => vocab?.honesty ?? "";
export const redactionLimits = (vocab: Vocabulary | undefined): string => vocab?.redactionLimits ?? "";

/**
 * EV15: the limitations the daemon says are true RIGHT NOW, in its own words.
 * A limit whose `active` is false is not rendered at all — that is the whole
 * point of the daemon carrying the status: a fixed defect stops being
 * announced the day it is fixed instead of living on in a client's string.
 */
export function activeKnownLimits(vocab: Vocabulary | undefined): KnownLimit[] {
  return (vocab?.knownLimits ?? []).filter((limit) => limit.active === true);
}

// --- bindings, origins, the legacy floor ------------------------------------

/**
 * The workspace-scope binding is the one a row carries without a component id
 * (the fixture shape: a workspace binding has no `componentId`). Whether the
 * workspace floor is the LEGACY row is then a boolean read off that row — the
 * daemon's `legacy` field, never a backend name compared to a typed word.
 */
export function legacyWorkspaceRow(rows: readonly DisplayRow[]): DisplayRow | undefined {
  return rows.find((row) => row.legacy === true && (row.bindings ?? []).some((binding) => !binding.componentId));
}

/** Where a row is bound, as (scope word, component id) pairs — the daemon's words. */
export function bindingsOf(row: DisplayRow): readonly BindingView[] {
  return row.bindings ?? [];
}

/** A component-name lookup for the binding chips (from `component.list`). */
export function componentNames(components: readonly ComponentEntry[]): Map<string, string> {
  const names = new Map<string, string>();
  for (const component of components) names.set(component.id, component.name);
  return names;
}

/** The newest audit row per subject, so a row can show `<actor>` `<RelativeTime>`. */
export function latestChangeBySubject(changes: readonly ChangeRow[]): Map<string, ChangeRow> {
  const newest = new Map<string, ChangeRow>();
  for (const change of changes) {
    const held = newest.get(change.subjectId);
    if (!held || Date.parse(change.at) > Date.parse(held.at)) newest.set(change.subjectId, change);
  }
  return newest;
}

// --- the spec, read and rebuilt ---------------------------------------------

/**
 * An answer that is not an array is an EMPTY list, never a crash. The fake
 * daemon's neutral answer for a method no test configured is `{}` — truthy, and
 * the shape a real daemon cannot send for these doors — so every array read in
 * the section goes through here rather than reaching for `.map` on whatever
 * arrived.
 */
export function listOf<T>(incoming: readonly T[] | undefined): T[] {
  return Array.isArray(incoming) ? [...incoming] : [];
}

const asRecord = (incoming: unknown): Record<string, unknown> =>
  typeof incoming === "object" && incoming !== null ? (incoming as Record<string, unknown>) : {};

const asStringArray = (incoming: unknown): string[] =>
  Array.isArray(incoming) ? incoming.filter((item): item is string => typeof item === "string") : [];

/** The variable NAMES passthrough carries — names, never values. */
export function passthroughOf(spec: SpecView | undefined): string[] {
  return asStringArray(asRecord(asRecord(spec?.env).passthrough));
}

/** The references an environment injects, by name. */
export function secretsOf(spec: SpecView | undefined): string[] {
  return asStringArray(asRecord(spec?.env).secrets);
}

/** The `set` pairs: a name and the content the person chose for a NON-secret variable. */
export function setPairsOf(spec: SpecView | undefined): Array<{ name: string; content: string }> {
  const set = asRecord(asRecord(spec?.env).set);
  return Object.entries(set)
    .filter((entry): entry is [string, string] => typeof entry[1] === "string")
    .map(([name, content]) => ({ name, content }));
}

/** Every key under which a name is set, for display: the daemon's keys, sorted. */
export function limitsOf(spec: SpecView | undefined): Array<{ name: string; shown: string }> {
  const limits = asRecord(spec?.limits);
  return Object.entries(limits)
    .filter((entry) => entry[1] !== null && entry[1] !== undefined)
    .map(([name, shown]) => ({ name, shown: String(shown) }));
}

/** The egress posture as the daemon recorded it: the mode word plus its allow list. */
export function egressOf(spec: SpecView | undefined): { mode: string; allow: string[] } {
  const egress = asRecord(spec?.egress);
  return { mode: typeof egress.mode === "string" ? egress.mode : "", allow: asStringArray(egress.allow) };
}

export function homeOf(spec: SpecView | undefined): string {
  return typeof spec?.home === "string" ? spec.home : "";
}

/** The base image the row carries, if any — the two fields the daemon's shape has. */
export function imageOf(spec: SpecView | undefined): { ref: string; pull: string } | undefined {
  const image = asRecord(spec?.image);
  const ref = typeof image.ref === "string" ? image.ref : "";
  if (ref === "") return undefined;
  return { ref, pull: typeof image.pull === "string" ? image.pull : "" };
}

/** The per-loop services the row carries (EV14), as the daemon recorded them. */
export function servicesOf(
  spec: SpecView | undefined,
): Array<{ name: string; image: string; port: number | undefined }> {
  if (!Array.isArray(spec?.services)) return [];
  return spec.services.map((entry) => {
    const service = asRecord(entry);
    return {
      name: typeof service.name === "string" ? service.name : "",
      image: typeof service.image === "string" ? service.image : "",
      port: typeof service.port === "number" ? service.port : undefined,
    };
  });
}

// --- the draft the editor edits, and the spec string it writes --------------

export interface ServiceDraft {
  name: string;
  image: string;
  port: string;
}

/** What the person is editing, in field-shaped form — never a wire object. */
export interface SpecDraft {
  passthrough: string[];
  set: Array<{ name: string; content: string }>;
  secrets: string[];
  home: string;
  egressMode: string;
  egressAllow: string[];
  imageRef: string;
  imagePull: string;
  limits: Array<{ name: string; shown: string }>;
  services: ServiceDraft[];
}

export function emptyDraft(vocab: Vocabulary | undefined): SpecDraft {
  return {
    passthrough: [],
    set: [],
    secrets: [],
    home: homeModes(vocab)[0] ?? "",
    egressMode: egressModes(vocab)[0] ?? "",
    egressAllow: [],
    imageRef: "",
    imagePull: pullPolicies(vocab)[0] ?? "",
    limits: [],
    services: [],
  };
}

/** An edit starts from what the daemon ANSWERED, so a save never silently drops a field. */
export function draftFromSpec(spec: SpecView | undefined, vocab: Vocabulary | undefined): SpecDraft {
  const image = imageOf(spec);
  return {
    passthrough: passthroughOf(spec),
    set: setPairsOf(spec),
    secrets: secretsOf(spec),
    home: homeOf(spec) || (homeModes(vocab)[0] ?? ""),
    egressMode: egressOf(spec).mode || (egressModes(vocab)[0] ?? ""),
    egressAllow: egressOf(spec).allow,
    imageRef: image?.ref ?? "",
    imagePull: image?.pull ?? (pullPolicies(vocab)[0] ?? ""),
    limits: limitsOf(spec),
    services: servicesOf(spec).map((service) => ({
      name: service.name,
      image: service.image,
      port: service.port === undefined ? "" : String(service.port),
    })),
  };
}

/**
 * The `spec` the write doors take is a JSON string (the daemon canonicalises
 * it and refuses what it will not store). Built field by field — never a
 * spread of anything the person typed into a wire object, and never carrying
 * a value-shaped key for a secret reference.
 */
export function specJson(draft: SpecDraft): string {
  const set: Record<string, string> = {};
  for (const pair of draft.set) {
    if (pair.name !== "") set[pair.name] = pair.content;
  }
  const limits: Record<string, unknown> = {};
  for (const limit of draft.limits) {
    if (limit.name === "") continue;
    const asNumber = Number(limit.shown);
    limits[limit.name] = limit.shown !== "" && Number.isFinite(asNumber) ? asNumber : limit.shown;
  }
  const services = draft.services
    .filter((service) => service.name !== "" || service.image !== "")
    .map((service) => ({
      name: service.name,
      image: service.image,
      port: service.port === "" ? 0 : Number(service.port),
      env: {},
      secrets: [],
    }));
  const image = draft.imageRef === "" ? null : { ref: draft.imageRef, pull: draft.imagePull };
  return JSON.stringify({
    env: { passthrough: draft.passthrough, set, secrets: draft.secrets },
    home: draft.home,
    limits,
    egress: { mode: draft.egressMode, allow: draft.egressAllow },
    image,
    services,
    preview: null,
  });
}

// --- the audit's diff: what one write did to the recorded spec --------------

/** Every name a spec makes visible, each in the one form the audit shows it in. */
function visibleNames(spec: SpecView | undefined): Set<string> {
  const names = new Set<string>();
  for (const name of passthroughOf(spec)) names.add(name);
  for (const pair of setPairsOf(spec)) names.add(pair.name);
  for (const name of secretsOf(spec)) names.add(`secret:${name}`);
  return names;
}

/**
 * The difference between two recorded specs — the audit rows carry
 * `specAfter`, so a revision's diff is computed from what the daemon recorded,
 * never from what the draft said. Names only, which is all a spec ever shows.
 */
export interface SpecDiff {
  added: string[];
  removed: string[];
  changed: string[];
}

export function specDiff(before: SpecView | undefined, after: SpecView | undefined): SpecDiff {
  const beforeNames = visibleNames(before);
  const afterNames = visibleNames(after);
  const added = [...afterNames].filter((name) => !beforeNames.has(name));
  const removed = [...beforeNames].filter((name) => !afterNames.has(name));
  const beforeSet = new Map(setPairsOf(before).map((pair) => [pair.name, pair.content]));
  const changed = setPairsOf(after)
    .filter((pair) => beforeSet.has(pair.name) && beforeSet.get(pair.name) !== pair.content)
    .map((pair) => pair.name);
  return { added, removed, changed };
}

// --- the fields the generated contract does not describe yet ----------------

/**
 * `environment.effective`'s `downgradeNote` (the legacy / EV6 sentence) and
 * `environment.provisions`' `lastError` exist on the wire (`omitempty` on
 * both, per `internal/protocol/environment.go`) and in no conformance
 * fixture, so the generated contract has no field for them. They are read
 * here defensively rather than cast at the call site: an answer that carries
 * them shows them verbatim, an answer that does not shows nothing.
 */
export function downgradeNoteOf(answer: unknown): string {
  const note = asRecord(answer).downgradeNote;
  return typeof note === "string" ? note : "";
}

export function lastErrorOf(row: unknown): string {
  const error = asRecord(row).lastError;
  return typeof error === "string" ? error : "";
}

/** `removedAt` likewise: `omitempty` on `ProvisionView.RemovedAt`. */
export function removedAtOf(row: unknown): string {
  const removed = asRecord(row).removedAt;
  return typeof removed === "string" ? removed : "";
}

/** The `changes` door's unfiltered read: an empty subject id is the wire's unset. */
export function changesParamsFor(subjectKind: string, limit: number, subjectId = "") {
  return { subjectKind, subjectId, action: "", limit };
}

/**
 * Whether a check answer carries a refusal at all. The refusal's KIND and
 * SENTENCE are rendered from the answer verbatim, and the two booleans are
 * labelled by the panel's own i18n words — this helper only asks whether the
 * daemon said anything to say, so nothing here holds a state word.
 */
export function hasRefusal(check: SecretRefCheck | undefined): boolean {
  if (!check) return false;
  return check.refusalKind !== "" || check.refusalSentence !== "";
}
