// daemonSettings — the pure, testable half of the Daemon settings panel (S44 T8).
//
// No React, no RPC, no English chrome: every function here is unit-testable
// without a DOM, and the panel (DaemonSettingsPanel.tsx) is descriptor-driven —
// it renders whatever `settings.list` returns and names no key, so a newly
// declared setting appears with zero UI change (CS10). The panel localises the
// tokens and structured facts these functions return; it never reaches back for
// the meaning of a layer or the shape of a control.

import type { RpcMethods } from "../../proto/contract";

/**
 * The one shape every row is held as — the richest of the settings.* views.
 *
 * `settings.list` carries `enumValues`; the `settings.set` / `settings.unset` /
 * `settings.get` result settings are all assignable to it because the extra
 * fields (`enumValues`, `min`, `max`) are optional on this shape. A row is
 * replaced by a response's `setting` directly, with no cast (see the panel).
 */
export type SettingView = RpcMethods["settings.list"]["result"]["settings"][number];

/** Which control a declared setting gets. */
export type ControlKind = "switch" | "select" | "number" | "text";

/**
 * The control a setting's `type` maps to.
 *
 * `bool → switch`, `enum → select`, `int → number`, and everything else
 * (including a `string`, and any type a later slice declares) → `text`: a raw
 * text control is the safe default and never claims a value is valid (CS3).
 */
export function controlKindFor(view: SettingView): ControlKind {
  switch (view.type) {
    case "bool":
      return "switch";
    case "enum":
      return "select";
    case "int":
      return "number";
    default:
      return "text";
  }
}

/**
 * Group by `area`, preserving the daemon's order — areas by first appearance,
 * keys within an area in input order.
 *
 * The daemon already sorts; this helper only must not re-sort and disagree with
 * the CLI (which renders the same `settings.list` order).
 */
export function groupByArea(
  settings: SettingView[],
): Array<{ area: string; items: SettingView[] }> {
  const order: string[] = [];
  const groups = new Map<string, SettingView[]>();
  for (const view of settings) {
    if (!groups.has(view.area)) {
      order.push(view.area);
      groups.set(view.area, []);
    }
    groups.get(view.area)!.push(view);
  }
  return order.map((area) => ({ area, items: groups.get(area)! }));
}

/** The layer a source maps to. The panel localises the token; this returns it. */
export type SourceToken = "default" | "saved" | "environment";

/**
 * Which layer won, as a stable token the panel turns into a badge.
 *
 * `default → default`, `database → saved`, `environment → environment`. A source
 * the daemon never reports falls back to `default` — the base layer — so an
 * unexpected value can never read as a saved or environment lock.
 */
export function sourceBadge(view: SettingView): SourceToken {
  switch (view.source) {
    case "default":
      return "default";
    case "database":
      return "saved";
    case "environment":
      return "environment";
    default:
      return "default";
  }
}

/**
 * True only when the environment layer is in force — the control then has no
 * effect, so it is offered read-only and the row names the variable (CS4).
 */
export function isLocked(view: SettingView): boolean {
  return view.source === "environment";
}

/**
 * The layer(s) in force, as structured facts the panel localises.
 *
 * `describeLayers` decides *which* layers are in force; it never produces
 * English. The panel composes the localised lines from these fields — the
 * "default" note, the "saved, by …" line, and the environment note with its
 * second line for a shadowed saved value.
 */
export interface LayerDescription {
  source: SourceToken;
  /** The effective value — the layer that won. */
  value: string;
  /** The declared default (always shown beside a value). */
  defaultValue: string;
  /** The env var in force, when `source === "environment"` (else the key). */
  envVar: string;
  /** A saved value the environment layer shadows — only when in force (else null). */
  shadowed: string | null;
  /** Who last changed it, and when — empty string when never recorded. */
  updatedBy: string;
  updatedAt: string;
}

export function describeLayers(view: SettingView): LayerDescription {
  return {
    source: sourceBadge(view),
    value: view.value,
    defaultValue: view.default,
    envVar: view.shadowedBy || view.key,
    // A saved value is "shadowed" only when the environment layer is in force —
    // for a `database` source the saved value is the winner, not the shadow.
    shadowed: view.source === "environment" ? view.stored : null,
    updatedBy: view.updatedBy,
    updatedAt: view.updatedAt,
  };
}

/**
 * The result of checking a control's draft against the daemon's contract.
 *
 * `value` is the canonical text to send — the draft unchanged (parseDraft
 * converts nothing: the control produced canonical text, CS3). `reason` is why
 * the draft is malformed; when it is present the panel never sends it.
 */
export interface DraftResult {
  ok: boolean;
  value?: string;
  reason?: string;
}

/**
 * Validate a control's draft and hand back the canonical text to send — or the
 * reason the draft is malformed, which the panel never sends (CS3).
 *
 * - `bool` — only `"true"` / `"false"`; anything else (e.g. `"yes"`) is malformed.
 * - `int`  — canonical base-10 only: digits, an optional leading minus, no
 *            leading zero, no `+`, no exponent (`"007"`, `"1e3"`, `"+1"`, `""`
 *            are all malformed), within `min`/`max` inclusive.
 * - `enum` — must be a declared member.
 * - `string` / any other type — within `max` (the length bound) ?? 4096.
 */
export function parseDraft(view: SettingView, text: string): DraftResult {
  switch (controlKindFor(view)) {
    case "switch":
      return text === "true" || text === "false"
        ? { ok: true, value: text }
        : { ok: false, reason: "expected 'true' or 'false'" };

    case "number": {
      if (!/^-?(0|[1-9]\d*)$/.test(text)) {
        return { ok: false, reason: "expected canonical base-10 digits" };
      }
      const n = Number(text);
      const withinBounds = inBounds(view, n);
      if (!withinBounds.ok) return { ok: false, reason: withinBounds.reason };
      return { ok: true, value: text };
    }

    case "select": {
      const members = view.enumValues ?? [];
      if (members.length > 0 && !members.includes(text)) {
        return { ok: false, reason: `not one of: ${members.join(", ")}` };
      }
      return { ok: true, value: text };
    }

    case "text":
    default: {
      const maxLen = view.max ?? 4096;
      if (text.length > maxLen) {
        return { ok: false, reason: `too long (max ${maxLen})` };
      }
      return { ok: true, value: text };
    }
  }
}

/** `min`/`max` are inclusive bounds; a value past either names the range. */
function inBounds(view: SettingView, n: number): { ok: boolean; reason?: string } {
  if (view.min !== undefined && n < view.min) return { ok: false, reason: rangeMessage(view) };
  if (view.max !== undefined && n > view.max) return { ok: false, reason: rangeMessage(view) };
  return { ok: true };
}

function rangeMessage(view: SettingView): string {
  if (view.min !== undefined && view.max !== undefined) {
    return `must be between ${view.min} and ${view.max}`;
  }
  if (view.max !== undefined) return `must be ≤ ${view.max}`;
  if (view.min !== undefined) return `must be ≥ ${view.min}`;
  return "out of range";
}

/** Nothing to reset when the default is in force and no value is saved. */
export function resetDisabled(view: SettingView): boolean {
  return view.source === "default" && view.stored === null;
}

/** An environment variable that is set but invalid for the setting (CS). */
export function hasEnvInvalid(view: SettingView): boolean {
  return view.envInvalid !== "";
}

/** A saved value that is no longer valid — the default is in force (CS). */
export function hasStoredInvalid(view: SettingView): boolean {
  return view.storedInvalid;
}
