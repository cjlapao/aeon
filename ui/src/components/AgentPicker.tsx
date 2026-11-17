import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, EmptyState, Input } from "./kit";
import { ErrorPanel } from "./ErrorPanel";
import { RpcError } from "../lib/bridge";
import { useRpcQuery } from "../lib/useRpcQuery";
import { LiveController, type LiveState } from "../lib/live";
import type { RpcMethods } from "../proto/contract";

// The reusable agent picker (S45 T9, PM12; S46 reuses it).
//
// # The one identity rule
//
// The picker's output is **always a real roster value or empty** — never a
// free-typed string. A person searches; they cannot invent. `agent.list` is
// the roster the S43 management surface answers on (its own bench page:
// "the roster carries identity only"); the contract types each entry's
// `name` as that identity and no fixture ever showed a separate slug field,
// so the picker shows the identity and the display name it actually
// receives and generates nothing: no client-side slugify, no free-text
// accept. What the picker emits is what the roster sent, byte for byte.
//
// # Honest three states
//
// loading / error / the roster's own empty — an empty roster is stated ("no
// agents"), never rendered as a blank dropdown, and the control is disabled
// rather than pretending to have options.

/** One roster entry as `agent.list` answers it (the generated contract's shape). */
type RosterEntry = RpcMethods["agent.list"]["result"]["agents"][number];

/**
 * The identity a roster entry governs. The contract types `name`; a newer
 * daemon may also ship `slug` (the fixture-derived contract leaves fields it
 * never saw uncovered) — when it does, the slug is the identity shown and
 * emitted, because policy agent targets are slugs (PM13). Nothing here
 * derives one.
 */
function identityOf(entry: RosterEntry): string {
  const slug = (entry as { slug?: unknown }).slug;
  return typeof slug === "string" && slug !== "" ? slug : entry.name;
}

export interface AgentPickerProps {
  /** The selected agent identity ("" = none). */
  value: string;
  onChange: (value: string) => void;
  /** Offer an explicit "none" option (default false — a required target). */
  allowEmpty?: boolean;
  /**
   * Optional filter: only entries whose provenance equals this value are
   * offered (the roster's own field — e.g. S46 may only want imported
   * agents). Absent means every roster entry.
   */
  kind?: string;
  /** The daemon-backed host threads its connection in (SettingsScreen pattern). */
  controller?: LiveController;
  live?: LiveState;
  /** Accessible label for the search box. */
  label?: string;
}

/** The picker's own connection when a host does not pass one: a picker
 *  mounted outside a daemon-backed screen still reaches the daemon through
 *  the same generic bridge — one connection owner per window is the app's
 *  rule, so a host should pass `controller`/`live`; the fallback exists so
 *  the shared component degrades to its own honest reader, not to nothing. */
const fallbackController = new LiveController();

export function AgentPicker({
  value,
  onChange,
  allowEmpty = false,
  kind,
  controller = fallbackController,
  live,
  label,
}: AgentPickerProps) {
  const { t } = useI18n();
  const [search, setSearch] = useState("");
  const effectiveLive = live ?? controller.getState();

  const { data, loading, error, refetch } = useRpcQuery("agent.list", {}, controller, effectiveLive);

  const entries = useMemo(() => {
    const agents = Array.isArray(data?.agents) ? data.agents : [];
    // The `kind` filter is an equality on the roster's own provenance field
    // — a read of what the daemon sent, never a UI-closed set.
    return kind === undefined ? agents : agents.filter((a) => a.provenance === kind);
  }, [data, kind]);

  const needle = search.trim().toLowerCase();
  const matches = needle === ""
    ? entries
    : entries.filter(
        // Search by BOTH identities the roster carries — slug and display
        // name — case-insensitively.
        (a) => identityOf(a).toLowerCase().includes(needle) || a.name.toLowerCase().includes(needle),
      );

  const selectFromRoster = (candidate: string | undefined): void => {
    // The refusal that gives this picker its name: an exact roster value is
    // accepted, anything else — a typo, a half-typed name — is refused
    // outright and the selection stays untouched.
    if (candidate === undefined) {
      if (allowEmpty) onChange("");
      return;
    }
    const exact = entries.find((a) => identityOf(a) === candidate || a.name === candidate);
    if (exact) onChange(identityOf(exact));
  };

  const searchValue = search;
  const exactFromSearch =
    needle === "" ? undefined : matches.find((a) => identityOf(a).toLowerCase() === needle || a.name.toLowerCase() === needle);

  return (
    <div className="space-y-2" data-testid="agent-picker">
      <Input
        aria-label={label ?? t("components.agentPicker.searchLabel")}
        placeholder={t("components.agentPicker.searchPlaceholder")}
        value={searchValue}
        onChange={(event) => setSearch(event.target.value)}
      />
      {loading ? (
        <EmptyState title={t("components.agentPicker.loading")} />
      ) : error ? (
        <ErrorPanel error={new RpcError(0, error)} onRetry={refetch} />
      ) : entries.length === 0 ? (
        // The honest empty: a roster with no agents says so and the control
        // is disabled — never a blank dropdown that invites free text.
        <div className="space-y-2">
          <EmptyState title={t("components.agentPicker.noAgents")} />
          <Alert intent="warning" title={t("components.agentPicker.noAgentsDisabled")} />
        </div>
      ) : (
        <ul className="flex max-h-48 flex-wrap gap-2 overflow-y-auto" role="listbox" aria-label={t("components.agentPicker.listLabel")}>
          {allowEmpty && (
            <li>
              <button
                type="button"
                role="option"
                aria-selected={value === ""}
                data-testid="agent-picker-none"
                className={`rounded-md border px-2 py-1 text-sm ${
                  value === "" ? "aeon-accent-text border-current font-medium" : "border-slate-300 dark:border-slate-700"
                }`}
                onClick={() => selectFromRoster(undefined)}
              >
                {t("components.agentPicker.none")}
              </button>
            </li>
          )}
          {matches.length === 0 && (
            <li className="text-sm text-slate-500 dark:text-slate-400" data-testid="agent-picker-no-match">
              {t("components.agentPicker.noMatch")}
            </li>
          )}
          {matches.map((entry) => {
            const identity = identityOf(entry);
            const selected = identity === value;
            return (
              <li key={`${identity}-${entry.provenance}`}>
                <button
                  type="button"
                  role="option"
                  aria-selected={selected}
                  data-testid="agent-picker-option"
                  className={`rounded-md border px-2 py-1 text-sm ${
                    selected
                      ? "aeon-accent-text border-current font-medium"
                      : "border-slate-300 dark:border-slate-700"
                  }`}
                  onClick={() => selectFromRoster(identity)}
                >
                  {/* slug + display name, from the roster — the two things a
                      person uses to tell agents apart (D2 of the walk). */}
                  <span className="font-medium">{identity}</span>
                  {entry.name !== identity && <span className="ml-2 opacity-70">{entry.name}</span>}
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {/* Typing never selects: the only way a search becomes a value is a
          click on a roster row — so the search box shows what was refused. */}
      {searchValue.trim() !== "" && exactFromSearch === undefined && !loading && !error && (
        <p className="text-xs text-slate-500 dark:text-slate-400" data-testid="agent-picker-refused-free-text">
          {t("components.agentPicker.freeTextRefused")}
        </p>
      )}
    </div>
  );
}

export default AgentPicker;
