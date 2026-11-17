import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useI18n, useToast } from "@cjlapao/ui-kit";
import { onNavigate, onNavigateRefused, rpc, toRpcError } from "../lib/bridge";
import { registerNavigateHandler } from "../lib/notifications";

// The way back in, from the tray and from `aeon://` links (S58 T8, SV9).
//
// # Two parsers, one fixture — on purpose
//
// The Rust shell refuses a link it cannot parse before anything here ever
// sees it; this file parses it AGAIN, and both parsers read the same
// `test/fixtures/deeplinks.json` table (the Rust side through
// `include_str!`). Two parsers over independently-written expectations
// drift; two parsers over one shared table fail loudly the day either
// grammar moves. The table is the contract, not the two code paths.
//
// # What a link can do: navigate. That is the whole list.
//
// Nothing in a URL can answer, retry, or stop anything — the handler's
// vocabulary is `navigate` plus ONE read (`loop.show`, to tell a deleted
// loop from a slow one, with the daemon's own refusal rendered verbatim).
// Refusals are five fixed words; the raw URL is never rendered — the
// refused toast shows a reason from a closed list, never reflected text.
//
// # Where the links come from
//
// The shell emits `aeon://navigate` (canonical form, already parsed there)
// for tray clicks and accepted deep links, and `aeon://navigate-refused`
// (one reason word) for everything else. The in-app toast's "Open" action
// registers into the same handler, so a click inside the window and a
// click on the tray run the same code.

/** There is no Inbox screen yet (SV18): the inbox link goes Home today. */
const INBOX_PATH = "/";
// TODO(S58-follow-on, SV18): map `aeon://inbox` to `/inbox` when the Inbox
// screen specified in design.md SV18 is built. This table is the one place
// that mapping lives, so the follow-on is a route change and one fixture
// row — not a hunt through link handlers.
const LOOP_PATH = (id: string) => `/loops/${id}`;
const ROOT_PATH = (id: string) => `/components/${id}`;

/** What a link names: a place. `id` is present exactly for loop/root. */
export interface NavTarget {
  kind: "inbox" | "loop" | "root";
  id?: string;
}

/** The closed reason vocabulary the shell emits — the ONLY refusal text
 * this component will ever show. A payload outside it shows nothing but
 * the title, because echoing an unrecognized payload is exactly the
 * reflected-text failure the whole enum exists to prevent. */
const REFUSAL_WORDS = ["wrong-scheme", "unknown-door", "bad-target", "oversize-target", "carries-extra"] as const;

/**
 * The web-side mirror of the shell's `parse_link` (S58 T7): exactly
 * `aeon://inbox`, `aeon://loop/<id>`, `aeon://root/<id>`, the id pattern
 * `^[A-Za-z0-9_-]{1,64}$` — same grammar, same fixture table, same
 * refusals. Pure and total: never throws, never reads the network, never
 * returns anything but a place.
 */
export function parseTarget(url: unknown): NavTarget | null {
  if (typeof url !== "string" || !url.startsWith("aeon://")) return null;
  const rest = url.slice("aeon://".length);
  // No query, no fragment, no percent, no backslash — an accepted link
  // carries a place and nothing else, checked over the whole remainder
  // before any id does.
  if (/[?#%\\]/.test(rest)) return null;
  const segments = rest.split("/");
  if (segments[0] === "inbox") {
    return segments.length === 1 && segments[0] === "inbox" ? { kind: "inbox" } : null;
  }
  if (segments[0] === "loop" || segments[0] === "root") {
    if (segments.length !== 2) return null;
    const id = segments[1] ?? "";
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(id)) return null;
    const kind = segments[0];
    if (kind !== "loop" && kind !== "root") return null;
    return { kind, id };
  }
  return null;
}

/** The one mapping from place to route. Exported for the fixture test. */
export function pathFor(target: NavTarget): string {
  switch (target.kind) {
    case "inbox": return INBOX_PATH;
    case "loop": return LOOP_PATH(target.id ?? "");
    case "root": return ROOT_PATH(target.id ?? "");
  }
}

/**
 * Mounted once in `ProductShell`, beside `NotificationListener`: the two
 * navigate channels are listened here and nowhere else, and the toast
 * action reaches the same handler through the registry.
 */
export function NavigateListener() {
  const navigate = useNavigate();
  const { toast } = useToast();
  const { t } = useI18n();

  useEffect(() => {
    const go = (raw: unknown): void => {
      const target = parseTarget(raw);
      if (!target) {
        // A malformed payload on the ACCEPT channel is the shell saying
        // something the grammar refuses: same fixed words, never the raw
        // text, and no navigation — a link that is not a place goes
        // nowhere.
        toast.warning(t("notifications.link.refused"), "bad-target");
        return;
      }
      navigate(pathFor(target));
      if (target.kind === "loop" && target.id) {
        // The read that separates "deleted" from "slow": the destination
        // page will load it anyway; this only means a link to a loop that
        // no longer exists says so, in the daemon's own verbatim words,
        // instead of showing a blank workspace. A link navigates and
        // reads; it never acts.
        void rpc("loop.show", { id: target.id, includeBlueprint: false }).catch((cause: unknown) => {
          toast.danger(t("notifications.link.gone"), toRpcError(cause).message);
        });
      }
    };
    registerNavigateHandler(go);
    let unlisten: (() => void) | undefined;
    let disposed = false;
    void (async () => {
      try {
        const stopNavigate = await onNavigate(go);
        const stopRefused = await onNavigateRefused((reason: unknown) => {
          const word = REFUSAL_WORDS.includes(reason as (typeof REFUSAL_WORDS)[number])
            ? (reason as string)
            : "";
          // Title only for anything outside the enum — an unrecognized
          // payload is not quoted, ever.
          toast.warning(t("notifications.link.refused"), word);
        });
        if (disposed) {
          stopNavigate();
          stopRefused();
          return;
        }
        unlisten = () => {
          stopNavigate();
          stopRefused();
        };
      } catch {
        // `listen` is ACL-gated; a refused subscription is the documented
        // no-Tauri browser path — the links simply have no channel here,
        // which is the same silence every other listener keeps on that
        // path, not a failure to invent a second door for.
      }
    })();
    return () => {
      disposed = true;
      registerNavigateHandler(null);
      unlisten?.();
    };
  }, [navigate, toast, t]);

  return null;
}
