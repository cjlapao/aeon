import { useState } from "react";
import { Button, Pill } from "../../../components/kit";
import type { ProposalRow, McpServerAddPayload, McpServerRemovePayload } from "./proposals";
import { discardProposal, proposalStatusTone } from "./proposals";

// ProposalRowView — the one row renderer ProposalsPage and PolicyPage's
// Proposed table both import (TD6: one rendering, not reimplemented).
// Exactly ONE action anywhere on this component: Discard (TD7's side-door
// rule — there is no commit affordance here in any form, not a button, not
// a confirm dialog, not a keyboard shortcut).

interface LiveMcpServer {
  name: string;
  tier: string;
  transport: string;
}

export interface ProposalRowViewProps {
  row: ProposalRow;
  /** mcp.status's current servers, for the mcp_server_* diff. Omit to skip the diff (no live data fetched). */
  liveMcpServers?: LiveMcpServer[];
  /** policy.list's current rows (by id), for the policy_* diff. Omit to skip the diff. */
  livePolicyIds?: string[];
  onDiscarded?: (id: number) => void;
}

export function ProposalRowView({ row, liveMcpServers, livePolicyIds, onDiscarded }: ProposalRowViewProps) {
  const [discarding, setDiscarding] = useState(false);
  const [discardError, setDiscardError] = useState<string | null>(null);
  const isOpen = row.status === "proposed";

  const discard = async () => {
    setDiscarding(true);
    setDiscardError(null);
    try {
      const res = await discardProposal(row.id);
      if (res.ok === false) {
        setDiscardError(res.error ?? "discard refused");
        return;
      }
      onDiscarded?.(row.id);
    } finally {
      setDiscarding(false);
    }
  };

  return (
    <li className="rounded-md border border-slate-200 p-3 dark:border-white/10!" data-testid={`proposal-row-${row.id}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs opacity-60">#{row.id}</span>
        <Pill tone="neutral" size="xs">{row.kind}</Pill>
        <Pill tone={proposalStatusTone(row.status)} size="xs" data-testid={`proposal-status-${row.id}`}>{row.status}</Pill>
        <span className="text-xs opacity-60">{row.createdByKind}:{row.createdById}</span>
        {row.appliedError && (
          <span className="text-xs text-red-700 dark:text-red-300!" data-testid={`proposal-applied-error-${row.id}`}>
            {row.appliedError}
          </span>
        )}
      </div>

      <div className="mt-2" data-testid={`proposal-diff-${row.id}`}>
        <ProposalDiff row={row} liveMcpServers={liveMcpServers} livePolicyIds={livePolicyIds} />
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-2">
        {/* The exact CLI command, copyable text — never a structured action (TD6/TD7). */}
        <code className="font-mono text-[11px] text-slate-400" data-testid={`proposal-command-${row.id}`}>{row.command}</code>
        {isOpen && (
          <Button size="xs" variant="soft" onClick={() => void discard()} disabled={discarding} data-testid={`proposal-discard-${row.id}`}>
            {discarding ? "discarding…" : "Discard"}
          </Button>
        )}
      </div>
      {discardError && (
        <p className="mt-1 text-xs text-red-700 dark:text-red-300!" data-testid={`proposal-discard-error-${row.id}`}>{discardError}</p>
      )}
    </li>
  );
}

/** The live-vs-proposed diff, per kind — never a raw JSON dump where a per-kind rendering is readable. */
function ProposalDiff({ row, liveMcpServers, livePolicyIds }: { row: ProposalRow; liveMcpServers?: LiveMcpServer[]; livePolicyIds?: string[] }) {
  switch (row.kind) {
    case "mcp_server_add": {
      const p = row.payload as McpServerAddPayload;
      const already = liveMcpServers?.some((s) => s.name === p.server.name);
      return (
        <div className="text-xs">
          <p>
            <span className="font-mono">{p.server.name}</span> · {p.server.transport.type} · tier {p.tier}
            {p.agent ? ` · agent ${p.agent}` : ""}
          </p>
          <p className="opacity-60">
            {liveMcpServers === undefined ? "live config not loaded" : already ? "already configured live — a conflicting proposal" : "not in the live config yet — pending"}
          </p>
        </div>
      );
    }
    case "mcp_server_remove": {
      const p = row.payload as McpServerRemovePayload;
      const stillLive = liveMcpServers?.some((s) => s.name === p.name);
      return (
        <div className="text-xs">
          <p>remove <span className="font-mono">{p.name}</span></p>
          <p className="opacity-60">
            {liveMcpServers === undefined ? "live config not loaded" : stillLive ? "still configured live — removing this would take effect" : "already absent from the live config"}
          </p>
        </div>
      );
    }
    case "policy_add": {
      const p = row.payload as { id?: string; action?: string; type?: string };
      const collides = livePolicyIds?.includes(p.id ?? "");
      return (
        <div className="text-xs">
          <p><span className="font-mono">{p.id}</span> · {p.type} · {p.action}</p>
          <p className="opacity-60">
            {livePolicyIds === undefined ? "live corpus not loaded" : collides ? "already in the live corpus — a conflicting proposal" : "not in the live corpus yet — pending"}
          </p>
        </div>
      );
    }
    case "policy_remove": {
      const p = row.payload as { id?: string };
      const stillLive = livePolicyIds?.includes(p.id ?? "");
      return (
        <div className="text-xs">
          <p>remove <span className="font-mono">{p.id}</span></p>
          <p className="opacity-60">
            {livePolicyIds === undefined ? "live corpus not loaded" : stillLive ? "still in the live corpus — removing this would take effect" : "already absent from the live corpus"}
          </p>
        </div>
      );
    }
    case "seed_corpus": {
      const p = row.payload as { agents?: string[] };
      return (
        <div className="text-xs">
          <p>seed corpus rules for: {(p.agents ?? []).join(", ")}</p>
          <p className="opacity-60">per-rule: an existing rule is left alone, re-running seeds nothing twice</p>
        </div>
      );
    }
  }
}
