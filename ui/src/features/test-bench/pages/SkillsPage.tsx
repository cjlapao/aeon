import { useMemo, useState } from "react";
import { Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { toRpcError, type RpcError } from "../../../lib/bridge";
import {
  SKILL_STATUSES,
  getSkill,
  installSkill,
  removeSkill,
  useSkills,
  type SkillGetResult,
  type SkillSource,
  type SkillStatus,
  type SkillView,
} from "../../../lib/skills";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * The Agent Skills catalog (S65) — what the agents will have available,
 * validated, shown where it is consumed.
 *
 * # Why this page exists at all
 *
 * The slice's promise in one screen: Aeon discovers Agent Skills from the
 * operator&apos;s `~/.agents/skills` and its own managed folder, validates each
 * against the open specification, and shows the verdicts — **without
 * anything being injected into a chat.** The validation shown IS
 * validation done: every badge and every issue line is the daemon&apos;s own
 * verdict, delivered verbatim through the socket. Broken skills stay
 * listed with their issues; nothing is silently dropped (P31&apos;s third
 * state, rendered).
 *
 * # What this page is NOT
 *
 * It does not execute anything. `scripts/` files are COUNTED, never run —
 * the daemon&apos;s rule, mirrored in the browser. It does not write to the
 * user root: installs land in the managed folder, and removing a name that
 * lives only in the operator&apos;s own folder refuses with the installer&apos;s
 * own sentence. Since S66 the catalog DOES ride every step — the daemon
 * appends it to each step&apos;s context and the chat shows an injection row
 * naming the offer; this page is the catalog&apos;s health screen (and, from
 * S66, its watch-state screen), not the injection surface.
 */

const STATUS_TONES: Record<SkillStatus, "green" | "amber" | "red"> = {
  valid: "green",
  warn: "amber",
  invalid: "red",
};

const STATUS_WORDS: Record<SkillStatus, string> = {
  valid: "spec-clean",
  warn: "valid, with notes",
  invalid: "refused by the validator",
};

function SkillRow({
  skill,
  expanded,
  onToggle,
  onRemoved,
}: {
  skill: SkillView;
  expanded: boolean;
  onToggle: () => void;
  onRemoved: () => void;
}) {
  const [detail, setDetail] = useState<SkillGetResult | null>(null);
  const [detailError, setDetailError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  const open = async () => {
    onToggle();
    if (expanded || detail) return;
    try {
      setDetailError(null);
      setDetail(await getSkill(skill.name));
    } catch (e) {
      setDetailError(toRpcError(e));
    }
  };

  const remove = async () => {
    try {
      setBusy(true);
      await removeSkill(skill.name);
      onRemoved();
    } catch (e) {
      setDetailError(toRpcError(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="border-b border-[var(--border)] py-2" data-testid={`skills-row-${skill.name}`}>
      <div className="flex items-center gap-3">
        <Pill tone={STATUS_TONES[skill.status]} size="sm">{skill.status}</Pill>
        <button type="button" className="text-left font-medium grow" onClick={open}>
          {skill.name}
          {skill.shadowed ? (
            <span className="muted"> — shadowed by {skill.shadowedBy || "the other root"}</span>
          ) : null}
        </button>
        <span className="muted text-xs">
          {skill.source === "managed" ? "managed" : "user"} · {skill.files.scripts} scripts ·{" "}
          {skill.files.references} refs · {skill.files.assets} assets
        </span>
      </div>
      {skill.description ? (
        <div className="muted text-sm mt-1 line-clamp-2">{skill.description}</div>
      ) : (
        <div className="muted text-sm mt-1">(no description — the validator&apos;s business, below)</div>
      )}
      {skill.issues && skill.issues.length > 0 ? (
        <ul className="mt-1">
          {skill.issues.map((issue, i) => (
            <li key={i} className={issue.severity === "error" ? "text-red-500 text-xs" : "text-amber-500 text-xs"}>
              [{issue.severity}] {issue.field}: {issue.detail}
            </li>
          ))}
        </ul>
      ) : null}
      {expanded ? (
        <div className="mt-2 pl-3 border-l border-[var(--border)]">
          {detailError ? <ErrorPanel error={detailError} /> : null}
          {detail ? (
            <>
              <div className="muted text-xs">
                {detail.skill.path} · body {detail.skill.bodyBytes}B / {detail.skill.bodyLines} lines ·{" "}
                {detail.skill.totalBytes}B on disk
                {detail.skill.license ? ` · license: ${detail.skill.license}` : ""}
                {detail.skill.frontmatterName && detail.skill.frontmatterName !== detail.skill.name
                  ? ` · frontmatter names it "${detail.skill.frontmatterName}" (the folder wins)`
                  : ""}
              </div>
              {detail.skill.unknown && Object.keys(detail.skill.unknown).length > 0 ? (
                <div className="muted text-xs mt-1">
                  kept-and-ignored fields: {Object.keys(detail.skill.unknown).join(", ")} — a newer
                  agent&apos;s extension fields do not invalidate a skill
                </div>
              ) : null}
              <pre className="text-xs mt-2 whitespace-pre-wrap max-h-72 overflow-auto">{detail.body}</pre>
              {detail.truncated ? (
                <div className="muted text-xs">
                  the body is cut at the wire cap — the file on disk is complete
                </div>
              ) : null}
            </>
          ) : detailError ? null : (
            <span className="muted text-xs">reading SKILL.md…</span>
          )}
          {skill.source === "managed" ? (
            <div className="mt-2">
              <Button size="sm" variant="ghost" onClick={() => void remove()} disabled={busy} data-testid={`skills-remove-${skill.name}`}>
                remove from managed folder
              </Button>
            </div>
          ) : (
            <div className="muted text-xs mt-2">
              this skill lives in your own folder — Aeon never deletes from it
            </div>
          )}
        </div>
      ) : null}
    </div>
  );
}

export function SkillsPage({ connected = true }: { connected?: boolean } = {}) {
  const [status, setStatus] = useState<"" | SkillStatus>("");
  const [source, setSource] = useState<"" | SkillSource>("");
  const [query, setQuery] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);

  const [installSource, setInstallSource] = useState("");
  const [installRef, setInstallRef] = useState("");
  const [installSubpath, setInstallSubpath] = useState("");
  const [installForce, setInstallForce] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [installed, setInstalled] = useState<string | null>(null);

  const [actionError, setActionError] = useState<RpcError | null>(null);

  const { result, error, busy, reload, revalidate } = useSkills(connected, {
    status: status || undefined,
    source: source || undefined,
    query: query.trim() || undefined,
  });

  const rows = useMemo(() => result?.skills ?? [], [result]);

  const install = async () => {
    const src = installSource.trim();
    if (!src) return;
    try {
      setInstalling(true);
      setActionError(null);
      setInstalled(null);
      const res = await installSkill({
        source: src,
        ref: installRef.trim() || undefined,
        subpath: installSubpath.trim() || undefined,
        force: installForce,
      });
      setInstalled(
        `installed ${res.skill.name} [${res.skill.status}]${res.replaced ? " (replaced the managed copy)" : ""}`,
      );
      setInstallSource("");
      await reload();
    } catch (e) {
      setActionError(toRpcError(e));
    } finally {
      setInstalling(false);
    }
  };

  return (
    <div>
      <Panel title="Agent skills — the catalog, validated (S65)">
        <div className="muted">
          Aeon discovers Agent Skills (the SKILL.md format) from your{" "}
          <code>~/.agents/skills</code> and its own managed folder, validates each one against the
          open specification, and shows the verdicts here. Nothing is injected into a chat and
          nothing in a skill is executed — scripts are counted, never run. Installs land in the
          managed folder; your own folder is never written or deleted by Aeon.
        </div>
      </Panel>

      <Panel title="Filters — and the freshness door">
        <div className="row">
          <FormField label="status" className="grow">
            <Select value={status} onChange={(e) => setStatus(e.target.value as "" | SkillStatus)}>
              <option value="">all verdicts</option>
              {SKILL_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {s} — {STATUS_WORDS[s]}
                </option>
              ))}
            </Select>
          </FormField>
          <FormField label="root" className="grow">
            <Select value={source} onChange={(e) => setSource(e.target.value as "" | SkillSource)}>
              <option value="">both roots</option>
              <option value="user">user (~/.agents/skills)</option>
              <option value="managed">managed (Aeon&apos;s folder)</option>
            </Select>
          </FormField>
          <FormField label="search" className="grow">
            <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="name or description" />
          </FormField>
          <FormField label=" " className="">
            <Button onClick={revalidate} disabled={!connected || busy}>
              {busy ? "re-reading…" : "validate (re-read disk)"}
            </Button>
          </FormField>
        </div>
        {result ? (
          <div className="muted text-xs mt-2">
            {result.summary.valid} valid · {result.summary.warn} with warnings ·{" "}
            {result.summary.invalid} invalid · {result.summary.shadowed} shadowed — scanned{" "}
            {result.scannedAt} · user <code>{result.userRoot}</code> · managed{" "}
            <code>{result.managedRoot}</code>
          </div>
        ) : null}
        {/* S66: the watcher's three states, spoken — watched, blind-with-a-
            reason, and unknown-on-an-old-daemon (P31). The sentence is the
            daemon's own watchError verbatim when there is one. */}
        {result ? (
          <div className="muted text-xs" data-testid="skills-watch-line">
            {result.watching === true ? (
              <>
                watching — a folder dropped into either root is noticed without a restart
                {result.watchError ? <> · <span className="text-amber-600 dark:text-amber-400!">partly blind: {result.watchError}</span></> : null}
              </>
            ) : result.watching === false ? (
              <>not watching — {result.watchError || "the daemon gave no reason"} — the catalog still answers; “validate” re-reads the disk</>
            ) : (
              <>watch state unknown — this daemon predates the watcher; “validate” re-reads the disk</>
            )}
          </div>
        ) : null}
      </Panel>

      {error ? <ErrorPanel error={error} /> : null}
      {actionError ? <ErrorPanel error={actionError} /> : null}

      <Panel title="Catalog">
        {rows.length === 0 ? (
          <div className="muted" data-testid="skills-empty">
            {connected
              ? "no skills matched — an empty catalog is reported, not hidden: the roots above are empty as filtered (or absent)."
              : "not connected — the catalog is the daemon's to answer."}
          </div>
        ) : (
          rows.map((s) => (
            <SkillRow
              key={`${s.source}:${s.path}`}
              skill={s}
              expanded={expanded === `${s.source}:${s.path}`}
              onToggle={() => setExpanded(expanded === `${s.source}:${s.path}` ? null : `${s.source}:${s.path}`)}
              onRemoved={() => {
                setExpanded(null);
                void reload();
              }}
            />
          ))
        )}
      </Panel>

      <Panel title="Install — into the managed folder (the fetch runs in the daemon)">
        <div className="row">
          <FormField label="source — GitHub locator or a folder on the daemon's disk" className="grow">
            <Input
              value={installSource}
              onChange={(e) => setInstallSource(e.target.value)}
              placeholder="owner/repo · owner/repo/skills/pdf@v1.2 · https://github.com/… · /home/dev/downloads/pdf"
            />
          </FormField>
          <FormField label="ref (branch/tag)" className="">
            <Input value={installRef} onChange={(e) => setInstallRef(e.target.value)} placeholder="main" />
          </FormField>
          <FormField label="subpath" className="">
            <Input value={installSubpath} onChange={(e) => setInstallSubpath(e.target.value)} placeholder="skills/pdf" />
          </FormField>
        </div>
        <div className="row items-center mt-2">
          <label className="muted text-xs flex items-center gap-2">
            <input type="checkbox" checked={installForce} onChange={(e) => setInstallForce(e.target.checked)} />
            replace an existing managed skill of this name
          </label>
          <Button onClick={install} disabled={!connected || installing || !installSource.trim()} data-testid="skills-install">
            {installing ? "installing…" : "install"}
          </Button>
        </div>
        <div className="muted text-xs mt-2">
          The daemon clones shallowly with a 60s budget, stages off the destination, validates the
          frontmatter against the FINAL folder name BEFORE placing, and refuses without a word of
          it landed. A skill whose SKILL.md breaks a spec rule never half-installs.
        </div>
        {installed ? <div className="text-sm mt-2 text-green-500">{installed}</div> : null}
      </Panel>
    </div>
  );
}

registerBenchPage({
  id: "skills",
  label: "Agent Skills",
  path: "/bench/skills",
  order: 4500,
  needsConnection: true,
  render: (ctx) => <SkillsPage connected={ctx.connected} />,
});
