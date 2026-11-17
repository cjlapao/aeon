import type { NetworkGraphNode, NetworkGraphEdge, NetworkGraphCommunity, TrueColor } from "@cjlapao/ui-kit";
import type { RpcMethods } from "../../../proto/contract";

/**
 * graphNetwork — the code graph as the kit's `NetworkGraph` wants it (S30 T7; shared by the bench's
 * Graph page and Settings → Platform status). Pure over a daemon answer: no canvas, no daemon call, so the
 * mapping is testable without either. Only a `found` result draws; every other state is spoken by
 * the caller, never drawn as connections the daemon did not send.
 */
export type ExplainResult = RpcMethods["graph.explain"]["result"];
export type PathResult = RpcMethods["graph.path"]["result"];
export type NodeRef = NonNullable<ExplainResult["node"]>;

// The code-graph kinds → the ui-kit's TrueColor palette, for the network's
// community legend. A kind nobody has named falls back to slate — the same
// neutral the page uses for its "no answer yet" — so an unknown kind is
// visible, never swallowed.
const KIND_TONES: Record<string, TrueColor> = {
  function: "blue",
  method: "teal",
  type: "violet",
  interface: "amber",
  class: "green",
  file: "slate",
  package: "indigo",
};

export function toneForKind(kind: string): TrueColor {
  return KIND_TONES[kind] ?? "slate";
}

export interface GraphNetwork {
  nodes: NetworkGraphNode[];
  edges: NetworkGraphEdge[];
  communities: NetworkGraphCommunity[];
  /** String id → the originating node, so a click maps back to file:lines. */
  byId: Map<string, NodeRef>;
}

// buildGraphNetwork renders a found explain result as an ego-network for the
// ui-kit's NetworkGraph: the central symbol plus its direct callers (incoming)
// and callees (outgoing), coloured by kind. It is pure over the found result,
// so the mapping is testable without the canvas. A found result always carries
// a node (the daemon's contract), but a null node is refused here rather than
// thrown — the network is one rendering of the same found data, and it must
// never be the surface that crashes (the roots.map lesson, repeated).
export function buildGraphNetwork(res: ExplainResult | null): GraphNetwork | null {
  if (!res || res.state !== "found" || !res.node) return null;
  const center = res.node;
  const byId = new Map<string, NodeRef>();
  const nodes: NetworkGraphNode[] = [];
  const communities: NetworkGraphCommunity[] = [];
  const seenNode = new Set<string>();
  const seenCommunity = new Set<string>();

  const addNode = (n: NodeRef) => {
    const id = String(n.id);
    if (seenNode.has(id)) return;
    seenNode.add(id);
    nodes.push({ id, label: n.symbol, community: n.kind });
    byId.set(id, n);
    if (!seenCommunity.has(n.kind)) {
      seenCommunity.add(n.kind);
      communities.push({ id: n.kind, label: n.kind, tone: toneForKind(n.kind) });
    }
  };

  addNode(center);
  (res.outgoing ?? []).forEach((e) => addNode(e.to));
  (res.incoming ?? []).forEach((e) => addNode(e.from));

  const edges: NetworkGraphEdge[] = [];
  const seenEdge = new Set<string>();
  const addEdge = (source: string, target: string, dir: "out" | "in", i: number) => {
    const key = `${source}->${target}`;
    if (seenEdge.has(key)) return;
    seenEdge.add(key);
    edges.push({ id: `${dir}${i}-${source}-${target}`, source, target });
  };
  (res.outgoing ?? []).forEach((e, i) => addEdge(String(center.id), String(e.to.id), "out", i));
  (res.incoming ?? []).forEach((e, i) => addEdge(String(e.from.id), String(center.id), "in", i));

  return { nodes, edges, communities, byId };
}


/** A path answer as a chain: the start node and each hop's node, joined in order. The hop edges are
 *  returned by id so the canvas can highlight them. Only a `found`-shaped answer (a start and at least
 *  one hop) draws; the daemon's other path states are the caller's to say. */
export function buildPathNetwork(res: PathResult | null): (GraphNetwork & { highlighted: string[] }) | null {
  if (!res || !res.start || !Array.isArray(res.hops) || res.hops.length === 0) return null;
  const byId = new Map<string, NodeRef>();
  const nodes: NetworkGraphNode[] = [];
  const communities: NetworkGraphCommunity[] = [];
  const seenCommunity = new Set<string>();
  const add = (n: NodeRef) => {
    const id = String(n.id);
    if (byId.has(id)) return;
    byId.set(id, n);
    nodes.push({ id, label: n.symbol, community: n.kind });
    if (!seenCommunity.has(n.kind)) {
      seenCommunity.add(n.kind);
      communities.push({ id: n.kind, label: n.kind, tone: toneForKind(n.kind) });
    }
  };
  add(res.start);
  const edges: NetworkGraphEdge[] = [];
  let prev = String(res.start.id);
  res.hops.forEach((hop, i) => {
    add(hop.to);
    const next = String(hop.to.id);
    edges.push({ id: `hop${i}-${prev}-${next}`, source: prev, target: next });
    prev = next;
  });
  return { nodes, edges, communities, byId, highlighted: edges.map((e) => e.id) };
}
