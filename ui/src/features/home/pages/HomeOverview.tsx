import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { StatCard, useI18n } from "@cjlapao/ui-kit";
import { ActionPanel, Button, EmptyState } from "../../../components/kit";
import { buildTree, countByRoot, rollup, workPath, type WorkComponent, type WorkLoop } from "../../work/workModel";

/**
 * The Home pieces that answer "what needs me, what is running, where is it" across every project.
 * Read-only: each row is a link into the loop or the component; nothing here writes.
 */

export interface HomeGate { loop: WorkLoop; prompt: string }

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * One thing waiting on a person. `to` is the address its button opens; a row with no `to` shows its
 * door sentence instead (a proposal whose deciding page this app does not know names its command).
 */
export interface NeedsYouRow {
  key: string;
  kind: "question" | "blocked" | "stopped" | "proposal" | "gate" | "triage";
  title: string;
  detail: string;
  componentRoot: string;
  at: string;
  to?: string;
  door?: string;
}

/** The daemon's attention item, as `inbox.list` answers it (the fields Home reads). */
export interface InboxItem {
  key: string; kind: string; severity: string; componentId?: string; loopId?: string; loopSlug?: string;
  itemRef?: string; reason: string; since: string; door?: { method?: string; sentence: string }; prompt?: string;
}

const loopTo = (loopId: string | undefined) => (loopId ? `/loops/${encodeURIComponent(loopId)}` : undefined);

/** A proposal's deciding page, by its kind prefix. Only the kinds this app has a page for; others name the command. */
function proposalTo(reason: string): string | undefined {
  const kind = /^an? (\S+) proposal/.exec(reason)?.[1] ?? "";
  if (kind.startsWith("policy_")) return "/config/policy";
  if (kind.startsWith("mcp_")) return "/config/mcp";
  if (kind.startsWith("agent_")) return "/config/agents";
  return undefined;
}

/** The daemon's feed → rows. The daemon decides what needs a person and in what order; this only names where each goes. */
export function rowsFromInbox(items: readonly InboxItem[], loops: readonly WorkLoop[], gates: readonly HomeGate[], components: readonly WorkComponent[]): NeedsYouRow[] {
  const prompts = new Map(gates.map((gate) => [gate.loop.id, gate.prompt]));
  const loopBySlug = new Map(loops.map((loop) => [`${loop.componentRoot}\u0000${loop.slug}`, loop]));
  const rootOf = (componentId: string | undefined) => components.find((c) => c.id === componentId)?.root ?? "";
  return items.map((item): NeedsYouRow => {
    const root = rootOf(item.componentId);
    const loop = item.loopId ? loops.find((l) => l.id === item.loopId) : loopBySlug.get(`${root}\u0000${item.loopSlug ?? ""}`);
    const loopId = item.loopId ?? loop?.id;
    const base = { key: item.key, detail: item.loopSlug || item.door?.sentence || "", componentRoot: root, at: item.since };
    if (item.kind.startsWith("question")) return { ...base, kind: "question", title: item.prompt || (loopId ? prompts.get(loopId) : undefined) || item.reason, to: loopTo(loopId) };
    if (item.kind === "stopped") return { ...base, kind: "stopped", title: item.reason, to: loopTo(loopId) };
    if (item.kind.startsWith("blocked")) return { ...base, kind: "blocked", title: loop?.title || item.loopSlug || item.reason, detail: item.reason, to: loopTo(loopId) };
    if (item.kind === "proposal") return { ...base, kind: "proposal", title: item.reason, detail: item.door?.sentence ?? "", to: proposalTo(item.reason), door: item.door?.sentence };
    if (item.kind === "gate-failed") return { ...base, kind: "gate", title: item.reason, detail: item.loopSlug ?? "", to: item.componentId ? `/work/${encodeURIComponent(item.componentId)}/guardrails` : undefined };
    return { ...base, kind: "triage", title: item.reason, detail: "", to: item.componentId ? `/work/${encodeURIComponent(item.componentId)}/guardrails` : undefined };
  });
}

/** The fallback when the daemon's inbox cannot be read: open questions and blocked loops, derived here. Oldest first. */
export function needsYouRows(loops: readonly WorkLoop[], gates: readonly HomeGate[]): NeedsYouRow[] {
  const asked = new Set(gates.map((gate) => gate.loop.id));
  const rows: NeedsYouRow[] = gates.map((gate) => ({
    key: `q-${gate.loop.id}`, kind: "question", title: gate.prompt, detail: gate.loop.title || gate.loop.slug, componentRoot: gate.loop.componentRoot, at: gate.loop.updatedAt, to: loopTo(gate.loop.id),
  }));
  for (const loop of loops) {
    if (loop.status !== "blocked" || asked.has(loop.id)) continue;
    rows.push({ key: `b-${loop.id}`, kind: "blocked", title: loop.title || loop.slug, detail: loop.phase || loop.slug, componentRoot: loop.componentRoot, at: loop.updatedAt, to: loopTo(loop.id) });
  }
  return rows.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
}

/** Loops that finished (not running, waiting or suspended) in the last day, split by how they ended. */
export function finishedToday(loops: readonly WorkLoop[], now: number): { total: number; blocked: number } {
  const done = loops.filter((loop) => !["running", "waiting", "suspended"].includes(loop.status) && now - Date.parse(loop.updatedAt) < DAY_MS);
  return { total: done.length, blocked: done.filter((loop) => loop.status === "blocked" || loop.status === "failed").length };
}

function rootLabel(components: readonly WorkComponent[], root: string): string {
  return components.find((item) => item.root === root)?.name ?? root.split("/").filter(Boolean).pop() ?? root;
}

export function HomeStats({ loops, rows, components, now }: { loops: readonly WorkLoop[]; rows: readonly NeedsYouRow[]; components: readonly WorkComponent[]; now: number }) {
  const { t } = useI18n();
  const where = new Set(rows.map((row) => row.componentRoot).filter(Boolean)).size;
  const running = loops.filter((loop) => loop.status === "running").length;
  const done = finishedToday(loops, now);
  return (
    <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 170px), 1fr))" }} data-testid="home-stats">
      <StatCard variant="simple" size="sm" padding="sm" tone={rows.length ? "amber" : "neutral"} icon="Warning" label={t("home.stat.needsYou")} value={rows.length} subtitle={t("home.stat.needsYouSub", { count: where })} data-testid="home-stat-needs" />
      <StatCard variant="simple" size="sm" padding="sm" tone="neutral" icon="Rocket" label={t("home.stat.running")} value={running} subtitle={t("home.stat.runningSub")} data-testid="home-stat-running" />
      <StatCard variant="simple" size="sm" padding="sm" tone="neutral" icon="Check" label={t("home.stat.finished")} value={done.total} subtitle={t("home.stat.finishedSub", { count: done.blocked })} data-testid="home-stat-finished" />
      <StatCard variant="simple" size="sm" padding="sm" tone="neutral" icon="Group" label={t("home.stat.components")} value={components.length} subtitle={t("home.stat.componentsSub")} data-testid="home-stat-components" />
    </div>
  );
}

/** What each kind of attention row looks like: the accent and icon say "stopped", "asking", "blocked" before the words are read. */
const TONE_OF: Record<NeedsYouRow["kind"], "red" | "amber" | "blue"> = { blocked: "red", stopped: "red", question: "amber", proposal: "amber", gate: "amber", triage: "blue" };
const ICON_OF: Record<NeedsYouRow["kind"], string> = { blocked: "Warning", stopped: "Warning", question: "Notification", proposal: "Check", gate: "Warning", triage: "Bug" };

/** Titles come from agents and loops and can be any length; the feed shows at most this many characters of one. */
export const MAX_TITLE_CHARS = 256;
export const clip = (text: string | undefined, max = MAX_TITLE_CHARS): string => {
  const t = (text ?? "").trim();
  return t.length <= max ? t : `${t.slice(0, max - 1).trimEnd()}…`;
};

export function NeedsYouFeed({ rows, components }: { rows: readonly NeedsYouRow[]; components: readonly WorkComponent[] }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  return (
    <section className="min-w-0 space-y-3" aria-label={t("home.needs.title")} data-testid="home-gates">
      <h2 className="text-xl font-semibold">
        {t("home.needs.title")} <span className="text-sm font-normal text-slate-500">{rows.length}</span>
        <span className="ml-3 text-xs font-normal text-slate-500">{t("home.needs.oldest")}</span>
      </h2>
      {rows.length === 0 ? (
        <EmptyState title={t("home.needs.empty")} fullWidth size="xs" />
      ) : (
        <div className="min-w-0 max-w-full space-y-2">
          {rows.map((row) => (
            // A title is whatever an agent or a loop was called, so it is cut at MAX_TITLE_CHARS and may wrap or clip, never widen the row.
            <ActionPanel
              key={row.key}
              data-testid="home-gate-banner"
              className="min-w-0 max-w-full"
              size="sm"
              tone={TONE_OF[row.kind]}
              icon={ICON_OF[row.kind]}
              title={<span className="line-clamp-2 [overflow-wrap:anywhere]" title={row.title}>{clip(row.title)}</span>}
              subtitle={<span className="block truncate" title={row.detail}>{t(`home.needs.${row.kind}`)}{row.componentRoot ? ` · ${rootLabel(components, row.componentRoot)}` : ""}{row.detail ? ` · ${clip(row.detail, 120)}` : ""}</span>}
              actions={row.to ? [
                <Button key="act" size="sm" color={row.kind === "question" ? "amber" : undefined} onClick={() => navigate(row.to!)}>
                  {t(`home.needs.act.${row.kind}`)}
                </Button>,
              ] : undefined}
            />
          ))}
        </div>
      )}
    </section>
  );
}

/** One card per top-level project: how many components, how many loops run, how many need a person. */
export function HomeProjects({ loops, components }: { loops: readonly WorkLoop[]; components: readonly WorkComponent[] }) {
  const { t } = useI18n();
  const tree = useMemo(() => buildTree(components), [components]);
  const counts = useMemo(() => countByRoot(loops), [loops]);
  return (
    <section className="min-w-0 space-y-3" aria-label={t("home.projects.title")} data-testid="home-projects">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">{t("home.projects.title")}</h2>
        <Link to={workPath("")} className="aeon-accent-text text-sm underline">{t("home.projects.openWork")}</Link>
      </div>
      {tree.length === 0 ? (
        <EmptyState title={t("home.projects.empty")} fullWidth size="xs" />
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {tree.map((node) => {
            const sum = rollup(node, counts);
            const inside = node.children.length;
            return (
              <Link key={node.component.id} to={workPath(node.component.id)} className="block min-w-0 rounded-xl border border-slate-200 p-3 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-800/60" data-testid={`home-project-${node.component.id}`}>
                <p className="truncate font-semibold">{node.component.name}</p>
                <p className="truncate text-xs text-slate-500 dark:text-slate-400">
                  {inside > 0 ? `${t("home.projects.components", { count: inside })} · ` : ""}
                  {sum.running + sum.needsYou === 0 ? t("home.projects.idle") : t("home.projects.activity", { running: sum.running, needs: sum.needsYou })}
                </p>
              </Link>
            );
          })}
        </div>
      )}
    </section>
  );
}
