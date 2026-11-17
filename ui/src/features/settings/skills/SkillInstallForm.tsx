import { useState } from "react";
import { Alert, Button, FormField, Input, Toggle } from "../../../components/kit";
import { errorSentence, toRpcError } from "../../../lib/bridge";
import { installSkill } from "../../../lib/skills";

/**
 * Install a skill into the managed folder. The fetch happens in the daemon: it clones with a
 * time budget, stages away from the destination, checks the SKILL.md against the specification
 * and only then places it. A skill that breaks a rule never half-installs; the daemon's refusal
 * is shown as it came.
 */
export interface SkillInstallFormProps {
  onInstalled: (message: string) => void;
  onCancel: () => void;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function SkillInstallForm({ onInstalled, onCancel }: SkillInstallFormProps) {
  const [source, setSource] = useState("");
  const [ref, setRef] = useState("");
  const [subpath, setSubpath] = useState("");
  const [force, setForce] = useState(false);
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);

  async function install() {
    setBusy(true);
    setRefusal(undefined);
    try {
      const res = await installSkill({ source: source.trim(), ref: ref.trim() || undefined, subpath: subpath.trim() || undefined, force });
      onInstalled(`Installed ${res.skill.name} (${res.skill.status})${res.replaced ? ", replacing the managed copy" : ""}.`);
    } catch (cause) {
      setRefusal(errorSentence(toRpcError(cause)));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-8" data-testid="skill-install-form">
      {refusal !== undefined && <div data-testid="skill-install-refusal"><Alert intent="danger" title={refusal} /></div>}

      <section className="space-y-3">
        <h3 className={H}>Where does it come from?</h3>
        <FormField label="Source">
          <Input variant="ghost" aria-label="skill source" value={source} disabled={busy} placeholder="owner/repo, a GitHub URL, or a folder on the daemon's disk" onChange={(e) => setSource(e.target.value)} />
        </FormField>
        <FormField label="Branch or tag (optional)">
          <Input variant="ghost" aria-label="skill ref" value={ref} disabled={busy} placeholder="main" onChange={(e) => setRef(e.target.value)} />
        </FormField>
        <FormField label="Folder inside the repository (optional)">
          <Input variant="ghost" aria-label="skill subpath" value={subpath} disabled={busy} placeholder="skills/pdf" onChange={(e) => setSubpath(e.target.value)} />
        </FormField>
      </section>

      <section className="space-y-3">
        <h3 className={H}>What if it is already installed?</h3>
        <Toggle
          size="sm"
          aria-label="replace an existing managed skill"
          checked={force}
          disabled={busy}
          label="Replace the managed copy"
          description="Without this, an install over an existing name is refused."
          onChange={(e) => setForce(e.target.checked)}
        />
      </section>

      <div className="flex gap-2">
        <Button size="sm" disabled={busy || source.trim() === ""} onClick={() => void install()} data-testid="skill-install-go">{busy ? "Installing…" : "Install"}</Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={onCancel}>Cancel</Button>
      </div>
    </div>
  );
}
