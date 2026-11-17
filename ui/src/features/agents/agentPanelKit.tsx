import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider, ToastProvider, ToastViewport } from "@cjlapao/ui-kit";
import { LiveController, type LiveState } from "../../lib/live";
import { APP_LOCALES } from "../../lib/i18n";
import { AgentsScreen } from "./AgentsScreen";

// The fixtures and the moves the agent panel's test files share: the row as `agent.view` answers it,
// the window the table shows, the ledger, a mount of the whole screen, and the verbs a person uses
// to open and leave a panel. (No `vi.mock` here: a mock has to sit in the test file that needs it.)

export const liveCurrent: LiveState = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "1.0",
  protocolVersion: 1,
  generation: 1,
};

/**
 * The row, as `agent.view` answers it — the whole twenty-four keys, with values
 * chosen so a lost field cannot hide: `version` is "7" rather than the window's
 * "1.0.0", and the four dispatch columns carry text no control edits.
 */
export const VIEW_ROW = {
  id: 12,
  slug: "reviewer-plus",
  name: "Reviewer Plus (from agent.view)",
  description: "Reviews pull requests and posts findings",
  functionality: "read, list, write",
  metadata: '{"owner":"cjlapao"}',
  prompt: "You are a careful code reviewer.",
  skills: '[{"id":"diff","name":"Read diffs"}]',
  version: "7",
  internal: false,
  avatarType: "minion",
  avatarColor: "blue",
  avatarMood: "idle",
  signature: "",
  certificate: "",
  signed: false,
  tools: '["read","list","write"]',
  temperature: 0.2,
  handoff: "{}",
  policyAgent: "policy-reviewer",
  bodyHash: "sha256:1f4b0c9d",
  origin: "user",
  createdAt: "2026-10-30T09:00:00Z",
  updatedAt: "2026-10-30T09:00:00Z",
};

/** A first-party row, for the read-only rule (CU6). */
export const INTERNAL_ROW = { ...VIEW_ROW, slug: "golang-engineer", internal: true, origin: "system" };

/** The same two slugs as the WINDOW answers them: nine columns, different words. */
export const WINDOW_ROWS = [
  {
    slug: "reviewer-plus",
    name: "Reviewer Plus (from agent.rows)",
    description: "A summary line, and all the window carries",
    version: "1.0.0",
    internal: false,
    signed: false,
    origin: "user",
    createdAt: "2026-10-30T09:00:00Z",
    updatedAt: "2026-10-30T09:00:00Z",
  },
  {
    slug: "golang-engineer",
    name: "Golang Engineer",
    description: "First-party content",
    version: "1.0.0",
    internal: true,
    signed: false,
    origin: "system",
    createdAt: "2026-10-30T09:00:00Z",
    updatedAt: "2026-10-30T09:00:00Z",
  },
];

export const LEDGER = {
  changes: [
    {
      id: 3,
      op: "edit",
      slug: "reviewer-plus",
      summary: "edit reviewer-plus (9 content fields)",
      actorKind: "user",
      actorId: "1000",
      occurredAt: "2026-11-06T10:00:00Z",
    },
  ],
  page: 1,
  pageSize: 20,
  total: 1,
  slug: "reviewer-plus",
};

export function rowsAnswer() {
  return { agents: WINDOW_ROWS, page: 1, pageSize: 20, total: WINDOW_ROWS.length, sort: "name", dir: "asc", q: "" };
}

/**
 * A refusal the way the daemon actually sends one.
 *
 * `protocol.Error.WithWhat` writes the sentence into `data.what` and leaves
 * `message` the code's own generic label (`internal/protocol/errors.go:115` and
 * the `Data` field at :68; `ErrAgentRefused` is −31600 with
 * `Message: "the agent-services boundary refused"`). S73 T8's walk found this
 * file's first fixtures handing the sentence in `message` instead — a friendlier
 * shape than the wire's, which would have let the page pass a test while showing
 * a person a generic headline. So the fixtures wear the envelope here, and the
 * assertions below are only worth what the envelope makes them worth.
 */
export function refusalEnvelope(sentence: string): string {
  return JSON.stringify({
    code: -31600,
    message: "the agent-services boundary refused",
    kind: "rpc",
    data: {
      what: sentence,
      means: "an agent ask is judged by doors with names — the refusal above is that door's own words",
      todo: "the `what` names the door and the thing refused",
    },
  });
}

/** The row `agent.view` answers with, or a refusal naming the slug it has no row for. */
export function viewAnswer(params: unknown) {
  const slug = (params as { slug?: string }).slug;
  if (slug === "reviewer-plus") return VIEW_ROW;
  if (slug === "golang-engineer") return INTERNAL_ROW;
  throw refusalEnvelope(`refused: ${slug} has no agent row`);
}

export function mount(canWrite = true) {
  return render(
    <I18nProvider locales={APP_LOCALES}>
      <ToastProvider>
      <ToastViewport />
      <MemoryRouter>
        <AgentsScreen controller={new LiveController()} live={liveCurrent} canWrite={canWrite} />
      </MemoryRouter>
      </ToastProvider>
    </I18nProvider>,
  );
}

/** Open one row's menu, so a verb can be chosen the way a person chooses it. */
export async function openRowMenu(slug: string) {
  fireEvent.click(screen.getByTestId(`agents-row-menu-${slug}`));
  return screen.findByRole("menu");
}

export async function chooseRowVerb(slug: string, label: RegExp) {
  const menu = await openRowMenu(slug);
  fireEvent.click(within(menu).getByRole("menuitem", { name: label }));
}

/** Leave the agent's page by its way back — the move that has to ask about a draft. */
export function closePanel() {
  fireEvent.click(screen.getByTestId("agent-back"));
}

/** Choose one of the row body's tabs — the way a person clicks it. */
export async function chooseTab(label: RegExp) {
  fireEvent.click(await screen.findByRole("tab", { name: label }));
}

/**
 * A field's control, found by the label written over it.
 *
 * The matcher is a regex and not the plain word because a required field's label
 * carries a decoration star — `<label>Name*</label>` — and an exact string match
 * would fail on exactly the one field (Name) that the create form insists on.
 */
export function field(label: string): HTMLInputElement | HTMLTextAreaElement {
  return screen.getByLabelText(new RegExp(`^${label}\\*?$`)) as HTMLInputElement | HTMLTextAreaElement;
}

