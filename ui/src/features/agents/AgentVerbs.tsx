import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Modal } from "../../components/kit";
import { CodeBlock } from "../../components/CodeBlock";
import { OverlaySidePanel } from "../../components/OverlaySidePanel";
import { rpcTyped, toRpcError } from "../../lib/bridge";
import { useAppToast } from "../../lib/useAppToast";
import { migrateCreated } from "./agentsModel";

// ---------------------------------------------------------------------------
// The three verbs that are not the four-mode panel (CU4): a row's Duplicate and
// Export, and the header's Migrate. They are about an ACT with one field, not a
// ROW — bolting them onto the panel would mean a mode whose tabs do not apply
// to it.
// ---------------------------------------------------------------------------

/** What a one-field verb needs from the screen: who may write, and what to re-read. */
export interface VerbProps {
  slug: string;
  canWrite: boolean;
  onClose: () => void;
  /** A write landed: the screen re-reads the table. */
  onChanged: () => void;
  /** Open an agent's page (the duplicate's new copy). */
  onOpenAgent?: (slug: string) => void;
}

/** The panel behind a row's `Duplicate`: one name in, one new agent out, and its page opens. */
export function AgentDuplicatePanel({ slug, canWrite, onClose, onChanged, onOpenAgent }: VerbProps) {
  const { t } = useI18n();
  const notify = useAppToast();
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const run = async () => {
    if (!canWrite || name.trim() === "") return;
    setBusy(true);
    try {
      // Both keys, always: the copy needs a slug of its own (AM2), and a slug
      // comes from a name.
      const answer = await rpcTyped("agent.duplicate", { slug, name: name.trim() });
      notify.success(t("agents.duplicate.answer", { slug: answer.slug, name: answer.name }));
      onChanged();
      onOpenAgent?.(answer.slug);
    } catch (cause) {
      // The daemon's refusal, whole, in a toast that stays until it is dismissed.
      notify.error(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  };

  return (
    <OverlaySidePanel
      onClose={onClose}
      title={t("agents.duplicate.title")}
      titleClassName="text-xl font-semibold"
      subtitle={t("agents.duplicate.subtitle", { slug })}
      variant="simple"
      width={480}
      resizable={false}
    >
      <div className="space-y-6" data-testid="agent-duplicate-panel">
        {!canWrite && (
          <Alert
            intent="warning"
            title={t("agents.reason.notConnectedTitle")}
            description={t("agents.reason.notConnected")}
          />
        )}
        <section className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.duplicate.step1")}</h3>
          <p className="text-sm opacity-80" data-testid="agent-duplicate-note">{t("agents.duplicate.note")}</p>
          <FormField label={t("agents.duplicate.name")} hint={t("agents.duplicate.nameHint")} required labelFor="agent-duplicate-name">
            <Input
              id="agent-duplicate-name"
              variant="ghost"
              size="md"
              value={name}
              disabled={!canWrite || busy}
              onChange={(event) => setName(event.target.value)}
              data-testid="agent-duplicate-name"
            />
          </FormField>
        </section>
        <footer className="flex justify-end gap-2 border-t border-white/10 pt-4">
          <Button variant="ghost" onClick={onClose} data-testid="agent-duplicate-cancel">{t("agents.create.cancel")}</Button>
          <Button onClick={() => void run()} disabled={!canWrite || busy || name.trim() === ""} data-testid="agent-duplicate-run">
            {busy ? t("agents.panel.saving") : t("agents.row.duplicate")}
          </Button>
        </footer>
      </div>
    </OverlaySidePanel>
  );
}

/** The panel behind a row's `Export`: choose where the card goes, and see it. */
export function AgentExportPanel({ slug, canWrite, onClose, onChanged }: VerbProps) {
  const { t } = useI18n();
  const notify = useAppToast();
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [card, setCard] = useState<string | undefined>(undefined);

  const given = path.trim() !== "";

  const run = async () => {
    if (!canWrite) return;
    setBusy(true);
    try {
      const answer = await rpcTyped("agent.export", { slug, path: path.trim() });
      setCard(answer);
      notify.success(t(given ? "agents.export.doneFile" : "agents.export.doneCard"));
      // An export writes a file, not a row (AR4 mints no ledger row for it);
      // the re-read is asked because it is cheap and honest, not because a
      // change is claimed.
      onChanged();
    } catch (cause) {
      notify.error(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  };
  const copy = () => {
    if (card === undefined) return;
    void navigator.clipboard?.writeText(card).then(() => notify.success(t("agents.export.copied")), () => undefined);
  };

  return (
    <OverlaySidePanel
      onClose={onClose}
      title={t("agents.export.title")}
      titleClassName="text-xl font-semibold"
      subtitle={t("agents.export.subtitle", { slug })}
      variant="simple"
      width={520}
      resizable={false}
    >
      <div className="space-y-6" data-testid="agent-export-panel">
        {!canWrite && (
          <Alert
            intent="warning"
            title={t("agents.reason.notConnectedTitle")}
            description={t("agents.reason.notConnected")}
          />
        )}
        <section className="space-y-2">
          <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.export.step1")}</h3>
          <p className="text-sm opacity-80">{t("agents.export.help")}</p>
          <FormField label={t("agents.export.path")} hint={t("agents.export.pathHint")} labelFor="agent-export-path">
            <Input
              id="agent-export-path"
              variant="ghost"
              size="md"
              value={path}
              disabled={busy}
              placeholder="/home/operator/aeon-packages/card.json"
              onChange={(event) => setPath(event.target.value)}
              data-testid="agent-export-path"
            />
          </FormField>
        </section>
        <footer className="flex justify-end gap-2 border-t border-white/10 pt-4">
          <Button variant="ghost" onClick={onClose} data-testid="agent-export-cancel">{t("agents.create.cancel")}</Button>
          <Button onClick={() => void run()} disabled={!canWrite || busy} data-testid="agent-export-run">
            {busy ? t("agents.panel.saving") : t("agents.row.export")}
          </Button>
        </footer>
        {card !== undefined && (
          <section className="space-y-2" data-testid="agent-export-answer">
            {/* Two answers, told apart by the field the person typed — an empty
                path asks the daemon for the card itself, not a write. */}
            <p className="text-sm font-medium" data-testid="agent-export-answer-line">
              {t(given ? "agents.export.answer.path" : "agents.export.answer.card")}
            </p>
            <CodeBlock code={card} language={given ? "plain" : "json"} data-testid="agent-export-answer-text" />
            {!given && <Button size="sm" variant="ghost" onClick={copy} data-testid="agent-export-copy">{t("agents.export.copy")}</Button>}
          </section>
        )}
      </div>
    </OverlaySidePanel>
  );
}

/**
 * The header's `Migrate`, in a modal: the daemon derives agent rows from the
 * plugin lifecycle table and answers how many it created. The confirm is
 * deliberate and not a typed one — a migrate creates rows, refuses rather than
 * overwrites, and its ledger entry names no single slug (AR1), which is said
 * before the button, not after. The answer is a toast, and the modal closes.
 */
export function AgentMigrateModal({ canWrite, onClose, onChanged }: Omit<VerbProps, "slug">) {
  const { t } = useI18n();
  const notify = useAppToast();
  const [busy, setBusy] = useState(false);

  const run = async () => {
    if (!canWrite) return;
    setBusy(true);
    try {
      const answer = await rpcTyped("agent.migrate", {});
      const n = migrateCreated(answer);
      if (n !== null) notify.success(t("agents.migrate.answer", { created: n }));
      onChanged();
      onClose();
    } catch (cause) {
      notify.error(toRpcError(cause));
      setBusy(false);
    }
  };

  return (
    <Modal
      isOpen
      onClose={onClose}
      title={t("agents.migrate.title")}
      description={t("agents.migrate.body")}
      data-testid="agent-migrate-modal"
      actions={
        <div className="flex items-center gap-2">
          <Button variant="ghost" onClick={onClose} data-testid="agent-migrate-cancel">
            {t("agents.migrate.cancel")}
          </Button>
          <Button onClick={() => void run()} disabled={!canWrite || busy} data-testid="agent-migrate-run">
            {busy ? t("agents.panel.saving") : t("agents.migrate.confirm")}
          </Button>
        </div>
      }
    >
      <div className="space-y-3" data-testid="agent-migrate-body">
        <p className="text-sm opacity-80" data-testid="agent-migrate-note">{t("agents.migrate.ledgerNote")}</p>
      </div>
    </Modal>
  );
}
