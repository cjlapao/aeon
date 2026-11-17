import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Pill, Table, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { RelativeTime } from "../../../components/RelativeTime";
import { notifications } from "../../../lib/notifications";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import type { LiveController, LiveState } from "../../../lib/live";
import { useEnvironmentProvisions, writeEnvironmentTeardown } from "./environmentsRpc";
import { lastErrorOf, listOf, removedAtOf, type ProvisionRow } from "./environmentsModel";

/**
 * The Provisions tab (S59 T11, EV12): the ledger the driver keeps — state,
 * loop, backend, handle, owner, the ages of each transition, and the driver's
 * own sentence when a removal failed.
 *
 * Tear down is the human door off a row that will not settle. The panel does
 * NOT decide which rows look stuck: comparing a `state` word to a string typed
 * here is exactly the invented status this slice forbids, so the control is
 * offered on every row and `environment.teardown` settles it — the door walks
 * the ledger (`provisioning → ready → tearing_down → removed/failed`) and
 * refuses to mark an unknown provision removed, verbatim. The confirm names the
 * row and the row shown afterwards is the ANSWER, not an edit of the draft.
 */
export interface ProvisionsTabProps {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

const STATE_TONES = ["emerald", "sky", "amber", "slate", "rose"] as const;
const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function ProvisionsTab({ controller, live, canWrite }: ProvisionsTabProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();
  const [openId, setOpenId] = useState("");

  // The state filter is deliberately absent: the closed set of states is the daemon's and
  // `environment.vocabulary` does not publish it, so the tab reads the whole ledger rather than
  // typing a state word to narrow it.
  const ledger = useEnvironmentProvisions({ state: "", limit: 200 }, controller, live);
  const rows = listOf(ledger.data);
  const tones = rows.map((row) => row.state);
  const opened = rows.find((row) => row.id === openId);
  const toneOf = (state: string) => {
    const at = tones.indexOf(state);
    return at < 0 ? "slate" : STATE_TONES[at % STATE_TONES.length] ?? "slate";
  };

  const tear = async (row: ProvisionRow) => {
    setOpenId("");
    const confirmed = await confirm({
      title: t("settings.environments.provision.teardownTitle"),
      description: t("settings.environments.provision.teardownBody", { id: row.id }),
      confirmValue: row.id,
      confirmValueLabel: t("settings.environments.provision.confirmLabel"),
      confirmLabel: t("settings.environments.provision.teardownConfirm"),
    });
    if (!confirmed) return; // the human door stays shut: nothing was sent
    const result = await action.run(() => writeEnvironmentTeardown(row.id), { errorTitle: t("settings.environments.provision.teardownFailed") });
    if (result) {
      // The row is what the daemon answered with.
      notifications.push({
        title: t("settings.environments.provision.tornTitle", { id: result.id }),
        body: `${result.state}${removedAtOf(result) === "" ? "" : ` · ${removedAtOf(result)}`}`,
        tone: "success",
        source: "local",
      });
      ledger.refetch();
    }
  };

  const columns: TableColumn<ProvisionRow>[] = [
    {
      id: "id",
      groupable: false,
      header: t("settings.environments.col.provision"),
      render: (row) => (
        <div className="w-0 min-w-full py-1" data-testid={`provision-row-${row.id}`}>
          <code className="block truncate text-xs font-semibold">{row.id}</code>
          <span className="block truncate text-xs opacity-70" data-testid={`provision-loop-${row.id}`}>{row.loopSlug === "" ? t("settings.environments.provision.noLoop") : row.loopSlug}</span>
        </div>
      ),
    },
    { id: "state", groupable: false, header: t("settings.environments.col.state"), width: 140, render: (row) => <Pill size="sm" tone={toneOf(row.state)} data-testid={`provision-state-${row.id}`}>{row.state}</Pill> },
    { id: "backend", groupable: false, header: t("settings.environments.col.backend"), width: 130, render: (row) => <Pill size="sm" tone="slate">{row.backend}</Pill> },
    { id: "handle", groupable: false, header: t("settings.environments.col.handle"), width: 200, render: (row) => <span className="block truncate text-xs opacity-80" data-testid={`provision-handle-${row.id}`}>{row.handle === "" ? t("settings.environments.provision.noHandle") : row.handle}</span> },
    { id: "created", groupable: false, header: t("settings.environments.col.created"), width: 140, render: (row) => <span className="text-xs opacity-70" data-testid={`provision-created-${row.id}`}><RelativeTime iso={row.createdAt} /></span> },
  ];

  return (
    <div className="flex min-h-[22rem] flex-1 flex-col gap-3" data-testid="environments-provision-list">
      {ledger.error && (
        <Alert className="shrink-0" intent="danger" title={t("settings.environments.provision.listFailed")} description={ledger.error} data-testid="environments-provisions-failed">
          <Button size="sm" onClick={ledger.refetch}>{t("settings.environments.actions.retry")}</Button>
        </Alert>
      )}

      <div className="min-h-[18rem] flex-1">
        <Table<ProvisionRow>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(row) => row.id}
          corner="rounded-sm"
          loading={ledger.loading}
          onRowClick={(row) => setOpenId(row.id)}
          emptyState={ledger.error ? <span /> : <div data-testid="environments-provisions-empty"><EmptyState title={t("settings.environments.provision.emptyTitle")} subtitle={t("settings.environments.provision.emptyBody")} /></div>}
          fullHeight
        />
      </div>

      {opened && (
        <OverlaySidePanel title={opened.id} titleClassName="text-xl font-semibold" variant="simple" width={520} onClose={() => setOpenId("")}>
          <div className="space-y-8">
            {canWrite && (
              <div>
                <Button size="sm" disabled={action.busy} onClick={() => void tear(opened)} data-testid={`provision-teardown-${opened.id}`}>{t("settings.environments.provision.teardown")}</Button>
              </div>
            )}
            <section className="space-y-3">
              <h3 className={H}>{t("settings.environments.q.history")}</h3>
              <dl className="space-y-1 text-sm">
                <div><dt className="inline opacity-70">{t("settings.environments.provision.root")}: </dt><dd className="inline break-all font-mono text-xs">{opened.componentRoot}</dd></div>
                <div><dt className="inline opacity-70">{t("settings.environments.provision.owner")}: </dt><dd className="inline">{opened.owner}</dd></div>
                <div><dt className="inline opacity-70">{t("settings.environments.provision.created")}: </dt><dd className="inline"><RelativeTime iso={opened.createdAt} /></dd></div>
                <div data-testid={`provision-ready-${opened.id}`}><dt className="inline opacity-70">{t("settings.environments.provision.ready")}: </dt><dd className="inline">{opened.readyAt === "" ? "—" : <RelativeTime iso={opened.readyAt} />}</dd></div>
                {removedAtOf(opened) !== "" && <div data-testid={`provision-removed-${opened.id}`}><dt className="inline opacity-70">{t("settings.environments.provision.removed")}: </dt><dd className="inline"><RelativeTime iso={removedAtOf(opened)} /></dd></div>}
              </dl>
              {lastErrorOf(opened) !== "" && <p className="whitespace-pre-line text-xs italic" data-testid={`provision-error-${opened.id}`}>{lastErrorOf(opened)}</p>}
            </section>
          </div>
        </OverlaySidePanel>
      )}

      {modal}
    </div>
  );
}
