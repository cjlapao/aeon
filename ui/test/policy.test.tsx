import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { PolicyPage } from "../src/features/test-bench/pages/PolicyPage";
import { TestBench } from "../src/features/test-bench/TestBench";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S10's read-side window (T14), held to the four things it is for and the one
// thing it must never be.
//
// The payloads below are **T13's conformance fixtures** — the three in-force
// renders are the strings `conformance/policy/inforce.json` carries, the empty
// statement is `list.json`'s, the refusal rows are `events.json`'s. Testing the
// page against those pins means a Go-side re-wording that breaks the app shows
// up here, and not in a screenshot review.
//
// The assertions are about HONESTY, not layout: that the three states stay
// three (D14/P31), that an empty set SPEAKS (D4), that the retention triple
// travels with its rows (D15), that the daemon's sentence arrives byte-identical
// (D9), that the third answer is branched on a FIELD (SEC-033) — and that the
// page has no keyboard into the guard (N2/R4c).

/** `conformance/policy/list.json` — the D4 sentence, byte for byte. */
const EMPTY_STATEMENT =
  "no policies. default is DENY (PL5) — nothing is granted until something grants it.";

/** `conformance/policy/inforce.json` — T11's three sentences, un-collapsed. */
const ACKNOWLEDGED = "in force · acknowledged by agent:boss since 2026-10-01T09:00:00Z";
const UNACKNOWLEDGED = "in force · unacknowledged — still enforced (PL10)";
const COULD_NOT_DETERMINE = "could not determine · no evaluator for cost_cap yet (D10)";

const IN_FORCE_ANSWERS = [
  { policyID: "grant-read-tool", state: "enforced_acknowledged", render: ACKNOWLEDGED },
  { policyID: "no-bash", state: "enforced_unacknowledged", render: UNACKNOWLEDGED },
  { policyID: "per-token-budget", state: "could_not_determine", render: COULD_NOT_DETERMINE },
];

/** `conformance/policy/events.json` — the rows, plus the D15 triple. */
const EVENTS = {
  rows: [
    {
      kind: "refusal",
      subject: "agent:boss",
      policyID: "no-bash",
      detail: 'refused: agent:boss · tool_access "no-bash" · target agent:boss · source operator · no expiry',
      at: "2026-10-18T14:05:12.500Z",
    },
    {
      kind: "refusal",
      subject: "loop:s10-policy",
      policyID: "",
      detail: "refused: loop:s10-policy · no allow matched · default deny (PL5)",
      at: "2026-07-22T09:31:00Z",
    },
    // The closed set's other kind (T10's): the gate refusing something the
    // AGENT itself attempted. It is not a refusal of a stranger's call, and
    // the window renders it as its own thing.
    {
      kind: "disagreement",
      subject: "agent:boss",
      policyID: "no-bash",
      detail: 'refused: agent:boss · tool_access "no-bash" · target agent:boss · source operator · no expiry',
      at: "2026-07-22T09:31:00Z",
    },
  ],
  depth: 3,
  capDays: 90,
  oldest: "2026-07-22T09:31:00Z",
};

const LIST_ONE_RULE = {
  policies: [
    { id: "grant-read-tool", type: "tool_access", target: "agent:boss", action: "allow", source: "operator", notAfter: null },
    { id: "until-halloween", type: "command_access", target: "lane-type:review", action: "deny", source: "operator", notAfter: "2026-11-01T00:00:00Z" },
    // PL1 made visible: an Allow is operator-only, but a Deny may come from
    // anybody in the chain — so `source` is a fact the window has to show, not
    // a decoration beside the action.
    { id: "plugin-no-webhook", type: "trigger_access", target: "mcp:webhooks", action: "deny", source: "agent", notAfter: "2026-12-31T23:59:59Z" },
  ],
};

/** The reads this page may ever call — the closed set of ruling R4, widened at S35 T7 to admit `proposal.list` (TD9). */
// S35 T7 widened this from four to five: ProposedPolicyPanel also reads
// proposal.list (TD9's Fact<T> carries)  14 still every method on this
// page a read, never an authoring verb.
const THE_ALLOWED_READS = ["policy.list", "policy.why", "policy.inForce", "policy.events", "proposal.list"];

function install(responses: Record<string, unknown | ((params: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({
    responses: {
      "policy.list": { policies: [], statement: EMPTY_STATEMENT },
      "policy.inForce": { answers: [] },
      "policy.events": { rows: [], depth: 0, capDays: 90, oldest: null },
      ...responses,
    },
  });
}

/** A daemon refusal in the shape the shell actually serialises (three parts). */
function refusal(message: string): () => never {
  return () => {
    throw JSON.stringify({
      code: -31400,
      message,
      data: { what: "the policy read was refused", means: "the boundary will not render an answer it cannot vouch for", todo: "read it from a terminal", fatal: false },
    });
  };
}

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetBridge();
});

describe("the policy window", () => {
  it("states the default deny for an empty set — the sentence, never a 0 (D4)", async () => {
    install({ "policy.list": { policies: [], statement: EMPTY_STATEMENT } });
    render(<PolicyPage />);

    const empty = await screen.findByTestId("policies-empty");
    // The sentence, VERBATIM — this is the empty state, not a caption on it.
    expect(empty.textContent).toContain(EMPTY_STATEMENT);
    // And not the number a reader would read as "fine".
    expect(empty.textContent).not.toMatch(/0 polic/);
    expect(screen.queryByTestId("policies-list")).toBeNull();
  });

  it("says so when an older daemon answers an empty set without the sentence", async () => {
    install({ "policy.list": { policies: [] } });
    render(<PolicyPage />);

    const unstated = await screen.findByTestId("policies-empty-unstated");
    expect(unstated.textContent).toContain("no default-deny sentence");
    // It does not quietly borrow the current sentence to fill the gap.
    expect(unstated.textContent).not.toContain(EMPTY_STATEMENT);
  });

  it("lists the stored set with its expiry SIGNED, never skipped (PL7)", async () => {
    install({ "policy.list": LIST_ONE_RULE });
    render(<PolicyPage />);

    await screen.findByTestId("policy-grant-read-tool");
    expect(screen.getByTestId("policy-grant-read-tool-expiry").textContent).toBe("no expiry");
    expect(screen.getByTestId("policy-until-halloween-expiry").textContent).toBe("2026-11-01T00:00:00Z");
    // Identity, not content: no `spec` crosses the socket, so none can appear.
    expect(screen.getByTestId("policies-list").textContent).not.toContain("toolName");
    // PL1: an Allow is operator-only and a Deny may come from anyone in the
    // chain, so `source` is shown and an agent-authored deny is not laundered
    // into looking like an operator's.
    expect(screen.getByTestId("policy-plugin-no-webhook").textContent).toContain("source agent");
  });

  it("renders the three in-force states un-collapsed — none collapses into another (D14/P31)", async () => {
    const daemon = install({ "policy.inForce": { answers: IN_FORCE_ANSWERS } });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("inforce-ask"));
    await screen.findByTestId("inforce-list");
    expect(daemon.paramsOf("policy.inForce")).toEqual({ agent: "boss" });

    const ack = screen.getByTestId("inforce-render-grant-read-tool").textContent ?? "";
    const unack = screen.getByTestId("inforce-render-no-bash").textContent ?? "";
    const cnd = screen.getByTestId("inforce-render-per-token-budget").textContent ?? "";
    const renders = [ack, unack, cnd];
    // Each render is the daemon's string, byte for byte…
    expect(renders).toEqual([ACKNOWLEDGED, UNACKNOWLEDGED, COULD_NOT_DETERMINE]);
    // …and the three are pairwise different: no collapsing at the last mile.
    expect(new Set(renders).size).toBe(3);

    const states = IN_FORCE_ANSWERS.map(({ policyID }) =>
      screen.getByTestId(`inforce-state-${policyID}`).textContent ?? "",
    );
    expect(states).toEqual(["enforced_acknowledged", "enforced_unacknowledged", "could_not_determine"]);
    expect(new Set(states).size).toBe(3);

    // Visibly distinct: the Pill tone differs per state, so a scanning reader
    // gets three signals, not one colour with three words.
    const tones = IN_FORCE_ANSWERS.map(({ policyID }) =>
      screen.getByTestId(`inforce-state-${policyID}`).getAttribute("data-tone"),
    );
    expect(new Set(tones).size).toBe(3);
    // The acknowledged and unacknowledged lines share their head — that sharing
    // IS the claim that both bind (PL10) — while could-not-determine shares it
    // with neither.
    expect(ack.startsWith("in force · ")).toBe(true);
    expect(unack.startsWith("in force · ")).toBe(true);
    expect(cnd.startsWith("could not determine")).toBe(true);
  });

  it("gives a state this build does not know its own tone rather than guessing one (P31)", async () => {
    install({
      "policy.inForce": {
        answers: [...IN_FORCE_ANSWERS, { policyID: "future-rule", state: "enforced_something_new", render: "some future sentence (D14)" }],
      },
    });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("inforce-ask"));
    const future = await screen.findByTestId("inforce-future-rule");
    expect(future.getAttribute("data-state")).toBe("enforced_something_new");
    const tone = screen.getByTestId("inforce-state-future-rule").getAttribute("data-tone");
    const knownTones = IN_FORCE_ANSWERS.map(({ policyID }) =>
      screen.getByTestId(`inforce-state-${policyID}`).getAttribute("data-tone"),
    );
    expect(knownTones).not.toContain(tone);
    // The words still travel: the unknown state never means an invented render.
    expect(screen.getByTestId("inforce-render-future-rule").textContent).toBe("some future sentence (D14)");
  });

  it("asks in force for a loop too — exactly one party per answer (D14)", async () => {
    const daemon = install({ "policy.inForce": { answers: IN_FORCE_ANSWERS } });
    render(<PolicyPage />);

    // Neither party named: the button refuses itself rather than spending a
    // round trip the daemon would refuse.
    expect((screen.getByTestId("inforce-ask") as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("inforce-hint").textContent).toContain("Name one party");

    fireEvent.change(screen.getByTestId("inforce-loop"), { target: { value: "s10-policy" } });
    expect((screen.getByTestId("inforce-ask") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("inforce-ask"));
    await waitFor(() => expect(daemon.paramsOf("policy.inForce")).toEqual({ loop: "s10-policy" }));

    // Both named: back to refusing, because one answer is one party.
    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    expect((screen.getByTestId("inforce-ask") as HTMLButtonElement).disabled).toBe(true);
  });

  it("prints the why sentence byte-identical to the payload — no trim, no re-case, no sanitising (D9)", async () => {
    // Deliberately hostile to a formatter: leading and trailing spaces, a
    // double space, mixed case, and a BiDi control — all things a "tidying"
    // renderer eats. Go owns the rendering; this page may only print it.
    const sentence = '  refused: agent:BosS · tool_access "no-bash"  · target agent:BosS · source operator · no expiry \u202e';
    const daemon = install({ "policy.why": { allowed: false, sentence } });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("why-agent"), { target: { value: "BosS" } });
    fireEvent.change(screen.getByTestId("why-subject"), { target: { value: "bash" } });
    fireEvent.click(screen.getByTestId("why-check"));
    await screen.findByTestId("why-answer");

    // Equality, not containment: a prefix, a quote or a trim fails this.
    expect(screen.getByTestId("why-sentence").textContent).toBe(sentence);
    expect(daemon.paramsOf("policy.why")).toEqual({ type: "tool_access", agent: "BosS", tool: "bash" });
  });

  it("branches on policy.why's unimplemented FIELD, never on the sentence's prefix (SEC-033)", async () => {
    // Two payloads that read identically at the prefix and mean different
    // things. A client doing startsWith("refused: ") collapses them; the page
    // must not.
    const denial = "refused: agent:boss · no allow matched · default deny (PL5)";
    const dten = "refused: agent:boss · command_access has no evaluator yet · refusing rather than permitting (D10)";

    const daemon = install({ "policy.why": (params: unknown) => {
      const asked = (params as { type?: string }).type;
      return asked === "command_access"
        ? { allowed: false, sentence: dten, unimplemented: true }
        : { allowed: false, sentence: denial };
    } });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("why-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("why-check"));
    await screen.findByTestId("why-answer");
    expect(screen.getByTestId("why-sentence").textContent).toBe(denial);
    expect(screen.getByTestId("why-verdict").textContent).toBe("refused");
    expect(screen.queryByTestId("why-unimplemented")).toBeNull();

    fireEvent.change(screen.getByTestId("why-type"), { target: { value: "command_access" } });
    fireEvent.click(screen.getByTestId("why-check"));
    await waitFor(() => expect(screen.getByTestId("why-sentence").textContent).toBe(dten));
    // The third answer, kept apart from the denial it resembles.
    expect(screen.getByTestId("why-verdict").textContent).toBe("cannot decide yet");
    expect(screen.getByTestId("why-verdict").getAttribute("data-tone")).toBe("violet");
    expect(screen.getByTestId("why-unimplemented")).toBeTruthy();
    expect(daemon.methods().filter((m) => m === "policy.why")).toHaveLength(2);
  });

  it("shows an allowed answer as allowed, with the sentence still whole", async () => {
    const sentence = 'allowed: agent:main · tool_access "grant-read-tool" · target agent:main · source operator · no expiry';
    install({ "policy.why": { allowed: true, sentence } });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("why-agent"), { target: { value: "main" } });
    fireEvent.click(screen.getByTestId("why-check"));
    await screen.findByTestId("why-answer");
    expect(screen.getByTestId("why-verdict").textContent).toBe("allowed");
    expect(screen.getByTestId("why-sentence").textContent).toBe(sentence);
  });

  it("shows the refusal log with its depth · cap · oldest triple (D15)", async () => {
    const daemon = install({ "policy.events": EVENTS });
    render(<PolicyPage />);

    const summary = await screen.findByTestId("events-summary");
    expect(summary.textContent).toBe("depth 3 · cap 90 days · oldest 2026-07-22T09:31:00Z");
    expect(daemon.paramsOf("policy.events")).toEqual({ limit: 20 });

    expect(screen.getByTestId("event-0-detail").textContent).toBe(
      'refused: agent:boss · tool_access "no-bash" · target agent:boss · source operator · no expiry',
    );
    // An empty policyID is the default-deny row: it names no rule because no
    // rule fired. Said, not blanked.
    expect(screen.getByTestId("event-1").textContent).toContain("no policy named");
    expect(screen.getByTestId("event-1-detail").textContent).toBe("refused: loop:s10-policy · no allow matched · default deny (PL5)");
    // The other closed kind, rendered as itself (T10's disagreement record).
    expect(screen.getByTestId("event-2").textContent).toContain("disagreement");
  });

  it("says oldest none on an empty log — a stated fact, not a gap (D15)", async () => {
    install({ "policy.events": { rows: [], depth: 0, capDays: 90, oldest: null } });
    render(<PolicyPage />);

    const summary = await screen.findByTestId("events-summary");
    expect(summary.textContent).toBe("depth 0 · cap 90 days · oldest none");
    expect(screen.getByTestId("events-none")).toBeTruthy();
  });

  it("keeps a refused read inside its own section instead of blanking the page", async () => {
    // The boundary's fail-closed read (SEC-015's discipline on the read side):
    // one escaped row refuses the WHOLE list. The other three answers are still
    // true, and still have to be visible.
    install({
      "policy.list": refusal("the policy read was refused"),
      "policy.inForce": { answers: IN_FORCE_ANSWERS },
      "policy.events": EVENTS,
    });
    render(<PolicyPage />);

    const error = await screen.findByTestId("policies-error");
    expect(error.textContent).toContain("the policy read was refused");
    expect(error.textContent).toContain("the boundary will not render an answer it cannot vouch for");

    // The three sections that did answer are on the page.
    expect(await screen.findByTestId("events-summary")).toBeTruthy();
    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("inforce-ask"));
    expect(await screen.findByTestId("inforce-list")).toBeTruthy();
    expect(screen.getByTestId("policy-page")).toBeTruthy();
  });

  it("keeps a refused events read inside its own section too", async () => {
    install({ "policy.events": refusal("the event read was refused"), "policy.list": LIST_ONE_RULE });
    render(<PolicyPage />);

    expect(await screen.findByTestId("events-error")).toBeTruthy();
    expect(await screen.findByTestId("policy-grant-read-tool")).toBeTruthy();
    expect(screen.queryByTestId("events-summary")).toBeNull();
  });

  it("keeps a refused why read inside its own section, with the sentence absent", async () => {
    // A why that cannot answer prints nothing rather than a guess: there is no
    // sentence to show, and "refused:" invented here would be this page
    // authoring a verdict (D9 says the boundary owns the sentence).
    install({ "policy.why": refusal("the why read was refused"), "policy.list": LIST_ONE_RULE });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("why-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("why-check"));
    const error = await screen.findByTestId("why-error");
    expect(error.textContent).toContain("the why read was refused");
    expect(screen.queryByTestId("why-answer")).toBeNull();
    expect(screen.queryByTestId("why-sentence")).toBeNull();
    // The other sections are unaffected.
    expect(await screen.findByTestId("policy-grant-read-tool")).toBeTruthy();
  });

  it("keeps a refused in-force read inside its own section, and says so when nothing governs", async () => {    install({ "policy.inForce": refusal("the in-force read was refused") });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("inforce-ask"));
    const error = await screen.findByTestId("inforce-error");
    expect(error.textContent).toContain("the in-force read was refused");
    // The refusal is not rendered as "nothing governs this party", which would
    // read as an agent under no rules at all.
    expect(screen.queryByTestId("inforce-none")).toBeNull();
    expect(screen.getByTestId("policy-page")).toBeTruthy();
  });

  it("says nothing governs the party when the answer is empty — and points at the default deny", async () => {
    install({ "policy.inForce": { answers: [] } });
    render(<PolicyPage />);

    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("inforce-ask"));
    const none = await screen.findByTestId("inforce-none");
    expect(none.textContent).toContain("default is deny");
    expect(screen.queryByTestId("inforce-list")).toBeNull();
  });

  // THE test the slice's ruling turns on (R4c / N2 / PL15).
  it("gives every query control an accessible name, and offers a keyboard path", async () => {
    // Semantic markup, not divs-with-click-handlers: the kit's FormField wires
    // `label for`, so a screen reader names each control with the question it
    // answers, and every control here is a real button/input/select.
    install({ "policy.list": LIST_ONE_RULE });
    render(<PolicyPage />);
    await screen.findByTestId("policy-grant-read-tool");

    expect(screen.getByLabelText("agent (who is asking)")).toBeTruthy();
    expect(screen.getByLabelText("type (the closed seven)")).toBeTruthy();
    expect(screen.getByLabelText("asked about (tool / command / route …)")).toBeTruthy();
    expect(screen.getByLabelText("agent")).toBeTruthy();
    expect(screen.getByLabelText("loop")).toBeTruthy();
    // The read-only claim, said in the UI and not only in a comment.
    expect(screen.getByTestId("policy-note").textContent).toContain("Read-only");
    expect(screen.getAllByRole("button").length).toBeGreaterThan(0);
    expect(screen.getByRole("combobox")).toBeTruthy();
  });

  it("the page has no authoring affordance", async () => {
    const daemon = install({ "policy.list": LIST_ONE_RULE, "policy.inForce": { answers: IN_FORCE_ANSWERS } });
    const { container } = render(<PolicyPage />);
    await screen.findByTestId("policy-grant-read-tool");

    // No form, and nothing that could submit one.
    expect(container.querySelector("form")).toBeNull();
    expect(container.querySelectorAll("form button")).toHaveLength(0);
    expect(container.querySelectorAll("textarea")).toHaveLength(0);

    // Exercise every control the page offers, then hold the page to the closed
    // set: the allowed reads, and NOTHING else. A `policy.add` would land here.
    fireEvent.change(screen.getByTestId("why-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("why-check"));
    fireEvent.change(screen.getByTestId("inforce-agent"), { target: { value: "boss" } });
    fireEvent.click(screen.getByTestId("inforce-ask"));
    fireEvent.click(screen.getByTestId("policies-refresh"));
    fireEvent.click(screen.getByTestId("events-refresh"));
    await waitFor(() => expect(daemon.methods()).toContain("policy.inForce"));

    const sent = [...new Set(daemon.methods())];
    expect(sent.sort()).toEqual([...THE_ALLOWED_READS].sort());
    for (const method of sent) expect(THE_ALLOWED_READS).toContain(method);
  });

  it("names no authoring method anywhere under src/, and never parses a sentence", () => {
    // Split into two pairs 2026-11-04 for S50 T7. The two proposal decision
    // doors gained a SECOND named caller — the roadmap section's proposals
    // strip, which commits/rejects roadmap_idea rows through the same generic
    // human door (its guard: features/roadmap/roadmapRpc.guard.test.ts); the
    // policy authoring trio stays exactly where S45 left it. Widening the one
    // shared predicate instead would have exempted policy.add under
    // features/roadmap/ too — a weakening nobody asked for, so the ban is now
    // per pair. Built by concatenation so this file can be grepped for what it
    // forbids. S35 T7: proposal.commit/reject generalize the same rule — the
    // UI's own half of T6's backend grep (TestOnlyProposalIsSocketWritable).
    const forbiddenPolicy = ["policy." + "add", "policy." + "remove", "policy." + "set"];
    const forbiddenProposal = ["proposal." + "commit", "proposal." + "reject"];
    // Amended 2026-10-30 by S45 (PM1/PM10), the UI counterpart of the daemon-side
    // authoring guards that were amended in T4/T6 (cmd/aeon/policy_amend_test.go).
    // The rule is no longer "authoring is CLI-only": it is "authoring is a HUMAN
    // DOOR" — the person's `0600` socket. The Settings policy section
    // (src/features/settings/policy/**) is exactly that door in the app, so it is
    // the ONE surface allowed to name these methods; the write-story there is the
    // whole of PM1. Every OTHER surface under src/ — above all the test bench,
    // which is a diagnostic and reaches the same daemon — still ends at reads /
    // proposal.create, and can name none of these even by typo. The SEC-033
    // prose-branching ban below is NOT exempted: no surface may branch on prose.
    const humanDoor = (path: string): boolean => path.replace(/\\/g, "/").includes("features/settings/");
    // The proposal decision doors' second named caller (2026-11-04, S50 T7):
    // the roadmap section's proposals strip. Same human door on the socket,
    // narrowed by its own guard to kind roadmap_idea commits/rejects; the
    // policy trio above is NOT exempt here — that is why this is a second
    // predicate and not an edit of the first.
    const proposalDoor = (path: string): boolean =>
      humanDoor(path) || path.replace(/\\/g, "/").includes("features/roadmap/");
    // The SEC-033 shape is forbidden in source, not only in behaviour: a client
    // that branches on prose has already collapsed D10 into the denial. Built
    // by concatenation so THIS file can be grepped for what it forbids (the
    // same convention `internal/protocol/policy_test.go`'s needles follow).
    const proseBranching = [
      "startsWith(" + '"refused',
      "startsWith(" + "'refused",
      "indexOf(" + '"refused',
    ];
    const files: string[] = [];
    const walk = (dir: string): void => {
      for (const entry of readdirSync(dir)) {
        const path = join(dir, entry);
        if (statSync(path).isDirectory()) walk(path);
        else if (/\.(ts|tsx)$/.test(entry)) files.push(path);
      }
    };
    walk("src");
    expect(files.length).toBeGreaterThan(10);
    // Non-vacuous: the walk really reaches the bench desk (the surface that
    // must stay read/create-only) and the human door (the one that may author).
    expect(files.some((f) => f.replace(/\\/g, "/").includes("features/test-bench/"))).toBe(true);
    expect(files.some((f) => humanDoor(f))).toBe(true);

    // `src/proto/contract.ts` is generated ("Do not edit", by tools/protogen)
    // and mirrors the daemon's FULL method table as TS types — since S45 that
    // table includes the authoring methods the human door rides on. A type key
    // is a declaration, not a call site: the ban guards against a surface
    // naming a method it may call, and the bridge needs the typing to cover
    // every method, authored or not. Both scans skip it; the walk's
    // non-vacuous checks above already prove it reaches the surfaces the ban
    // exists for.
    const isGeneratedContract = (path: string): boolean => path.replace(/\\/g, "/") === "src/proto/contract.ts";
    for (const file of files) {
      if (isGeneratedContract(file)) continue;
      const body = readFileSync(file, "utf8");
      // The authoring-literal ban applies everywhere EXCEPT the authorized
      // human door; the bench and every other surface stay guarded. The
      // proposal pair's exemption is the split predicate above (settings +
      // roadmap), the policy pair's stays settings-only.
      if (!humanDoor(file)) {
        for (const needle of forbiddenPolicy) {
          expect(body.includes(needle), `${file} mentions ${needle}; authoring is a human door — only src/features/settings/** may name it (PL15, S45 PM1)`).toBe(false);
        }
      }
      if (!proposalDoor(file)) {
        for (const needle of forbiddenProposal) {
          expect(body.includes(needle), `${file} mentions ${needle}; the proposal decision doors are the human door — only src/features/settings/** and the roadmap section (S50 T7) may name them (PL15, S45 PM1)`).toBe(false);
        }
      }
      for (const needle of proseBranching) {
        expect(body.includes(needle), `${file} branches on a sentence's prefix (SEC-033)`).toBe(false);
      }
    }
    // The field the page IS allowed to branch on must exist in the generated
    // contract — the fixture is what put it there (DEF-059).
    expect(readFileSync(join("src", "proto", "contract.ts"), "utf8")).toContain("unimplemented");
  });

  it("keeps policy state out of browser storage (§4c)", async () => {
    // The rule §4c and `no-socket-in-web-layer.test.ts` guard: nothing this
    // page reads from the daemon is parked in the window's storage. Not a
    // policy row, not the last query, not the last verdict.
    const setItem = vi.spyOn(window.localStorage, "setItem");
    const setSession = vi.spyOn(window.sessionStorage, "setItem");
    install({ "policy.list": LIST_ONE_RULE });
    render(<PolicyPage />);
    await screen.findByTestId("policy-grant-read-tool");
    expect(setItem).not.toHaveBeenCalled();
    expect(setSession).not.toHaveBeenCalled();
    setItem.mockRestore();
    setSession.mockRestore();
  });

  it("renders Policy as a selected bench page", async () => {
    install({ "policy.list": LIST_ONE_RULE, "policy.inForce": { answers: [] }, "policy.events": EVENTS });
    render(<TestBench activePage="policy" />);
    expect(await screen.findByTestId("policy-page")).toBeTruthy();
    // Its own page boundary, so a render error here blanks this page and nothing
    // else — the same rule the Store tab taught.
    expect(await screen.findByTestId("policies-list")).toBeTruthy();
  });
});
