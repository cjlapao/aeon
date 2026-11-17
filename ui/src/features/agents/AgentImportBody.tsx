import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ConfirmModal, FileUpload, InfoRow, Panel, Pill } from "../../components/kit";
import type { RpcMethods } from "../../proto/contract";
import { AgentDetail } from "./AgentFields";
import { importWordTone } from "./agentsModel";
import { usePanelWrites, type PanelWrites } from "./usePanelWrites";

// The import panel, as three steps a person can follow without knowing what a card is:
// choose the file, check what is in it, import it. The file is read HERE and its contents go
// to the daemon (`agent.import` with `content`), so the daemon never has to be handed a path on
// its own machine. What the daemon decides — the signature check, the unsigned-import refusal,
// created / updated / already stored — is the daemon's, printed as it answered.

/** A card is a few kilobytes of JSON; a megabyte is already far too much. */
const MAX_CARD_BYTES = 1024 * 1024;

/** What the window can say about a card before the daemon has judged it. */
interface CardRead {
  text: string;
  name: string;
  slug: string;
  version: string;
  /** The card carries a signature. Whether it VERIFIES is the daemon's answer, not ours. */
  hasSignature: boolean;
}

type Read = { ok: true; card: CardRead } | { ok: false; problem: "notJson" | "notCard" };

/** Read a dropped file's text; jsdom and older webviews have no `Blob.text`, so fall back. */
function readText(file: File): Promise<string> {
  if (typeof file.text === "function") return file.text();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(reader.error);
    reader.readAsText(file);
  });
}

function readCard(text: string): Read {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { ok: false, problem: "notJson" };
  }
  const envelope = parsed as { card?: unknown; signature?: unknown } | null;
  const body = envelope !== null && typeof envelope === "object" ? envelope.card : undefined;
  if (body === null || typeof body !== "object") return { ok: false, problem: "notCard" };
  const card = body as { name?: unknown; slug?: unknown; version?: unknown };
  const word = (value: unknown) => (typeof value === "string" || typeof value === "number" ? String(value) : "");
  return {
    ok: true,
    card: {
      text,
      name: word(card.name),
      slug: word(card.slug),
      version: word(card.version),
      hasSignature: typeof envelope?.signature === "string" && envelope.signature !== "",
    },
  };
}

export interface AgentImportBodyProps {
  canWrite: boolean;
  onChanged: () => void;
  /** Open the agent that was just added. */
  onOpenAgent?: (slug: string) => void;
}

export function AgentImportBody({ canWrite, onChanged, onOpenAgent }: AgentImportBodyProps) {
  const { t } = useI18n();
  const [files, setFiles] = useState<File[]>([]);
  const [read, setRead] = useState<Read | undefined>(undefined);
  const [problem, setProblem] = useState<string | undefined>(undefined);
  // The person has to say yes to an unsigned card before it goes anywhere.
  const [confirming, setConfirming] = useState(false);
  const [imported, setImported] = useState<RpcMethods["agent.import"]["result"] | undefined>(undefined);
  const { busy, writeDisabled, rpcWrite, onChangedRef }: PanelWrites = usePanelWrites({ canWrite, guard: "import", onChanged });

  const choose = async (next: File[]) => {
    setFiles(next);
    setProblem(undefined);
    setRead(undefined);
    const file = next[0];
    if (file === undefined) return;
    setRead(readCard(await readText(file)));
  };

  const ready = read !== undefined && read.ok;
  const runImport = async (acceptUnsigned: boolean) => {
    if (!canWrite || read === undefined || !read.ok) return;
    // `acceptUnsigned` is the person's yes, for this import alone; it is sent only when they gave it.
    const answer = await rpcWrite("agent.import", acceptUnsigned ? { content: read.card.text, acceptUnsigned: true } : { content: read.card.text });
    if (answer === undefined) return;
    setImported(answer);
    onChangedRef.current();
  };
  const again = () => {
    setImported(undefined);
    setFiles([]);
    setRead(undefined);
    setProblem(undefined);
  };

  if (imported !== undefined) {
    const word = imported.result;
    const done =
      word === "created" ? t("agents.import.done.created", { name: imported.agent.name })
      : word === "updated" ? t("agents.import.done.updated", { name: imported.agent.name, version: imported.agent.version })
      : word.startsWith("no-op") ? t("agents.import.done.noop", { name: imported.agent.name })
      : t("agents.import.done.other", { word });
    return (
      <div className="space-y-4" data-testid="agent-panel-imported">
        <Panel tone="green" variant="simple" clickable={false}>
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-sm font-medium" data-testid="agent-panel-imported-name">{done}</p>
            {/* The daemon's own word, as it answered it, one tone each. */}
            <Pill tone={importWordTone(word)} size="sm" data-testid="agent-import-word">{word}</Pill>
          </div>
          {!imported.agent.signed && (
            <p className="mt-2 text-xs opacity-80" data-testid="agent-import-marked-unsigned">{t("agents.import.markedUnsigned")}</p>
          )}
          <AgentDetail agent={imported.agent} />
        </Panel>
        <div className="flex flex-wrap gap-2">
          {onOpenAgent !== undefined && (
            <Button onClick={() => onOpenAgent(imported.agent.slug)} data-testid="agent-import-open">{t("agents.import.open", { name: imported.agent.name })}</Button>
          )}
          <Button variant="ghost" onClick={again} data-testid="agent-import-another">{t("agents.import.another")}</Button>
        </div>
      </div>
    );
  }

  const reading = read === undefined ? undefined : read.ok ? read.card : undefined;
  const refused = read !== undefined && !read.ok ? t(`agents.import.${read.problem}`) : problem;
  return (
    <div className="space-y-6" data-testid="agent-import">
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">{t("agents.import.step1")}</h3>
        <p className="text-sm opacity-80" data-testid="agent-panel-import-note">{t("agents.import.step1Help")}</p>
        <FileUpload
          variant="simple"
          accept=".json,application/json"
          maxSize={MAX_CARD_BYTES}
          value={files}
          compact
          disabled={writeDisabled}
          error={refused}
          size="md"
          onChange={(next) => void choose(next)}
          onReject={(rejections) => setProblem(rejections[0]?.code === "size" ? t("agents.import.tooBig") : t("agents.import.wrongType"))}
          labels={{ title: t("agents.import.drop"), description: t("agents.import.dropHelp"), browse: t("agents.import.browse") }}
          aria-label={t("agents.import.step1")}
        />
      </section>

      {reading === undefined && (
        <section className="space-y-2" data-testid="agent-import-waiting">
          <h3 className="text-sm font-semibold opacity-70">{t("agents.import.step2")}</h3>
          <p className="text-sm opacity-70">{t("agents.import.step2Waiting")}</p>
        </section>
      )}

      {reading !== undefined && (
        <section className="space-y-2" data-testid="agent-import-summary">
          <h3 className="text-sm font-semibold">{t("agents.import.step2")}</h3>
          <Panel variant="simple" padding="sm" clickable={false}>
            <InfoRow label={t("agents.import.summary.name")} labelWidth="w-28" variant="plain" value={reading.name} data-testid="agent-import-name" />
            <InfoRow label={t("agents.import.summary.slug")} labelWidth="w-28" variant="plain" mono value={reading.slug} data-testid="agent-import-slug" />
            <InfoRow label={t("agents.import.summary.version")} labelWidth="w-28" variant="plain" value={reading.version} data-testid="agent-import-version" />
            <InfoRow label={t("agents.import.summary.signed")} labelWidth="w-28" variant="plain" hideIfEmpty={false} value={reading.hasSignature ? t("agents.import.summary.yes") : t("agents.import.summary.no")} data-testid="agent-import-signed" />
          </Panel>
          {!reading.hasSignature && (
            <Alert intent="warning" title={t("agents.import.unsignedTitle")} description={t("agents.import.unsignedBody")} data-testid="agent-import-unsigned" />
          )}
        </section>
      )}

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">{t("agents.import.step3")}</h3>
        <p className="text-sm opacity-80">{ready ? t("agents.import.step3Help") : t("agents.import.chooseFirst")}</p>
        <Button onClick={() => (read?.ok && !read.card.hasSignature ? setConfirming(true) : void runImport(false))} disabled={writeDisabled || !ready} data-testid="agent-panel-import-run">
          {busy ? t("agents.import.running") : t("agents.import.run")}
        </Button>
      </section>

      <p className="text-xs opacity-70" data-testid="agent-panel-import-consent">{t("agents.import.consent")}</p>

      {/* The person's explicit yes to an agent nobody has vouched for. It is theirs to give — nothing here forbids it. */}
      {confirming && (
        <ConfirmModal
          isOpen
          title={t("agents.import.confirmTitle")}
          description={t("agents.import.confirmBody")}
          confirmLabel={t("agents.import.confirmYes")}
          cancelLabel={t("agents.import.confirmNo")}
          onClose={() => setConfirming(false)}
          onConfirm={() => {
            setConfirming(false);
            void runImport(true);
          }}
          data-testid="agent-import-confirm-unsigned"
        />
      )}
    </div>
  );
}
