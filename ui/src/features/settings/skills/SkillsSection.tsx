import { useState } from "react";
import { Alert, Button, ContentActions, MultiToggle, SearchBar, Select } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { errorSentence } from "../../../lib/bridge";
import type { LiveController, LiveState } from "../../../lib/live";
import { useSkills, type SkillSource, type SkillStatus, type SkillView } from "../../../lib/skills";
import { SkillInstallForm } from "./SkillInstallForm";
import { SkillPanel } from "./SkillPanel";
import { SkillsTable, skillKey } from "./SkillsTable";

/**
 * Skills — what the agents will have available, validated.
 *
 * The daemon finds Agent Skills (SKILL.md folders) in the person's own folder and in its
 * managed folder, checks each against the open specification and answers with the verdicts.
 * Broken skills stay listed with their issues. Nothing in a skill is run. The frame is the
 * Agents page's (docs/aeon/UI-DESIGN-LANGUAGE.md): header with the primary verb, action bar,
 * a table that fills the height; one skill opens in a side panel, and install is another.
 */
export interface SkillsSectionProps {
  controller: LiveController;
  live: LiveState;
  canWrite?: boolean;
}

export function SkillsSection({ live, canWrite = live.state === "current" }: SkillsSectionProps) {
  const connected = live.state === "current";
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<"" | SkillStatus>("");
  const [source, setSource] = useState<"" | SkillSource>("");
  const [opened, setOpened] = useState<SkillView | undefined>(undefined);
  const [installing, setInstalling] = useState(false);
  const [notice, setNotice] = useState<string | undefined>(undefined);

  const { result, error, busy, reload, revalidate } = useSkills(connected, {
    status: status || undefined,
    source: source || undefined,
    query: q.trim() || undefined,
  });
  const rows = result?.skills ?? [];
  const reason = canWrite ? undefined : "Reconnecting — changes are off until the daemon is current.";

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="skills-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="skills-header"
        actions={
          <>
            <Button size="sm" variant="ghost" disabled={!connected || busy} onClick={revalidate} data-testid="skills-reread">{busy ? "Reading…" : "Re-read disk"}</Button>
            <Button size="sm" disabled={!canWrite} tooltip={reason} onClick={() => { setNotice(undefined); setInstalling(true); }} data-testid="skill-install">Install skill</Button>
          </>
        }
      >
        <div>
          <h1 className="text-3xl font-semibold">Skills</h1>
          <p className="mt-1 text-sm opacity-70">Agent Skills the daemon found in your folder and in its managed folder, each checked against the specification. Nothing in a skill is run.</p>
        </div>
      </ContentActions>

      {error && <Alert className="shrink-0" intent="danger" title={errorSentence(error)} data-testid="skills-error" />}
      {notice !== undefined && <Alert className="shrink-0" intent="success" title={notice} data-testid="skills-notice" />}

      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="skills-action-bar">
        <label className="block w-full max-w-md" data-testid="skills-search">
          <span className="sr-only">Search skills</span>
          <SearchBar placeholder="Search by name or description" debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ("")} />
        </label>
        <MultiToggle
          size="sm"
          aria-label="Verdict"
          options={[
            { value: "", label: "All" },
            { value: "valid", label: "Valid" },
            { value: "warn", label: "Notes" },
            { value: "invalid", label: "Refused" },
          ]}
          value={status}
          onChange={(next) => setStatus(next as "" | SkillStatus)}
        />
        <div className="w-48 shrink-0"><Select variant="ghost" size="md" aria-label="Folder" value={source} onChange={(e) => setSource(e.target.value as "" | SkillSource)}>
          <option value="">Both folders</option>
          <option value="user">Your folder</option>
          <option value="managed">Managed folder</option>
        </Select></div>
      </div>

      {result && (
        <p className="shrink-0 text-xs opacity-70" data-testid="skills-summary">
          {result.summary.valid} valid · {result.summary.warn} with notes · {result.summary.invalid} refused · {result.summary.shadowed} shadowed.{" "}
          {result.watching === true
            ? `Watching both folders${result.watchError ? ` (partly blind: ${result.watchError})` : ""}.`
            : result.watching === false
              ? `Not watching: ${result.watchError || "no reason given"}. Re-read the disk to refresh.`
              : "This daemon does not report watching. Re-read the disk to refresh."}
        </p>
      )}

      <SkillsTable
        rows={rows}
        loading={result === null && error === null}
        filtered={q.trim() !== "" || status !== "" || source !== ""}
        resetKey={`${q}|${status}|${source}`}
        onOpen={setOpened}
      />

      {opened !== undefined && (
        <OverlaySidePanel title={opened.name} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setOpened(undefined)}>
          <SkillPanel
            key={skillKey(opened)}
            skill={opened}
            canWrite={canWrite}
            onRemoved={() => { setOpened(undefined); setNotice(`Removed ${opened.name}.`); void reload(); }}
          />
        </OverlaySidePanel>
      )}

      {installing && (
        <OverlaySidePanel title="Install a skill" titleClassName="text-xl font-semibold" variant="simple" width={520} onClose={() => setInstalling(false)}>
          <SkillInstallForm
            onInstalled={(message) => { setInstalling(false); setNotice(message); void reload(); }}
            onCancel={() => setInstalling(false)}
          />
        </OverlaySidePanel>
      )}
    </div>
  );
}
