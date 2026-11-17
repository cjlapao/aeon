import { useEffect, useState } from "react";
import { Alert, Button, Input, Panel, Pill } from "../../../components/kit";
import { rpc, rpcTyped, toRpcError, type RpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { useFact } from "../lib/fact";
import { FactCell } from "../lib/FactCell";
import { ProbeChip, type ProbeSummary } from "../lib/ProbeChip";
import { createProposal } from "../lib/proposals";
import { ProposeImportForm, type ProposalCreateResult } from "./ProposeImportForm";
import type { RpcMethods } from "../../../proto/contract";

type GrantRow = RpcMethods["tools.grants"]["result"]["grants"][number];
type InForceAnswer = RpcMethods["policy.inForce"]["result"]["answers"][number];

const GRANT_GROUPS: { state: string; label: string }[] = [
  { state: "available", label: "Available" },
  { state: "ask", label: "Asks at use" },
  { state: "unavailable", label: "Unavailable" },
  { state: "refused", label: "Refused" },
];

/**
 * S34 T6 — the readiness view (design.md §3): identity, tools (the real
 * `GrantView` states — TB7, never merged), MCP declares, guard verdicts.
 * Every read goes through `useFact`/`rpcTyped` (TB1) — no other data path.
 */
export function AgentsPage() {
  const roster = useFact("agent.list", {});
  const [selected, setSelected] = useState<string | null>(null);
  const hasSelection = selected !== null;
  const grants = useFact("tools.grants", { agent: selected ?? "" }, hasSelection);
  const mcpStatus = useFact("mcp.status", {});
  const inForce = useFact("policy.inForce", { agent: selected ?? "" }, hasSelection);

  const refreshAll = () => {
    roster.refetch();
    mcpStatus.refetch();
    if (hasSelection) {
      grants.refetch();
      inForce.refetch();
    }
  };

  return (
    <div className="space-y-4" data-testid="agents-page">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-widest text-slate-400">S34 T6 · trust bench</p>
          <h2 className="mt-1 text-lg font-semibold">Agents — the readiness view</h2>
        </div>
        <Button size="sm" variant="ghost" onClick={refreshAll} data-testid="agents-refresh">
          refresh
        </Button>
      </div>

      {/* S37 T6 — Propose import: a proposal, never a commit. ProposalsPage is
          untouched; the one write is proposal.create, and the toast + preview
          are S35's exact pattern, read from the response. */}
      <Panel tone="slate">
        <ProposeImportForm
          create={(kind, payload) => rpc<ProposalCreateResult>("proposal.create", { kind, payload })}
          onToast={(message) => notifications.push({ title: message, tone: "info", source: "local" })}
        />
      </Panel>

      {/* 1. Identity */}
      <Panel tone="slate">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold">Identity</h3>
          <CliTwin command="aeon agent list --provenance --all" />
        </div>
        <FactCell fact={roster.fact}>
          {(value) => (
            <ul className="mt-3 space-y-1" data-testid="agents-roster">
              {value.agents.length === 0 && <p className="text-sm text-slate-500">no agents registered</p>}
              {value.agents.map((a) => (
                <li key={a.name} className="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setSelected(a.name)}
                    data-testid={`agent-row-${a.name}`}
                    aria-pressed={selected === a.name}
                    className={`flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-slate-100 dark:hover:bg-white/5! ${selected === a.name ? "bg-slate-100 dark:bg-white/10!" : ""}`}
                  >
                    <span className="font-medium">{a.name}</span>
                    <Pill tone={a.provenance === "first_party" ? "blue" : "violet"} size="xs">{a.provenance}</Pill>
                    <Pill tone={a.state === "revoked" ? "red" : a.state === "disabled" ? "neutral" : "emerald"} size="xs">
                      {a.state}
                    </Pill>
                    {a.source && <span className="truncate text-xs text-slate-400">{a.source}</span>}
                  </button>
                  {a.provenance !== "first_party" && a.state !== "revoked" && (
                    <AgentTransitionControls name={a.name} state={a.state} onChanged={() => roster.refetch()} />
                  )}
                </li>
              ))}
            </ul>
          )}
        </FactCell>
      </Panel>

      {!hasSelection && (
        <Alert tone="slate" title="Select an agent" description="Its tools, MCP declares and guard verdicts render below." />
      )}

      {hasSelection && (
        <>
          {/* 2. Tools — the four GrantView states, never merged (TB7). */}
          <Panel tone="slate">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-sm font-semibold">Tools — {selected}</h3>
              <CliTwin command={`aeon mcp list --agent ${selected}`} />
            </div>
            <FactCell fact={grants.fact}>{(value) => <GrantGroups agent={selected} grants={value.grants} />}</FactCell>
          </Panel>

          {/* 3. MCP declares */}
          <Panel tone="slate">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-sm font-semibold">MCP declares</h3>
              <CliTwin command="aeon mcp list" />
            </div>
            <FactCell fact={mcpStatus.fact}>
              {(value) => {
                const servers = (value.servers ?? []).filter((s) => s.declaredBy.includes(selected));
                const probes = value.probes as unknown as Record<string, ProbeSummary>;
                if (servers.length === 0) {
                  return <p className="mt-3 text-sm text-slate-500" data-testid="agents-mcp-none">declares no MCP server</p>;
                }
                return (
                  <ul className="mt-3 space-y-1.5" data-testid="agents-mcp-declares">
                    {servers.map((s) => (
                      <li key={s.name} className="flex items-center gap-2 text-sm" data-testid={`agents-mcp-declare-${s.name}`}>
                        <span className="font-medium">{s.name}</span>
                        <Pill tone="neutral" size="xs">{s.tier}</Pill>
                        <ProbeChip probe={probes[s.name]} />
                      </li>
                    ))}
                  </ul>
                );
              }}
            </FactCell>
          </Panel>

          {/* 4. Guard verdicts */}
          <Panel tone="slate">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-sm font-semibold">Guard verdicts</h3>
              <CliTwin command={`aeon policy in-force --agent ${selected}`} />
            </div>
            <FactCell fact={inForce.fact}>{(value) => <InForceList answers={value.answers} />}</FactCell>
          </Panel>
        </>
      )}
    </div>
  );
}

function GrantGroups({ agent, grants }: { agent: string; grants: GrantRow[] }) {
  if (grants.length === 0) {
    return <p className="mt-3 text-sm text-slate-500" data-testid="agents-grants-none">nothing declared or registered</p>;
  }
  return (
    <div className="mt-3 space-y-3">
      {GRANT_GROUPS.map((g) => {
        const rows = grants.filter((r) => r.state === g.state);
        if (rows.length === 0) return null;
        return (
          <div key={g.state} data-testid={`agents-grant-group-${g.state}`}>
            <p className="text-xs font-semibold uppercase tracking-widest text-slate-400">
              {g.label} ({rows.length})
            </p>
            <ul className="mt-1 space-y-1">
              {rows.map((r) => (
                <GrantRowView key={r.qualified} agent={agent} row={r} />
              ))}
            </ul>
          </div>
        );
      })}
    </div>
  );
}

function GrantRowView({ agent, row }: { agent: string; row: GrantRow }) {
  const [open, setOpen] = useState(false);
  return (
    <li className="text-sm" data-testid={`agents-grant-${row.qualified}`}>
      <div className="flex flex-wrap items-center gap-2">
        <Pill tone="neutral" size="xs">{row.tier}</Pill>
        <span className="font-mono text-xs">{row.qualified}</span>
        {row.reason && (
          <button type="button" onClick={() => setOpen((v) => !v)} className="text-xs text-blue-600 underline dark:text-blue-300!" data-testid={`agents-grant-why-${row.qualified}`}>
            why
          </button>
        )}
        <GrantChangeToggle agent={agent} row={row} />
      </div>
      {open && row.reason && <p className="mt-0.5 pl-2 text-xs text-slate-500">{row.reason}</p>}
      {open && <WhyExpand agent={agent} tool={row.qualified} />}
    </li>
  );
}

/** `policy.why` on expand — fetched lazily, scoped to the one tool a Tools-strip row names (design §3.4). */
function WhyExpand({ agent, tool }: { agent: string; tool: string }) {
  const [state, setState] = useState<{ kind: "loading" } | { kind: "ok"; sentence: string } | { kind: "error"; error: RpcError }>({ kind: "loading" });
  useEffect(() => {
    void rpcTyped("policy.why", { agent, type: "tool_access", tool })
      .then((r) => setState({ kind: "ok", sentence: r.sentence }))
      .catch((cause) => setState({ kind: "error", error: toRpcError(cause) }));
  }, [agent, tool]);
  if (state.kind === "loading") return <p className="pl-2 text-xs text-slate-400">asking policy.why…</p>;
  if (state.kind === "error") return <p className="pl-2 text-xs text-red-600 dark:text-red-300!">{state.error.message}</p>;
  return <p className="pl-2 text-xs text-slate-500" data-testid={`agents-grant-why-sentence-${tool}`}>{state.sentence}</p>;
}

function InForceList({ answers }: { answers: InForceAnswer[] }) {
  if (answers.length === 0) {
    return <p className="mt-3 text-sm text-slate-500" data-testid="agents-inforce-none">no policy names this agent</p>;
  }
  return (
    <ul className="mt-3 space-y-1.5" data-testid="agents-inforce-list">
      {answers.map((a) => (
        <li key={a.policyID} className="text-sm" data-testid={`agents-inforce-${a.policyID}`}>
          <Pill tone="neutral" size="xs">{a.state}</Pill> <span className="text-xs">{a.render}</span>
        </li>
      ))}
    </ul>
  );
}

/** TB10 — every strip's CLI twin, shown as quiet copyable text; the app never shells out. */
function CliTwin({ command }: { command: string }) {
  return <code className="font-mono text-[11px] text-slate-400" data-testid="cli-twin">{command}</code>;
}

/**
 * Enable/disable/revoke — S35 T4's plain writes, live immediately (TD5:
 * the imported agent already passed blocklist + consent at import, so
 * moving it starts nothing new, unlike a proposal). No "proposed" language
 * anywhere on this control: `agent.enable`/`disable`/`revoke` apply the
 * moment they are clicked, and the Identity strip's own refetch is what
 * shows the new state — never a staged row.
 */
function AgentTransitionControls({ name, state, onChanged }: { name: string; state: string; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);

  const move = async (method: "agent.enable" | "agent.disable") => {
    setBusy(true);
    setError(null);
    try {
      await rpc(method, { name });
      onChanged();
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  const revoke = async () => {
    if (reason.trim() === "") return; // mirrors the CLI's own demand, client-side — the real guard is the daemon's CHECK
    setBusy(true);
    setError(null);
    try {
      await rpc("agent.revoke", { name, reason });
      setRevoking(false);
      setReason("");
      onChanged();
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="flex flex-wrap items-center gap-1" onClick={(e) => e.stopPropagation()}>
      {state !== "enabled" && (
        <Button size="xs" variant="soft" disabled={busy} onClick={() => void move("agent.enable")} data-testid={`agent-enable-${name}`}>
          enable
        </Button>
      )}
      {state !== "disabled" && (
        <Button size="xs" variant="soft" disabled={busy} onClick={() => void move("agent.disable")} data-testid={`agent-disable-${name}`}>
          disable
        </Button>
      )}
      {!revoking ? (
        <Button size="xs" variant="soft" disabled={busy} onClick={() => setRevoking(true)} data-testid={`agent-revoke-open-${name}`}>
          revoke
        </Button>
      ) : (
        <span className="flex items-center gap-1">
          <Input
            size="xs"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="reason — required, survives (AP4)"
            data-testid={`agent-revoke-reason-${name}`}
          />
          <Button size="xs" disabled={busy || reason.trim() === ""} onClick={() => void revoke()} data-testid={`agent-revoke-confirm-${name}`}>
            confirm
          </Button>
        </span>
      )}
      {error && <span className="text-xs text-red-700 dark:text-red-300!" data-testid={`agent-transition-error-${name}`}>{error}</span>}
    </span>
  );
}

/**
 * The Tools strip's "request a change" affordance — a STAGED proposal
 * (TD7), never a plain write: a grant toggle is policy, S12's ruling, and
 * only `aeon proposal commit` ever applies it. Proposes the opposite
 * action from the tool's current effective state.
 */
function GrantChangeToggle({ agent, row }: { agent: string; row: GrantRow }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const nextAction = row.state === "available" ? "deny" : "allow";

  const propose = async () => {
    setBusy(true);
    setError(null);
    try {
      await createProposal("policy_add", {
        id: `ui.${agent}.${row.qualified.replace(/[^a-zA-Z0-9_.-]/g, "-")}.${Date.now()}`,
        type: "tool_access",
        target: { kind: "agent", id: agent },
        action: nextAction,
        source: "operator",
        spec: { toolName: row.qualified, state: nextAction },
      });
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="flex items-center gap-1">
      <Button size="xs" variant="soft" disabled={busy} onClick={() => void propose()} data-testid={`agents-grant-propose-${row.qualified}`}>
        propose {nextAction}
      </Button>
      {error && <span className="text-xs text-red-700 dark:text-red-300!">{error}</span>}
    </span>
  );
}
