import { useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ConfirmModal, SearchBar, Tabs, type TableSortState } from "../../components/kit";
import { useAppToast } from "../../lib/useAppToast";
import { rpcTyped, toRpcError } from "../../lib/bridge";
import { useTypeToConfirm } from "../../lib/useConfirm";
import type { LiveController, LiveState } from "../../lib/live";
import { AGENT_DEFAULT_DIR, AGENT_DEFAULT_SORT, AGENT_SORTS, type AgentDir, type AgentSort } from "./agentsModel";
import { useAgentRows } from "./agentsRows";
import { AgentPanel, type AgentPanelMode } from "./AgentPanel";
import { AgentsTable, type RowVerb } from "./AgentsTable";
import { AgentsActivity } from "./AgentsActivity";
import { AgentDetail } from "./AgentDetail";
import { AgentDuplicatePanel, AgentExportPanel, AgentMigrateModal } from "./AgentVerbs";

// `/config/agents` — the agents, as a list and as a page per agent.
//
// The LIST answers "which one?": a search, a table that loads the daemon's pages as it is
// scrolled, and the three things you do to the collection (New, Import, Migrate). The
// DETAIL answers "what is it, and change it": a page of its own, opened read-only, with
// Edit. Which of the two shows is the address — `?agent=<slug>` — so a refresh, the back
// button and a pasted link land where they were, and the search term rides the same
// address (`?q=`).
//
// The screen owns the SURFACE open on top (create/import in the side panel; duplicate,
// export, migrate) and the question about an unsaved draft; it owns no answer — every
// write ends by asking the daemon again. The frame stands down (`frame: "bare"`), so this
// component draws the page's own title.
//
// Delete is the typed confirmation (`useTypeToConfirm`, confirmValue the row's slug); when
// `agent.delete` refuses it names every blueprint, policy and MCP server still pointing at
// the slug (AM4), and that answer prints whole.

export type HeaderVerb = "create" | "import" | "migrate";
export type AgentVerb = HeaderVerb | RowVerb;

type Surface =
  | { kind: "panel"; mode: AgentPanelMode; slug: string }
  | { kind: "duplicate"; slug: string }
  | { kind: "export"; slug: string }
  | { kind: "migrate" };

export interface AgentsScreenProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

export function AgentsScreen({ controller, live, canWrite }: AgentsScreenProps) {
  const { t } = useI18n();
  const [params, setParams] = useSearchParams();
  const slug = params.get("agent") ?? "";
  const q = (params.get("q") ?? "").trim();
  // The list has two views: the agents, and the activity across them (the whole ledger).
  const view = params.get("view") === "activity" ? "activity" : "agents";
  const [sort, setSort] = useState<{ column: AgentSort; dir: AgentDir }>({ column: AGENT_DEFAULT_SORT, dir: AGENT_DEFAULT_DIR });
  const [surface, setSurface] = useState<Surface | undefined>(undefined);
  const notify = useAppToast();
  const [discardAsk, setDiscardAsk] = useState<(() => void) | undefined>(undefined);
  const [avatarEpoch, setAvatarEpoch] = useState(0);
  const { confirm, modal } = useTypeToConfirm();

  // The surface that holds a draft knows it; the screen is what would throw it away.
  const dirtyRef = useRef(false);
  const onDirtyChange = (dirty: boolean) => {
    dirtyRef.current = dirty;
  };

  const rows = useAgentRows(live, q, sort.column, sort.dir);
  const reason = canWrite ? undefined : t("agents.reason.notConnected");

  const askSearch = (term: string) =>
    setParams((p) => {
      const next = new URLSearchParams(p);
      if (term.trim() === "") next.delete("q");
      else next.set("q", term.trim());
      return next;
    }, { replace: true });
  const askView = (next: string) =>
    setParams((p) => {
      const out = new URLSearchParams(p);
      if (next === "activity") out.set("view", "activity");
      else out.delete("view");
      return out;
    }, { replace: true });
  const askSort = (next: TableSortState | null) => {
    // The kit clears its own sort with `null`; the daemon has no "no order", so a cleared
    // header asks for the order it would pick anyway. A column it does not resolve keeps
    // the order the person was already looking at.
    if (next === null) return setSort({ column: AGENT_DEFAULT_SORT, dir: AGENT_DEFAULT_DIR });
    const column = (AGENT_SORTS as readonly string[]).includes(next.columnId) ? (next.columnId as AgentSort) : sort.column;
    setSort({ column, dir: next.direction });
  };

  /** A write landed, in any surface: the list asks again. Never a local patch. */
  const onChanged = () => {
    rows.reload();
    setAvatarEpoch((n) => n + 1);
  };

  const discard = () => {
    const go = discardAsk;
    setDiscardAsk(undefined);
    go?.();
  };
  /** Run `go` now, or after the person agrees to drop an unsaved draft. */
  const confirmLeave = (go: () => void) => {
    if (!dirtyRef.current) return go();
    setDiscardAsk(() => () => {
      dirtyRef.current = false;
      go();
    });
  };
  const close = (next?: Surface) => confirmLeave(() => setSurface(next));

  const openAgent = (target: string, editing = false) =>
    setParams((p) => {
      const next = new URLSearchParams(p);
      next.delete("edit");
      if (target === "") next.delete("agent");
      else next.set("agent", target);
      if (editing) next.set("edit", "1");
      return next;
    });

  const askDelete = async (target: string) => {
    const agreed = await confirm({
      title: t("agents.delete.title", { slug: target }),
      description: t("agents.delete.body", { slug: target }),
      confirmValue: target,
      confirmValueLabel: t("agents.col.slug"),
      confirmLabel: t("agents.delete.confirm"),
    });
    if (!agreed) return;
    try {
      const answer = await rpcTyped("agent.delete", { slug: target });
      notify.success(t("agents.delete.done", { slug: answer.deleted }));
      onChanged();
      if (slug === target) openAgent("");
    } catch (cause) {
      // The daemon's refusal names every blueprint, policy and MCP server still pointing at the slug.
      notify.error(toRpcError(cause));
    }
  };

  /** One place a verb becomes a surface, so the header, a row and the detail page cannot drift. */
  const openVerb = (verb: AgentVerb, target: string) => {
    switch (verb) {
      case "create":
        return close({ kind: "panel", mode: "create", slug: "" });
      case "import":
        return close({ kind: "panel", mode: "import", slug: "" });
      case "migrate":
        return close({ kind: "migrate" });
      case "view":
        return confirmLeave(() => openAgent(target));
      case "edit":
        return confirmLeave(() => openAgent(target, true));
      case "duplicate":
        return close({ kind: "duplicate", slug: target });
      case "export":
        return close({ kind: "export", slug: target });
      case "delete":
        return void askDelete(target);
    }
  };

  const failed = rows.error !== undefined;
  const unsigned = rows.rows.some((r) => !r.signed);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4" data-testid="agents-screen">
      {slug !== "" ? (
        <AgentDetail
          key={`${slug}|${params.get("edit") ?? ""}`}
          slug={slug}
          startEditing={params.get("edit") === "1"}
          controller={controller}
          live={live}
          canWrite={canWrite}
          onBack={() => openAgent("")}
          confirmLeave={confirmLeave}
          onDirtyChange={onDirtyChange}
          onChanged={onChanged}
          onVerb={(verb) => openVerb(verb, slug)}
        />
      ) : (
        <>
          <header className="flex shrink-0 flex-wrap items-end justify-between gap-4" data-testid="agents-header">
            <div>
              <h1 className="text-3xl font-semibold">{t("agents.title")}</h1>
              <p className="mt-1 text-sm opacity-70">{t("agents.purpose")}</p>
            </div>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" disabled={!canWrite} tooltip={reason} onClick={() => openVerb("import", "")} data-testid="agents-import">{t("agents.action.import")}</Button>
              <Button size="sm" variant="ghost" disabled={!canWrite} tooltip={reason} onClick={() => openVerb("migrate", "")} data-testid="agents-migrate">{t("agents.action.migrate")}</Button>
              <Button size="sm" disabled={!canWrite} tooltip={reason} onClick={() => openVerb("create", "")} data-testid="agents-create">{t("agents.action.create")}</Button>
            </div>
          </header>

          <div className="shrink-0">
            <Tabs
              variant="underline"
              items={[{ id: "agents", label: t("agents.view.agents") }, { id: "activity", label: t("agents.view.activity") }]}
              value={view}
              onChange={askView}
            />
          </div>

          {view === "activity" ? (
            <AgentsActivity live={live} onOpenAgent={(target) => confirmLeave(() => openAgent(target))} />
          ) : (
            <>
          <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="agents-action-bar">
            {/* `SearchBar`'s input carries only a placeholder, so a real label names it. */}
            <label className="block w-full max-w-md" data-testid="agents-search">
              <span className="sr-only">{t("agents.search.label")}</span>
              <SearchBar placeholder={t("agents.search.placeholder")} debounceMs={300} loading={rows.loading} initialValue={q} onSearch={askSearch} onClear={() => askSearch("")} />
            </label>
            {unsigned && <span className="ml-auto flex items-center gap-2 text-sm opacity-70" data-testid="agents-unsigned-note"><span className="h-2 w-2 rounded-full bg-amber-400" aria-hidden />{t("agents.unsignedNote")}</span>}
          </div>

          {!canWrite && <Alert className="shrink-0" intent="warning" title={t("agents.reason.notConnectedTitle")} description={reason} data-testid="agents-not-current" />}

          {failed && (
            <div className="shrink-0 space-y-2" data-testid="agents-read-error">
              <Alert intent="danger" title={t("agents.read.failed")} description={rows.error} />
              <Button size="sm" variant="ghost" onClick={rows.error && rows.rows.length > 0 ? rows.loadMore : rows.reload} data-testid="agents-retry">{t("agents.action.retry")}</Button>
            </div>
          )}


          <AgentsTable
            rows={rows.rows}
            total={rows.total}
            q={q}
            loading={rows.loading}
            hasMore={rows.hasMore}
            onLoadMore={rows.loadMore}
            resetKey={rows.resetKey}
            sort={sort.column}
            dir={sort.dir}
            busy={false}
            canWrite={canWrite}
            reason={reason}
            avatarEpoch={avatarEpoch}
            onSort={askSort}
            onVerb={(verb, row) => openVerb(verb, row.slug)}
          />
            </>
          )}
        </>
      )}

      {surface?.kind === "panel" && (
        <AgentPanel key={`${surface.mode}|${surface.slug}`} mode={surface.mode} slug={surface.slug} controller={controller} live={live} canWrite={canWrite}
          onClose={() => close()} onChanged={onChanged} onDirtyChange={onDirtyChange}
          onOpenAgent={(target) => { close(); openAgent(target); }} />
      )}
      {surface?.kind === "duplicate" && <AgentDuplicatePanel slug={surface.slug} canWrite={canWrite} onClose={() => close()} onChanged={onChanged} onOpenAgent={(target) => { close(); openAgent(target); }} />}
      {surface?.kind === "export" && <AgentExportPanel slug={surface.slug} canWrite={canWrite} onClose={() => close()} onChanged={onChanged} />}
      {surface?.kind === "migrate" && <AgentMigrateModal canWrite={canWrite} onClose={() => close()} onChanged={onChanged} />}

      {modal}

      {/* The dirty-draft question: about unsaved text, so a plain confirm — the typed one belongs to the delete. */}
      {discardAsk !== undefined && (
        <ConfirmModal isOpen title={t("agents.discard.title")} description={t("agents.discard.body")} confirmLabel={t("agents.discard.confirm")}
          cancelLabel={t("agents.discard.cancel")} onClose={() => setDiscardAsk(undefined)} onConfirm={discard} data-testid="agents-discard-ask" />
      )}
    </div>
  );
}
