import { useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, Pill, Table, type TableColumn } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { OperationConfirm, ReparseConfirm } from "./OperationConfirm";
import { deleteGraphRoot, reparseGraphFile, startGraphBuild } from "./systemRpc";
import { blockFlying, fmtInt, rootsOf, startedWordsOf, stateTone, type StatusView } from "./systemModel";

/**
 * GraphMaintain — the code-graph maintenance panel (S54 T6): which roots are indexed, is a pass running,
 * and the three acts of the graph family's human doors. Each confirm NAMES the root and says what the
 * act does to DERIVED rows (build regenerates them, reparse replaces one file's, delete removes the
 * root's entire graph — a later build regenerates them; source files are never touched by any of the
 * three). The controls stand down while the daemon's own state word says a graph pass is running, and an
 * act that meets a flying pass hears the daemon's starter-naming answer as a toast. A refusal is a toast
 * too — no panel stays behind.
 *
 * The confirms are drawn on the page body, not inside the side panel: the panel is its own stacking
 * context and a modal drawn inside it would sit under it.
 */
type PendingAct = { kind: "build" | "delete" | "reparse"; root: string } | null;
type Root = NonNullable<StatusView["graph"]["roots"]>[number];

export interface GraphMaintainProps {
  block: StatusView["graph"] | undefined;
  canWrite: boolean;
  onChanged: () => void;
}

export function GraphMaintain({ block, canWrite, onChanged }: GraphMaintainProps) {
  const { t } = useI18n();
  const [pending, setPending] = useState<PendingAct>(null);
  const [busy, setBusy] = useState(false);
  const flying = blockFlying(block?.state);
  const held = !canWrite || flying;

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      const starter = startedWordsOf(await fn());
      // A flying build answers with its starter's words (GraphBuildAccepted naming), verbatim.
      if (starter !== undefined) {
        notifications.push({ title: t("settings.system.graph.alreadyFlying"), body: t("settings.system.startedBy", { by: starter.by, at: starter.at }), tone: "info", source: "local" });
      }
      setPending(null);
      onChanged();
    } catch (cause) {
      notifications.push({ title: t("settings.system.graph.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  };

  const button = (kind: "build" | "reparse" | "delete", root: string) => (
    <Button
      size="sm"
      variant="ghost"
      disabled={held}
      title={held ? t("settings.system.disabled") : undefined}
      onClick={() => setPending({ kind, root })}
      data-testid={`graph-${kind}-${root}`}
    >
      {t(`settings.system.actions.${kind}`)}
    </Button>
  );

  const columns: TableColumn<Root>[] = [
    {
      id: "root",
      groupable: false,
      header: t("settings.system.graph.root"),
      render: (row) => <code className="block break-all py-1 text-xs" data-testid={`graph-root-${row.root}`}>{row.root}</code>,
    },
    {
      id: "counts",
      groupable: false,
      header: t("settings.system.graph.counts"),
      width: 190,
      render: (row) =>
        row.neverBuilt === true ? (
          <span data-testid={`graph-never-built-${row.root}`}><Pill size="sm" tone="slate">{t("settings.system.graph.neverBuilt")}</Pill></span>
        ) : (
          <span className="text-xs opacity-80">{t("settings.system.graph.countsLine", { files: fmtInt(row.files), nodes: fmtInt(row.nodes), edges: fmtInt(row.edges) })}</span>
        ),
    },
    {
      id: "acts",
      groupable: false,
      header: "",
      width: 250,
      render: (row) => (
        <span className="flex justify-end gap-1">
          {button("build", row.root)}
          {row.neverBuilt === true ? null : button("reparse", row.root)}
          {row.neverBuilt === true ? null : button("delete", row.root)}
        </span>
      ),
    },
  ];

  const h = "text-xs font-semibold uppercase tracking-widest opacity-70";
  return (
    <div className="space-y-6 pb-6" data-testid="graph-maintain">
      <section className="space-y-2">
        <h3 className={h}>{t("settings.system.graph.q.roots")}</h3>
        <div className="min-h-[12rem]">
          <Table<Root> variant="outlined" data={rootsOf(block)} columns={columns} rowKey={(row) => row.root} corner="rounded-sm" fullHeight data-testid="graph-roots" />
        </div>
      </section>

      <section className="space-y-2">
        <h3 className={h}>{t("settings.system.graph.q.running")}</h3>
        <p className="flex items-center gap-2 text-sm">
          <Pill tone={stateTone(block?.state ?? "")}>{block?.state ?? ""}</Pill>
          <span className="opacity-80">{flying ? t("settings.system.graph.runningNow") : t("settings.system.graph.idleNow")}</span>
        </p>
      </section>

      <section className="space-y-2">
        <h3 className={h}>{t("settings.system.graph.q.acts")}</h3>
        <p className="text-sm opacity-80">{t("settings.system.graph.actsBody")}</p>
      </section>

      {createPortal(
        pending?.kind === "reparse" ? (
          <ReparseConfirm open root={pending.root} busy={busy} onSubmit={(path) => void act(() => reparseGraphFile(pending.root, path))} onCancel={() => setPending(null)} />
        ) : (
          <OperationConfirm
            open={pending !== null}
            title={pending?.kind === "delete" ? t("settings.system.graph.delete.title", { root: pending.root }) : t("settings.system.graph.build.title", { root: pending?.root ?? "" })}
            confirmLabel={pending?.kind === "delete" ? t("settings.system.actions.confirmDelete") : t("settings.system.actions.confirmBuild")}
            busy={busy}
            onConfirm={() => {
              if (pending?.kind === "delete") void act(() => deleteGraphRoot(pending.root));
              else if (pending?.kind === "build") void act(() => startGraphBuild(pending.root));
            }}
            onCancel={() => setPending(null)}
          >
            <p className="text-sm text-slate-600 dark:text-slate-400" data-testid="operation-statement">
              {pending?.kind === "delete" ? t("settings.system.graph.delete.statement") : t("settings.system.graph.build.statement")}
            </p>
          </OperationConfirm>
        ),
        document.body,
      )}
    </div>
  );
}
