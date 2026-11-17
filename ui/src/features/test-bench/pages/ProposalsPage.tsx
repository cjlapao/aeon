import { useState } from "react";
import { Alert, Button, Panel } from "../../../components/kit";
import { useFact } from "../lib/fact";
import { FactCell } from "../lib/FactCell";
import { ProposalRowView } from "../lib/ProposalRowView";
import type { ProposalRow } from "../lib/proposals";
import type { RpcMethods } from "../../../proto/contract";

/**
 * S35 T7 — the desk. Open proposals, newest first, each with a live-vs-
 * proposed diff and the exact CLI command. **No commit affordance anywhere
 * on this page, in any form** (TD7) — the only action is Discard, because
 * discarding loosens nothing while committing widens, and brief §"What
 * this is not" names a commit-behind-a-dialog as a new ruling, never a UI
 * choice this page gets to make.
 */
export function ProposalsPage() {
  const proposals = useFact("proposal.list", {});
  const mcpStatus = useFact("mcp.status", {});
  const policyList = useFact("policy.list", {});
  const [discarded, setDiscarded] = useState<Set<number>>(new Set());

  type McpStatusResult = RpcMethods["mcp.status"]["result"];
  const liveMcpServers =
    mcpStatus.fact.state === "ok"
      ? ((mcpStatus.fact.value as McpStatusResult).servers ?? []).map((s) => ({ name: s.name, tier: s.tier, transport: s.transport }))
      : undefined;
  const livePolicyIds =
    policyList.fact.state === "ok" ? (policyList.fact.value as RpcMethods["policy.list"]["result"]).policies.map((p) => p.id) : undefined;

  return (
    <div className="space-y-4" data-testid="proposals-page">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-widest text-slate-400">S35 T7 · trust desk</p>
          <h2 className="mt-1 text-lg font-semibold">Proposals — the desk</h2>
        </div>
        <Button size="sm" variant="ghost" onClick={() => proposals.refetch()} data-testid="proposals-refresh">
          refresh
        </Button>
      </div>

      <Alert
        tone="slate"
        title="No commit button exists on this page, or anywhere in the app"
        description="Promoting a proposal is a human at a terminal: aeon proposal commit <id>. This desk creates and discards; nothing here applies an effect."
        data-testid="proposals-note"
      />

      <Panel tone="slate">
        <FactCell fact={proposals.fact}>
          {(value) => {
            type ProposalListResult = { proposals?: ProposalRow[] };
            const rows = ((value as ProposalListResult).proposals ?? []).filter((r) => !discarded.has(r.id));
            if (rows.length === 0) {
              return <p className="mt-3 text-sm text-slate-500" data-testid="proposals-empty">no proposals — the normal state when nobody has staged one</p>;
            }
            return (
              <ul className="mt-3 space-y-3" data-testid="proposals-list">
                {rows.map((row) => (
                  <ProposalRowView
                    key={row.id}
                    row={row}
                    liveMcpServers={liveMcpServers}
                    livePolicyIds={livePolicyIds}
                    onDiscarded={(id) => setDiscarded((s) => new Set(s).add(id))}
                  />
                ))}
              </ul>
            );
          }}
        </FactCell>
      </Panel>
    </div>
  );
}
