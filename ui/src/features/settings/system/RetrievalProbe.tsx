import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { searchCorpus } from "./systemRpc";
import type { StatusComponent } from "./systemModel";
import type { RpcMethods } from "../../../proto/contract";

/**
 * RetrievalProbe — "try a search": what would an agent get back for this term? (S22/S28 read-only).
 *
 * Pick a root and a corpus, type a term, and the daemon answers with its own hits: a file range and a
 * score each, in its order — never a sentence it did not send, because a query surface that could show a
 * hand-typed answer could show a hand-typed fact. A keyword-only answer (no embeddings behind the index)
 * is said as such. Search and the code graph stay two separate lookups by design; this probe never fuses
 * them. A refusal is a toast.
 */
type Result = RpcMethods["search.query"]["result"];

export function RetrievalProbe({ components }: { components: StatusComponent[] }) {
  const { t } = useI18n();
  const [root, setRoot] = useState("");
  const [corpus, setCorpus] = useState("");
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Result | undefined>(undefined);

  const chosen = components.find((c) => c.component === root) ?? components[0];
  const rootValue = chosen?.component ?? "";
  const corpora = chosen?.corpora ?? [];
  const corpusValue = corpora.some((c) => c.name === corpus) ? corpus : corpora[0]?.name ?? "";

  const search = async () => {
    setBusy(true);
    try {
      setResult(await searchCorpus(rootValue, corpusValue, query.trim()));
    } catch (cause) {
      notifications.push({ title: t("settings.system.probe.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  };

  const hits = result?.hits ?? [];
  const top = hits.reduce((m, h) => Math.max(m, h.score), 0) || 1;

  return (
    <Panel title={t("settings.system.probe.title")} subtitle={t("settings.system.probe.subtitle")} data-testid="retrieval-probe">
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <FormField label={t("settings.system.probe.root")}>
            <Select variant="ghost" aria-label={t("settings.system.probe.root")} value={rootValue} onChange={(e) => { setRoot(e.target.value); setResult(undefined); }} data-testid="probe-root">
              {components.map((c) => <option key={c.component} value={c.component}>{c.component}</option>)}
            </Select>
          </FormField>
          <FormField label={t("settings.system.probe.corpus")}>
            <Select variant="ghost" aria-label={t("settings.system.probe.corpus")} value={corpusValue} onChange={(e) => { setCorpus(e.target.value); setResult(undefined); }} data-testid="probe-corpus">
              {corpora.map((c) => <option key={c.name} value={c.name}>{c.name}</option>)}
            </Select>
          </FormField>
        </div>
        <div className="flex items-end gap-2">
          <div className="min-w-0 flex-1">
            <FormField label={t("settings.system.probe.query")}>
              <Input
                variant="ghost"
                aria-label={t("settings.system.probe.query")}
                value={query}
                placeholder={t("settings.system.probe.queryHint")}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && query.trim() !== "" && rootValue !== "" && !busy) void search(); }}
                data-testid="probe-query"
              />
            </FormField>
          </div>
          <Button size="sm" disabled={busy || query.trim() === "" || rootValue === "" || corpusValue === ""} onClick={() => void search()} data-testid="probe-go">
            {t("settings.system.probe.go")}
          </Button>
        </div>

        {result !== undefined && (
          <div className="space-y-2" data-testid="probe-result">
            <p className="flex items-center gap-2 text-xs opacity-80">
              <span data-testid="probe-count">{t("settings.system.probe.count", { count: hits.length })}</span>
              {result.keywordOnly === true && <Pill size="sm" tone="amber" data-testid="probe-keyword-only">{t("settings.system.probe.keywordOnly")}</Pill>}
            </p>
            {hits.length === 0 ? (
              <p className="text-sm opacity-70" data-testid="probe-empty">{t("settings.system.probe.empty")}</p>
            ) : (
              <ul className="divide-y divide-[var(--border)]">
                {hits.map((hit, i) => (
                  <li key={`${hit.source}-${hit.lineStart}-${i}`} className="py-2" data-testid={`probe-hit-${i}`}>
                    <div className="flex items-baseline justify-between gap-3">
                      <code className="min-w-0 truncate text-xs">{hit.source}:{hit.lineStart}–{hit.lineEnd}</code>
                      <span className="shrink-0 text-xs opacity-70">{hit.score.toFixed(2)}</span>
                    </div>
                    <div className="mt-1 h-1 rounded bg-white/10"><div className="h-full rounded bg-sky-400" style={{ width: `${Math.max(4, Math.round((hit.score / top) * 100))}%` }} /></div>
                  </li>
                ))}
              </ul>
            )}
            <p className="text-xs opacity-60">{t("settings.system.probe.note")}</p>
          </div>
        )}
      </div>
    </Panel>
  );
}
