import { rpc } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";

// proposals.ts — S35 T7: the shared desk infrastructure three pages
// (ProposalsPage, AgentsPage, McpPage, PolicyPage) all import rather than
// each rebuilding.
//
// `payload` is hand-typed as `unknown`, never read off `contract.ts`, for
// the same reason S34 T6's own ProbeSummary is: protogen infers a shape
// PURELY from the fixtures' own example JSON, and `proposal.create`'s five
// kinds carry five genuinely different payload shapes — any finite set of
// fixture examples produces a merged, wrong type for the others. Every
// caller here builds its own per-kind payload object and reads this
// module's `ProposalRow` back, never the generated `RpcMethods["proposal.
// list"]["result"]["proposals"][number]` shape.

/** The five closed kinds (migration 0042's CHECK, store.ProposalKinds). */
export type ProposalKind = "policy_add" | "policy_remove" | "seed_corpus" | "mcp_server_add" | "mcp_server_remove";

/** One proposal row — the real backend shape (protocol.ProposalRow), hand-typed. */
export interface ProposalRow {
  id: number;
  kind: ProposalKind;
  payload: unknown;
  status: "proposed" | "committed" | "rejected" | "discarded";
  createdAt: string;
  createdByKind: string;
  createdById: string;
  decidedAt?: string;
  decidedByKind?: string;
  decidedById?: string;
  decideReason?: string;
  appliedError?: string;
  command: string;
}

/** `mcp_server_add`'s payload shape (mirrors cmd/aeon's mcpServerAddPayload). */
export interface McpServerAddPayload {
  tier: "public" | "private";
  agent?: string;
  server: {
    name: string;
    transport: { type: "stdio" | "http"; command?: string; args?: string[]; env?: string[]; url?: string };
    tools?: { name: string; rerun?: string; effect?: string }[];
  };
}

/** `mcp_server_remove`'s payload shape. */
export interface McpServerRemovePayload {
  name: string;
}

/**
 * createProposal is every page's one write — `proposal.create` — ending in
 * the exact toast text TD7 promotes to a safety property:
 * `"Proposed · commit with <command>"`, read verbatim from the row the
 * daemon just returned (never recomputed client-side — the daemon owns
 * the CLI command grammar, this page only displays it).
 */
export async function createProposal(kind: ProposalKind, payload: unknown): Promise<ProposalRow> {
  const row = await rpc<ProposalRow>("proposal.create", { kind, payload });
  notifications.push({ title: `Proposed · commit with ${row.command}`, tone: "info", source: "local" });
  return row;
}

export async function listProposals(status?: ProposalRow["status"]): Promise<ProposalRow[]> {
  // rpc<T>, not rpcTyped: proposal.list's generated params type is
  // Record<string, never> — the one fixture's params happened to be {},
  // so protogen inferred "no fields ever," not "status is optional."
  const res = await rpc<{ proposals: ProposalRow[] }>("proposal.list", status ? { status } : {});
  return res.proposals;
}

export interface DiscardResult {
  ok?: boolean;
  error?: string;
  id?: number;
  status?: string;
}

export async function discardProposal(id: number): Promise<DiscardResult> {
  return rpc<DiscardResult>("proposal.discard", { id });
}

/** Status-chip tone — a shared vocabulary so the chip reads identically on every page. */
export function proposalStatusTone(status: ProposalRow["status"]): "amber" | "green" | "red" | "slate" {
  switch (status) {
    case "proposed": return "amber";
    case "committed": return "green";
    case "rejected": return "red";
    case "discarded": return "slate";
  }
}
