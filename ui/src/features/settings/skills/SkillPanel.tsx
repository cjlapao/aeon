import { useEffect, useState } from "react";
import { Alert, Button, ConfirmModal, Pill } from "../../../components/kit";
import { errorSentence, toRpcError } from "../../../lib/bridge";
import { getSkill, removeSkill, type SkillGetResult, type SkillView } from "../../../lib/skills";

/**
 * One skill, in full. The verdict and every issue are the daemon's own words. The body is
 * shown as plain text — never rendered as markup — and nothing in the skill is executed:
 * scripts are counted, not run. Only a skill in Aeon's managed folder can be removed here;
 * the person's own folder is never written by Aeon.
 */
export interface SkillPanelProps {
  skill: SkillView;
  canWrite: boolean;
  onRemoved: () => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
const TONES = { valid: "green", warn: "amber", invalid: "red" } as const;

export function SkillPanel({ skill, canWrite, onRemoved }: SkillPanelProps) {
  const [detail, setDetail] = useState<SkillGetResult | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    setDetail(undefined);
    setError(undefined);
    getSkill(skill.name).then(
      (res) => { if (live) setDetail(res); },
      (cause) => { if (live) setError(errorSentence(toRpcError(cause))); },
    );
    return () => { live = false; };
  }, [skill.name, skill.path]);

  async function remove() {
    setAsking(false);
    setBusy(true);
    setError(undefined);
    try {
      await removeSkill(skill.name);
      onRemoved();
    } catch (cause) {
      setError(errorSentence(toRpcError(cause)));
    } finally {
      setBusy(false);
    }
  }

  const s = detail?.skill ?? skill;
  const managed = skill.source === "managed";

  return (
    <div className="space-y-8" data-testid="skill-panel">
      {error !== undefined && <div data-testid="skill-error"><Alert intent="danger" title={error} /></div>}

      <section className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <Pill size="sm" tone={TONES[s.status]}>{s.status}</Pill>
          <Pill size="sm" tone="slate">{managed ? "managed folder" : "your folder"}</Pill>
          {s.shadowed && <Pill size="sm" tone="slate">shadowed by {s.shadowedBy || "the other folder"}</Pill>}
        </div>
        <p className="text-sm">{s.description || "No description."}</p>
        {managed ? (
          <div>
            <Button size="sm" variant="ghost" disabled={!canWrite || busy} onClick={() => setAsking(true)} data-testid="skill-remove">Remove from the managed folder</Button>
          </div>
        ) : (
          <p className="text-sm opacity-70" data-testid="skill-user-note">This skill is in your own folder. Aeon never deletes from it.</p>
        )}
      </section>

      {s.issues && s.issues.length > 0 && (
        <section className="space-y-3" data-testid="skill-issues">
          <h3 className={H}>What did the validator find?</h3>
          <ul className="space-y-1 text-sm">
            {s.issues.map((issue, i) => (
              <li key={i} className={issue.severity === "error" ? "text-red-500" : "text-amber-500"}>
                [{issue.severity}] {issue.field}: {issue.detail}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="space-y-3">
        <h3 className={H}>Where is it, and what is in it?</h3>
        <dl className="space-y-1 text-sm">
          <div><dt className="inline opacity-70">Path: </dt><dd className="inline break-all font-mono text-xs">{s.path}</dd></div>
          <div><dt className="inline opacity-70">Files: </dt><dd className="inline">{s.files.scripts} scripts · {s.files.references} references · {s.files.assets} assets (scripts are counted, never run)</dd></div>
          <div><dt className="inline opacity-70">Size: </dt><dd className="inline">{s.bodyLines} lines in SKILL.md · {s.totalBytes} bytes on disk</dd></div>
          {s.license && <div><dt className="inline opacity-70">License: </dt><dd className="inline">{s.license}</dd></div>}
          {s.frontmatterName && s.frontmatterName !== s.name && (
            <div><dt className="inline opacity-70">Frontmatter name: </dt><dd className="inline">{s.frontmatterName} (the folder name wins)</dd></div>
          )}
          {s.unknown && Object.keys(s.unknown).length > 0 && (
            <div><dt className="inline opacity-70">Extra fields kept: </dt><dd className="inline">{Object.keys(s.unknown).join(", ")}</dd></div>
          )}
        </dl>
      </section>

      <section className="space-y-3">
        <h3 className={H}>What does it say?</h3>
        {detail === undefined && error === undefined && <p className="text-sm opacity-70">Reading SKILL.md…</p>}
        {detail !== undefined && (
          <>
            <pre className="max-h-[28rem] overflow-auto whitespace-pre-wrap rounded-sm border border-[var(--border)] p-3 text-xs" data-testid="skill-body">{detail.body}</pre>
            {detail.truncated && <p className="text-xs opacity-70">The text is cut at the wire limit. The file on disk is complete.</p>}
          </>
        )}
      </section>

      {asking && (
        <ConfirmModal
          isOpen
          title={`Remove ${skill.name}?`}
          description="The folder is deleted from Aeon's managed folder. A copy in your own folder, if there is one, is not touched."
          confirmLabel="Remove"
          cancelLabel="Keep it"
          onClose={() => setAsking(false)}
          onConfirm={() => void remove()}
          data-testid="skill-remove-ask"
        />
      )}
    </div>
  );
}
