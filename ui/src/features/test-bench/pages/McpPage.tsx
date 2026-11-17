import { useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpcTyped, toRpcError } from "../../../lib/bridge";
import { useFact } from "../lib/fact";
import { FactCell } from "../lib/FactCell";
import { ProbeChip, type ProbeSummary } from "../lib/ProbeChip";
import { createProposal, type McpServerAddPayload, type ProposalRow } from "../lib/proposals";
import type { RpcMethods } from "../../../proto/contract";

type ServerRow = NonNullable<RpcMethods["mcp.status"]["result"]["servers"]>[number];

/**
 * S34 T7 — the config-and-check view (design.md §4), reconciled against
 * T3/T4's reality findings: the real loader is all-or-nothing (no
 * per-server `refused` row — a broken config is a whole-file state), and
 * the chip vocabulary carries the real field names, never design.md's
 * original per-server guess.
 */
export function McpPage() {
  const status = useFact("mcp.status", {});
  const proposals = useFact("proposal.list", {});
  const [adding, setAdding] = useState(false);
  // Single-flight at the UI layer too (TB8's UI half): a second click while
  // one is in flight for the same server must not even SEND a second
  // mcp.check — the daemon's own single-flight (T4) would absorb it, but
  // TestMcpPageCheckButtonSingleFlightAcrossClicks wants exactly one CALL,
  // not just one dial. Component state is enough for this page's one
  // instance (the task's "module-level" robustness is for a multi-instance
  // case that does not exist here).
  const [checking, setChecking] = useState<Set<string>>(new Set());
  const [checked, setChecked] = useState<Map<string, ProbeSummary>>(new Map());

  const check = (server: string) => {
    if (checking.has(server)) return;
    setChecking((s) => new Set(s).add(server));
    void rpcTyped("mcp.check", { server })
      .then((result) => {
        setChecked((m) => new Map(m).set(server, { ok: result.ok, tools: result.tools?.length, error: result.error, ms: result.ms, checkedAt: result.checkedAt }));
      })
      .catch((cause) => {
        setChecked((m) => new Map(m).set(server, { ok: false, error: toRpcError(cause).message, ms: 0, checkedAt: new Date().toISOString() }));
      })
      .finally(() => {
        setChecking((s) => {
          const next = new Set(s);
          next.delete(server);
          return next;
        });
      });
  };

  return (
    <div className="space-y-4" data-testid="mcp-page">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-widest text-slate-400">S34 T7 · trust bench</p>
          <h2 className="mt-1 text-lg font-semibold">MCP — the config-and-check view</h2>
        </div>
        <Button size="sm" variant="ghost" onClick={() => { status.refetch(); proposals.refetch(); }} data-testid="mcp-refresh">
          refresh
        </Button>
      </div>

      <Panel tone="slate">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold">Configured servers</h3>
          <code className="font-mono text-[11px] text-slate-400" data-testid="cli-twin">aeon mcp list</code>
        </div>
        <FactCell fact={status.fact}>
          {(value) => {
            if (!value.present) {
              return (
                <Alert
                  tone="slate"
                  title="No MCP configuration"
                  description={`Looked at ${value.configPath}.`}
                  data-testid="mcp-absent"
                />
              );
            }
            if (value.loadError) {
              return (
                <Alert
                  tone="red"
                  title="The configuration could not be loaded"
                  description={value.loadError}
                  data-testid="mcp-broken"
                />
              );
            }
            const servers = value.servers ?? [];
            const probes = { ...(value.probes as unknown as Record<string, ProbeSummary>) };
            for (const [name, p] of checked) probes[name] = p;
            const openProposals: ProposalRow[] =
              proposals.fact.state === "ok"
                ? ((proposals.fact.value as unknown as { proposals: ProposalRow[] }).proposals ?? []).filter((p) => p.status === "proposed")
                : [];
            const pendingRemoveByName = new Set(
              openProposals.filter((p) => p.kind === "mcp_server_remove").map((p) => (p.payload as { name: string }).name),
            );
            const pendingAdds = openProposals.filter((p) => p.kind === "mcp_server_add");
            if (servers.length === 0 && pendingAdds.length === 0) {
              return <p className="mt-3 text-sm text-slate-500" data-testid="mcp-empty-servers">no servers configured</p>;
            }
            return (
              <table className="mt-3 w-full text-sm" data-testid="mcp-servers-table">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-widest text-slate-400">
                    <th className="pb-2">Server</th>
                    <th className="pb-2">Tier</th>
                    <th className="pb-2">Transport</th>
                    <th className="pb-2">Declared by</th>
                    <th className="pb-2">Last check</th>
                    <th className="pb-2" />
                  </tr>
                </thead>
                <tbody>
                  {servers.map((s) => (
                    <ServerRowView
                      key={s.name}
                      server={s}
                      checking={checking.has(s.name)}
                      probe={probes[s.name]}
                      onCheck={() => check(s.name)}
                      pendingRemove={pendingRemoveByName.has(s.name)}
                      onRemoved={() => proposals.refetch()}
                    />
                  ))}
                  {pendingAdds.map((p) => (
                    <PendingAddRow key={p.id} proposal={p} />
                  ))}
                </tbody>
              </table>
            );
          }}
        </FactCell>
      </Panel>

      <Panel tone="slate">
        <div className="flex items-center justify-between gap-3">
          <h3 className="text-sm font-semibold">Add server</h3>
          {!adding && (
            <Button size="sm" variant="soft" onClick={() => setAdding(true)} data-testid="mcp-add-open">
              Add server
            </Button>
          )}
        </div>
        {adding && <AddServerForm onDone={() => { setAdding(false); proposals.refetch(); }} onCancel={() => setAdding(false)} />}
      </Panel>
    </div>
  );
}

function ServerRowView({
  server, checking, probe, onCheck, pendingRemove, onRemoved,
}: { server: ServerRow; checking: boolean; probe?: ProbeSummary; onCheck: () => void; pendingRemove: boolean; onRemoved: () => void }) {
  const target = server.transport === "stdio" ? server.command ?? "" : server.url ?? "";
  const [removing, setRemoving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const removeIt = async () => {
    setRemoving(true);
    setError(null);
    try {
      await createProposal("mcp_server_remove", { name: server.name });
      onRemoved();
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setRemoving(false);
    }
  };

  return (
    <tr className="border-t border-slate-200 dark:border-white/10!" data-testid={`mcp-server-${server.name}`}>
      <td className="py-2 font-medium">
        {server.name}
        {pendingRemove && <Pill tone="amber" size="xs" className="ml-1" data-testid={`mcp-server-${server.name}-pending-remove`}>proposed · awaiting commit</Pill>}
      </td>
      <td className="py-2"><Pill tone="neutral" size="xs">{server.tier}</Pill></td>
      <td className="py-2 font-mono text-xs">{server.transport} · {target}</td>
      <td className="py-2 text-xs">{server.declaredBy.length > 0 ? server.declaredBy.join(", ") : "—"}</td>
      <td className="py-2"><ProbeChip probe={probe} checking={checking} /></td>
      <td className="py-2">
        <Button size="xs" variant="soft" onClick={onCheck} disabled={checking} data-testid={`mcp-check-${server.name}`}>
          {checking ? "checking…" : "Check"}
        </Button>
        <Button size="xs" variant="soft" onClick={() => void removeIt()} disabled={removing || pendingRemove} data-testid={`mcp-remove-${server.name}`}>
          {pendingRemove ? "remove proposed" : removing ? "proposing…" : "Remove"}
        </Button>
        {error && <p className="text-xs text-red-700 dark:text-red-300!">{error}</p>}
        <div>
          <code className="font-mono text-[11px] text-slate-400" data-testid="cli-twin">
            {server.declaredBy[0] ? `aeon mcp list --agent ${server.declaredBy[0]}` : "aeon mcp list"}
          </code>
        </div>
      </td>
    </tr>
  );
}

/**
 * A server that exists only as an open `mcp_server_add` proposal — no
 * Check button (MC11/TD8 extended to time): a probe on a server that does
 * not exist in the live config yet would be a lie, exactly what S34's
 * `not checked` chip is honest about elsewhere.
 */
function PendingAddRow({ proposal }: { proposal: ProposalRow }) {
  const p = proposal.payload as McpServerAddPayload;
  return (
    <tr className="border-t border-slate-200 dark:border-white/10!" data-testid={`mcp-server-pending-${p.server.name}`}>
      <td className="py-2 font-medium">
        {p.server.name}
        <Pill tone="amber" size="xs" className="ml-1">proposed · awaiting commit</Pill>
      </td>
      <td className="py-2"><Pill tone="neutral" size="xs">{p.tier}</Pill></td>
      <td className="py-2 font-mono text-xs">{p.server.transport.type} · {p.server.transport.command ?? p.server.transport.url ?? ""}</td>
      <td className="py-2 text-xs">{p.agent ?? "—"}</td>
      <td className="py-2 text-xs text-slate-500">not checked — not in the live config yet</td>
      <td className="py-2">
        <code className="font-mono text-[11px] text-slate-400" data-testid="cli-twin">{proposal.command}</code>
      </td>
    </tr>
  );
}

function AddServerForm({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const [name, setName] = useState("");
  const [tier, setTier] = useState<"public" | "private">("public");
  const [agent, setAgent] = useState("");
  const [transportType, setTransportType] = useState<"stdio" | "http">("stdio");
  const [command, setCommand] = useState("");
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = name.trim() !== "" && (tier === "public" || agent.trim() !== "") && (transportType === "stdio" ? command.trim() !== "" : url.trim() !== "");

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const payload: McpServerAddPayload = {
        tier,
        agent: tier === "private" ? agent : undefined,
        server: {
          name,
          transport: transportType === "stdio" ? { type: "stdio", command } : { type: "http", url },
        },
      };
      await createProposal("mcp_server_add", payload);
      onDone();
    } catch (cause) {
      setError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-3 space-y-3" data-testid="mcp-add-form">
      <div className="flex flex-wrap gap-3">
        <FormField label="name"><Input value={name} onChange={(e) => setName(e.target.value)} data-testid="mcp-add-name" /></FormField>
        <FormField label="tier">
          <Select value={tier} onChange={(e) => setTier(e.target.value as "public" | "private")} data-testid="mcp-add-tier">
            <option value="public">public</option>
            <option value="private">private</option>
          </Select>
        </FormField>
        {tier === "private" && (
          <FormField label="agent"><Input value={agent} onChange={(e) => setAgent(e.target.value)} data-testid="mcp-add-agent" /></FormField>
        )}
        <FormField label="transport">
          <Select value={transportType} onChange={(e) => setTransportType(e.target.value as "stdio" | "http")} data-testid="mcp-add-transport">
            <option value="stdio">stdio</option>
            <option value="http">http</option>
          </Select>
        </FormField>
        {transportType === "stdio" ? (
          <FormField label="command"><Input value={command} onChange={(e) => setCommand(e.target.value)} data-testid="mcp-add-command" /></FormField>
        ) : (
          <FormField label="url"><Input value={url} onChange={(e) => setUrl(e.target.value)} data-testid="mcp-add-url" /></FormField>
        )}
      </div>
      <p className="text-xs text-slate-500">
        Client-side shape check only — the daemon's `Config.Validate` (via T2's registry) is the authority.
      </p>
      {error && <p className="text-xs text-red-700 dark:text-red-300!" data-testid="mcp-add-error">{error}</p>}
      <div className="flex gap-2">
        <Button size="sm" disabled={busy || !canSubmit} onClick={() => void submit()} data-testid="mcp-add-submit">
          {busy ? "proposing…" : "Propose"}
        </Button>
        <Button size="sm" variant="ghost" onClick={onCancel} data-testid="mcp-add-cancel">cancel</Button>
      </div>
    </div>
  );
}
