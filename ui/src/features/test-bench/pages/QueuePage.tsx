import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import {
  METHOD_QUEUE_ADVANCE,
  METHOD_QUEUE_APPEND,
  METHOD_QUEUE_LIST,
  METHOD_QUEUE_REMOVE,
  METHOD_QUEUE_SETTINGS,

  type QueueAdvanceResult,
  type QueueItemView,
  type QueueListResult,
  type QueueSettingsResult,
  type QueueStatus,
} from "../../../lib/protocol";

/**
 * The loop queue bench (S24, T8) — the backlog of intentions before any of
 * them runs.
 *
 * # What this page is, and the one thing it is not
 *
 * A **window onto queue state plus the two knobs and the two doors a person
 * is meant to touch from the app**: `queue.list` renders the board,
 * `queue.append` holds a loop.start payload (CG3 — this form collects
 * exactly those fields and nothing else), `queue.settings` writes the pair,
 * `queue.remove` **cancels** (D12 — the row stays on the board and the board
 * keeps showing it, which is why `cancelled` renders and is never filtered),
 * and `queue.advance` is CG4's explicit door — the call itself is the
 * decision in `ask` mode, so the page asks nothing first.
 *
 * What it does **not** do (§4b's standing rule): under `queueAdvance: ask`,
 * the gate question an item must answer appears in the **existing
 * question/thread surface**, not inline here — this tab shows queue state and
 * does not re-implement the app's question-answering UI. And it is a bench,
 * not the product (S9 owns the designed experience); the CLI speaks the same
 * seven methods (T6) and the daemon underneath is the one implementation of
 * every law (CG9).
 *
 * # The seven statuses are seven
 *
 * `pending → sizing → ready → running → done`, plus `blocked` (which halts
 * the whole queue unconditionally, CG2) and `cancelled` (D12's resting
 * state). Each renders with its own label and its own `data-status`;
 * `test/queue.test.tsx` asserts the seven are distinct in the DOM, because
 * three colors doing double duty for seven states is the failure this page
 * exists to prevent from being invisible.
 *
 * # Size is a fact or it is nothing (D8)
 *
 * Present means the full triple — `min–max (basis)`, never a bare number.
 * Absent means **no size has been reported** and says so; a blank cell would
 * read as zero. Which absence-sentence renders is the settings panel's
 * `sizingMode`: under `manual`, `set-size` is the door; under `skill`, the
 * run that sizes it reports through its own `report_size` (D9).
 *
 * Attendedness is **declared, never inferred** (D5/G14): the append form's
 * third option is "the project's declared default decides" — never whoever
 * happens to be watching this page.
 */

/** CG1: the four statuses cancel may act on (running is refused, D11; done/cancelled are terminal). */
const CANCELLABLE: readonly string[] = ["pending", "sizing", "ready", "blocked"];

/** Seven distinct renderings — none of these strings repeats another. */
export const STATUS_LABELS: Record<QueueStatus, string> = {
  pending: "pending — held, not yet sized",
  sizing: "sizing — size is being worked out",
  ready: "ready — sized, waiting behind the gate",
  running: "running — this project's one active item",
  done: "done — materialized and finished",
  blocked: "BLOCKED — the whole queue halts here (CG2)",
  cancelled: "cancelled — removed, never deleted (D12)",
};

export function QueuePage() {
  const [root, setRoot] = useState("");
  const [items, setItems] = useState<QueueItemView[] | null>(null);
  const [settings, setSettings] = useState<QueueSettingsResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [notice, setNotice] = useState("");

  // The append form — a loop.start payload (CG3), collected and nothing else.
  const [blueprint, setBlueprint] = useState("");
  const [goal, setGoal] = useState("");
  const [tech, setTech] = useState("");
  const [withText, setWithText] = useState("");
  const [attended, setAttended] = useState<"default" | "attended" | "unattended">("default");

  // The settings pair — two knobs, written together (the socket refuses half
  // a pair; this page reads first and sends the merged whole).
  const [advanceMode, setAdvanceMode] = useState("ask");
  const [sizingMode, setSizingMode] = useState("manual");

  const load = useCallback(async () => {
    if (!root.trim()) return;
    try {
      setError(null);
      const [board, cfg] = await Promise.all([
        rpc<QueueListResult>(METHOD_QUEUE_LIST, { componentRoot: root.trim() }),
        rpc<QueueSettingsResult>(METHOD_QUEUE_SETTINGS, { componentRoot: root.trim() }),
      ]);
      setItems(board.items);
      setSettings(cfg);
      setAdvanceMode(cfg.queueAdvance);
      setSizingMode(cfg.sizingMode);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [root]);

  useEffect(() => {
    setItems(null);
    setSettings(null);
  }, [root]);

  const append = async () => {
    try {
      setError(null);
      setNotice("");
      const withEntries: Record<string, string> = {};
      for (const line of withText.split(/[;\n]/)) {
        const at = line.indexOf("=");
        if (at > 0) withEntries[line.slice(0, at).trim()] = line.slice(at + 1).trim();
      }
      const payload: Record<string, unknown> = {
        componentRoot: root.trim(),
        blueprint: blueprint.trim(),
        goal: goal.trim(),
      };
      if (tech.trim()) payload.tech = tech.trim();
      if (Object.keys(withEntries).length > 0) payload.with = withEntries;
      // Three states, DECLARED (D5/G14): the key is sent only when the form
      // declared one — absent means the project's default decides, and
      // "attended because a human is looking at this page" is never it.
      if (attended !== "default") payload.attended = attended === "attended";
      await rpc(METHOD_QUEUE_APPEND, payload);
      setGoal("");
      setNotice(`queued ${blueprint.trim()} — appending started nothing (CG4).`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const remove = async (id: number) => {
    try {
      setError(null);
      await rpc(METHOD_QUEUE_REMOVE, { id });
      setNotice(`#${id} cancelled — the row stays on the board (D12).`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const advance = async () => {
    try {
      setError(null);
      setNotice("");
      const res = await rpc<QueueAdvanceResult>(METHOD_QUEUE_ADVANCE, { componentRoot: root.trim() });
      setNotice(
        res.started
          ? "an item started — the call itself was the decision (CG4)."
          : "nothing started — either a row is blocked (CG2), a run is already going (D2), or the queue has no ready item.",
      );
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const saveSettings = async () => {
    try {
      setError(null);
      setNotice("");
      // Read-merge-write: the socket write carries BOTH knobs, and the
      // untouched one comes from the daemon's own current value.
      const cfg = await rpc<QueueSettingsResult>(METHOD_QUEUE_SETTINGS, { componentRoot: root.trim() });
      const next = await rpc<QueueSettingsResult>(METHOD_QUEUE_SETTINGS, {
        componentRoot: root.trim(),
        queueAdvance: advanceMode || cfg.queueAdvance,
        sizingMode: sizingMode || cfg.sizingMode,
      });
      setSettings(next);
      setNotice(`queue_advance=${next.queueAdvance} · sizing_mode=${next.sizingMode}${next.known ? "" : " — UNREGISTERED root: these are S24's declared defaults, not stored facts"}`);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const sizeCell = (item: QueueItemView) => {
    if (item.sizeMin !== null && item.sizeMax !== null && item.sizeBasis !== null) {
      // D8: range plus basis, never a bare number.
      return (
        <span data-testid={`size-${item.id}`}>
          {item.sizeMin}–{item.sizeMax} ({item.sizeBasis})
        </span>
      );
    }
    return (
      <span data-testid={`size-${item.id}`} className="text-muted">
        not sized yet —{" "}
        {settings?.sizingMode === "skill"
          ? "the run reports its own size through report_size (D9)"
          : "set-size is the door (manual mode)"}
      </span>
    );
  };

  return (
    <div className="stack">
      {error && <ErrorPanel error={error} onRetry={() => void load()} />}
      {notice && <Alert tone="slate">{notice}</Alert>}

      <Panel tone="slate" data-testid="queue-board">
        <FormField label="Component root">
          <Input
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            placeholder="/srv/projects/ledger"
            aria-label="component root"
          />
        </FormField>
        <Button onClick={() => void load()} disabled={!root.trim()}>
          Load the board
        </Button>
        {settings && (
          <Button onClick={() => void advance()} data-testid="advance-button">
            Advance (the explicit door, CG4)
          </Button>
        )}
        {items === null ? (
          <Alert tone="slate">No board loaded — a queue is per component root; name one.</Alert>
        ) : items.length === 0 ? (
          // D4: the empty board speaks, and says what does not happen.
          <Alert tone="slate">
            The backlog of {root.trim()} is EMPTY — appending starts nothing; nothing runs until
            advance says so.
          </Alert>
        ) : (
          <table>
            <thead>
              <tr>
                <th>pos</th>
                <th>status</th>
                <th>blueprint</th>
                <th>goal</th>
                <th>size (min–max, basis)</th>
                <th>attended</th>
                <th>actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} data-status={item.status}>
                  <td>{item.position}</td>
                  <td>
                    {/* Seven distinct renderings; the label map is asserted distinct by test. */}
                    <Pill data-testid={`status-${item.id}`}>{STATUS_LABELS[item.status]}</Pill>
                    {item.status === "blocked" && (
                      <span data-testid={`halt-${item.id}`}> — nothing starts past this row (CG2)</span>
                    )}
                  </td>
                  <td>{item.blueprint}</td>
                  <td>
                    {item.goal}
                    {item.tech ? ` · tech=${item.tech}` : ""}
                    {item.loopSlug ? ` · loop=${item.loopSlug}` : ""}
                  </td>
                  <td>{sizeCell(item)}</td>
                  <td>
                    {item.attended === null
                      ? "project's declared default decides (D5)"
                      : item.attended
                        ? "declared attended"
                        : "declared unattended"}
                  </td>
                  <td>
                    {CANCELLABLE.includes(item.status) ? (
                      <Button onClick={() => void remove(item.id)} data-testid={`remove-${item.id}`}>
                        Cancel
                      </Button>
                    ) : (
                      // D11: the running item's cancel is refused underneath;
                      // the board does not offer it.
                      <span className="text-muted">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel tone="slate" data-testid="queue-append">
        <FormField label="Blueprint (name or path — validated exactly as loop start validates it)">
          <Input value={blueprint} onChange={(e) => setBlueprint(e.target.value)} placeholder="two-step" />
        </FormField>
        <FormField label="Goal (the loop's own text, verbatim — CG3)">
          <Input value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="Surface the retry counter" />
        </FormField>
        <FormField label="Tech (optional)">
          <Input value={tech} onChange={(e) => setTech(e.target.value)} placeholder="go" />
        </FormField>
        <FormField label="With entries (semicolon-separated k=v, optional)">
          <Input value={withText} onChange={(e) => setWithText(e.target.value)} placeholder="ticket=LEDGER-9; owner=ana" />
        </FormField>
        <FormField label="Attendedness — declared, never inferred (D5/G14)">
          <Select value={attended} onChange={(e) => setAttended(e.target.value as typeof attended)}>
            <option value="default">the project's declared default decides</option>
            <option value="attended">declared attended</option>
            <option value="unattended">declared unattended</option>
          </Select>
        </FormField>
        <Button onClick={() => void append()} disabled={!blueprint.trim() || !goal.trim()}>
          Append to the queue
        </Button>
        <Alert tone="slate">
          Appending starts nothing. Under queueAdvance: ask, the gate question appears in the
          existing question/thread surface — not inline on this page.
        </Alert>
      </Panel>

      <Panel tone="slate" data-testid="queue-settings">
        <FormField label="queueAdvance (D3)">
          <Select value={advanceMode} onChange={(e) => setAdvanceMode(e.target.value)}>
            <option value="ask">ask — a person decides (the call itself)</option>
            <option value="auto">auto — a terminal event chains the next</option>
          </Select>
        </FormField>
        <FormField label="sizingMode (D7)">
          <Select value={sizingMode} onChange={(e) => setSizingMode(e.target.value)}>
            <option value="manual">manual — set-size is the door</option>
            <option value="skill">skill — report_size is the only writer (D9)</option>
          </Select>
        </FormField>
        <Button onClick={() => void saveSettings()} disabled={!root.trim()} data-testid="save-settings">
          Write both knobs
        </Button>
        {settings && (
          <p>
            queue_advance={settings.queueAdvance} · sizing_mode={settings.sizingMode}
            {settings.known ? "" : " · UNREGISTERED root — these are S24's declared defaults, never stored facts (P31)"}
          </p>
        )}
      </Panel>
    </div>
  );
}
