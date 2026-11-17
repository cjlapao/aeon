import { useState } from "react";
import { Alert, Button, Input } from "../../../components/kit";
import { toRpcError } from "../../../lib/bridge";
import type { RpcMethods } from "../../../proto/contract";

// S37 T6 — the "Propose import" affordance on the Agents page.
//
// It is a PROPOSAL, never a commit: it calls `proposal.create` for the
// `agent_import` kind, shows S35's exact toast, and renders the returned
// preview — labelled advisory. No commit, confirm or import-now control
// anywhere (criterion 6; commit stays CLI-only). `ProposalsPage.tsx` is
// untouched by this slice.
//
// Every string the form shows comes from the response or the typed input.
// The form never reads a filesystem path (criterion 6 / TD10 thin-client):
// the local absolute-path check is cosmetic only — the daemon's refusal is
// the authority, and a `{ok:false}` refusal is rendered verbatim, with the
// typed value kept (S35 test map #6: never a silent reset).

/** The `proposal.create` result — read off the generated contract, not retyped. */
export type ProposalCreateResult = RpcMethods["proposal.create"]["result"];

export interface ProposeImportFormProps {
  /** The one write: `proposal.create` for the typed kind/payload. */
  create: (kind: string, payload: unknown) => Promise<ProposalCreateResult>;
  /** The toast — S35's exact pattern, wired by the page to the notifications service. */
  onToast: (message: string) => void;
}

// The local, cosmetic absolute-path check. It never decides — the daemon's
// refusal is the authority (a stored relative path is refused at the commit
// door) — it only keeps a clearly-wrong value off the wire.
function isAbsolute(p: string): boolean {
  return p.startsWith("/") || /^[A-Za-z]:[\\/.]/.test(p);
}

export function ProposeImportForm({ create, onToast }: ProposeImportFormProps) {
  const [path, setPath] = useState("");
  const [busy, setBusy] = useState(false);
  const [cosmetic, setCosmetic] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [unwired, setUnwired] = useState(false);
  const [result, setResult] = useState<ProposalCreateResult | null>(null);

  const submit = async () => {
    const value = path.trim();
    setUnwired(false);
    if (!isAbsolute(value)) {
      setCosmetic("must be an absolute path");
      return; // never call the socket for a value the daemon would refuse anyway
    }
    setCosmetic(null);
    setRefusal(null);
    setResult(null);
    setBusy(true);
    try {
      const row = await create("agent_import", { pkgPath: value });
      // S35's exact toast, built from the response's command field — never a
      // command the form invents from the id (the daemon owns the grammar).
      onToast(`Proposed · commit with ${row.command}`);
      setResult(row);
    } catch (cause) {
      const err = toRpcError(cause);
      if (err.isTransport) {
        // The daemon is not connected: the three-state honesty, never an
        // empty result that reads as "nothing happened".
        setUnwired(true);
      } else {
        // A daemon refusal: render it verbatim, keep the typed value (no
        // silent reset).
        setRefusal(err.message);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-3" data-testid="propose-import-form">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Import an agent</h3>
      </div>
      <p className="text-xs text-slate-500">
        Proposes an import; nothing is imported until you run the command shown.
      </p>

      <div className="flex flex-wrap items-start gap-2">
        <Input
          aria-label="Package path"
          placeholder="/absolute/path/to/agent-package"
          value={path}
          onChange={(e) => setPath(e.target.value)}
          data-testid="propose-import-path"
          className="min-w-0 flex-1"
        />
        <Button
          onClick={() => void submit()}
          disabled={busy}
          data-testid="propose-import-submit"
        >
          Propose import
        </Button>
      </div>

      {cosmetic && (
        <p className="text-xs text-red-700 dark:text-red-300!" data-testid="propose-import-cosmetic">
          {cosmetic}
        </p>
      )}

      {unwired && (
        <Alert
          tone="red"
          title="the daemon is not connected"
          description="Propose import needs the daemon to answer; it could not reach one, so nothing was proposed. Reconnect and try again."
          data-testid="propose-import-unwired"
        />
      )}

      {refusal && (
        <p className="text-xs text-red-700 dark:text-red-300!" data-testid="propose-import-refusal">
          {refusal}
        </p>
      )}

      {result && <ImportPreview result={result} />}
    </div>
  );
}

/**
 * The preview — labelled advisory (AA4): the commit re-reads the package and
 * shows the authoritative consent, so this is never the decision. The consent
 * is shown in a `<pre>` byte for byte (S15 T4 — never summarised), the agent
 * name as a heading, and each carried Deny by its id and target kind/id.
 * Every string here is from the response; the form never reads a path.
 */
function ImportPreview({ result }: { result: ProposalCreateResult }) {
  const preview = result.preview;
  return (
    <div className="space-y-2" data-testid="propose-import-preview">
      <p className="text-xs text-slate-500" data-testid="propose-import-preview-label">
        Preview — the commit re-reads the package and shows the authoritative consent
      </p>
      {preview ? (
        <>
          <h4 className="text-sm font-semibold" data-testid="propose-import-preview-agent">
            {preview.agent}
          </h4>
          <pre className="whitespace-pre-wrap break-words font-mono text-xs" data-testid="propose-import-preview-consent">
            {preview.consent}
          </pre>
          {preview.carriedDeny.length > 0 && (
            <ul className="space-y-1" data-testid="propose-import-preview-carried">
              {preview.carriedDeny.map((d) => (
                <li key={d.id} className="text-xs" data-testid={`propose-import-carried-${d.id}`}>
                  {d.id} · {d.target.kind}:{d.target.id}
                </li>
              ))}
            </ul>
          )}
        </>
      ) : (
        // No preview on the response: say so, never render an empty result.
        <p className="text-xs text-slate-500" data-testid="propose-import-preview-none">
          no preview — the commit re-reads the package and shows the authoritative consent
        </p>
      )}
    </div>
  );
}
