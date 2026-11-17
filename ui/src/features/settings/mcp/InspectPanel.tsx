import { useCallback, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, Pill } from "../../../components/kit";
import { errorSentence, rpcTyped, toRpcError } from "../../../lib/bridge";
import {
  buildInspectParams,
  canInspect,
  schemaParams,
  type InspectAnswer,
  type InspectedTool,
  type McpVocabulary,
  type ServerDraft,
} from "./mcpModel";

/**
 * Inspect — what an MCP inspector does, inside the form. The daemon runs the
 * candidate command (nothing installed, nothing stored), reads the tools the
 * server offers with their descriptions and schemas, and stops it; the person
 * picks the tools to add. They arrive UNCLASSIFIED: the server's own hints
 * (read-only, destructive …) are shown as hints and classify nothing — only the
 * person's configuration does (MC6).
 */
export interface InspectPanelProps {
  draft: ServerDraft;
  vocabulary: McpVocabulary | undefined;
  disabled: boolean;
  /** The picked tools, to merge into the draft. */
  onUse: (tools: InspectedTool[], server: InspectAnswer) => void;
}

export function InspectPanel({ draft, vocabulary, disabled, onUse }: InspectPanelProps) {
  const { t } = useI18n();
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [answer, setAnswer] = useState<InspectAnswer | undefined>(undefined);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  // Off every time: stored values reach the command only when this run asks for them.
  const [useVariables, setUseVariables] = useState(false);

  const ready = canInspect(draft, vocabulary);

  const inspect = useCallback(async () => {
    setRunning(true);
    setError(undefined);
    try {
      const result = (await rpcTyped("mcp.server.inspect", buildInspectParams(draft, useVariables && draft.env.length > 0))) as unknown as InspectAnswer;
      setAnswer(result);
      setPicked(new Set(result.tools.map((tool) => tool.name)));
    } catch (cause) {
      setAnswer(undefined);
      setError(errorSentence(toRpcError(cause)));
    } finally {
      setRunning(false);
    }
  }, [draft, useVariables]);

  const toggle = (name: string) =>
    setPicked((current) => {
      const next = new Set(current);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });

  return (
    <div className="space-y-2 rounded-lg border border-slate-200 p-3 dark:border-slate-700" data-testid="inspect-panel">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide opacity-70">{t("settings.mcp.form.inspect.title")}</p>
          <p className="text-xs opacity-70">{t("settings.mcp.form.inspect.note")}</p>
        </div>
        <Button size="sm" disabled={disabled || running || !ready} onClick={() => void inspect()} data-testid="inspect-run">
          {t("settings.mcp.form.inspect.button")}
        </Button>
      </div>

      {draft.env.length > 0 && (
        <label className="flex items-start gap-2 text-xs" data-testid="inspect-use-variables">
          <input type="checkbox" className="mt-0.5" checked={useVariables} disabled={disabled || running} aria-label="give the command my stored values"
            onChange={(e) => setUseVariables(e.target.checked)} />
          <span>{t("settings.mcp.form.inspect.useVariables")}</span>
        </label>
      )}

      {running && <p className="text-xs opacity-80" data-testid="inspect-running">{t("settings.mcp.form.inspect.running")}</p>}
      {error !== undefined && <div data-testid="inspect-error"><Alert intent="danger" title={t("settings.mcp.form.inspect.failed")}>{error}</Alert></div>}

      {answer !== undefined && (
        <div className="space-y-2" data-testid="inspect-result">
          <p className="text-xs font-semibold">
            {t("settings.mcp.form.inspect.found", { count: answer.tools.length, server: answer.serverName, version: answer.serverVersion })}
          </p>
          {answer.tools.length === 0 ? (
            <Alert intent="warning" title={t("settings.mcp.form.inspect.none")} />
          ) : (
            <>
              <p className="text-xs opacity-70">{t("settings.mcp.form.inspect.hintNote")}</p>
              <div className="flex flex-wrap gap-2">
                <Button size="xs" variant="ghost" onClick={() => setPicked(new Set(answer.tools.map((tool) => tool.name)))}>{t("settings.mcp.form.inspect.all")}</Button>
                <Button size="xs" variant="ghost" onClick={() => setPicked(new Set())}>{t("settings.mcp.form.inspect.clear")}</Button>
              </div>
              <ul className="max-h-72 space-y-1 overflow-y-auto" data-testid="inspect-tools">
                {answer.tools.map((tool) => (
                  <li key={tool.name} className="rounded-md border border-slate-200 p-2 text-xs dark:border-slate-700" data-testid={`inspect-tool-${tool.name}`}>
                    <label className="flex cursor-pointer items-start gap-2">
                      <input type="checkbox" className="mt-0.5" checked={picked.has(tool.name)} onChange={() => toggle(tool.name)} aria-label={tool.name} />
                      <span className="min-w-0 flex-1">
                        <span className="flex flex-wrap items-center gap-1.5">
                          <strong className="font-mono">{tool.name}</strong>
                          {hintPills(tool)}
                        </span>
                        <span className="block whitespace-pre-wrap opacity-80">{tool.description || t("settings.mcp.form.inspect.noDescription")}</span>
                        {schemaParams(tool).length > 0 && (
                          <span className="block font-mono opacity-60">{t("settings.mcp.form.inspect.params", { names: schemaParams(tool).join(", ") })}</span>
                        )}
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
              <Button size="sm" disabled={disabled || picked.size === 0} data-testid="inspect-use" onClick={() => onUse(answer.tools.filter((tool) => picked.has(tool.name)), answer)}>
                {t("settings.mcp.form.inspect.use", { count: picked.size })}
              </Button>
            </>
          )}
        </div>
      )}
    </div>
  );
}

/** The server's own hints, shown as it named them (`readOnlyHint` reads "readOnly").
 *  Derived from the answer's keys — this section names no vocabulary word itself. */
function hintPills(tool: InspectedTool) {
  const hints = Object.entries(tool.annotations ?? {}).filter(([, on]) => on === true);
  return hints.map(([key]) => <Pill key={key} tone="slate" size="xs">{key.replace(/Hint$/, "")}</Pill>);
}
