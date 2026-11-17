import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DemosPage } from "../src/features/test-bench/pages/DemosPage";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// S15 T10 — the "Agents · plugins (S15)" panel contract (QAC-33).
//
// Three ways this panel can appear to pass while lying, all pre-empted here
// (S14's QAC-25 is the template):
//
//  1. A MANAGEMENT BUTTON. R6: import/enable/disable/revoke over any socket
//     a caller can drive hands over the means to install the thing that
//     bypasses the guards. The live half reads `agent.plugins` and NOTHING
//     else; the verbs live in the greyed rows as TEXT commands, never as
//     buttons. Asserting their absence is the test.
//  2. A CONSENT UI IN DISGUISE. The consent rides as a labeled REPLAY (the
//     daemon never persists it — F-13), carrying the measured REQUESTS words,
//     with no control that grants anything (MC4: a grant is `aeon policy
//     add`, by an operator, deliberately).
//  3. A REPLAY DRESSED AS LIVE. In-flight revocation, the disable contrast,
//     the ignored first-party claim, nothing-fetches and deletability cannot
//     be conjured from a fake daemon — every such row is `replay` and cites
//     the Go test that proves its envelope.
//
// The sentences below are COPIED FROM THE WALK (docs/aeon/S15-agent-plugins/
// DEMO.md — every one printed by a real daemon or CLI on 2026-10-25) or from
// the Go source (internal/plugin/consent.go, cmd/aeon/agent.go's
// pluginsHandleFor). The panel quotes them; this test quotes the quote.

const PLUGINS = {
  plugins: [
    { name: "groot-translator", state: "disabled", source: "/tmp/aeon-s15-pkgs/groot-translator", carriedDenials: 0, firstParty: false },
    { name: "self-restraining", state: "disabled", source: "/tmp/aeon-s15-pkgs/self-restraining", carriedDenials: 1, firstParty: false },
  ],
  firstPartyCount: 25,
  counts: { disabled: 2, enabled: 0, revoked: 0 },
  note: "zero imports is the normal state (nothing was fetched — nothing here can fetch); revoked records stay readable because a life that ended is a fact, not an embarrassment (AP4)",
};

const EMPTY_PLUGINS = {
  plugins: [],
  firstPartyCount: 25,
  counts: { disabled: 0, enabled: 0, revoked: 0 },
  note: PLUGINS.note,
};

// Verbatim from the 2026-10-25 walk (DEMO §2, §3, §4) — refusals as DATA.
const BLOCKLIST_SENTENCE = 'refused: shell-helper · refused: agent:shell-helper · tool_access "no-shell-anywhere" · target all-agents · source operator · no expiry · the agent was not imported (a blocklist that only fires at first use would let the same agent be importable forever; import and use ask the same gate)';
const SELF_GRANT_SENTENCE = 'refused: policies/grant.json · policy id "self-grant-bash" · refused: Allow authored by source "import" requires an operator (PL1) — a Deny or an Ask from "import" would be accepted';
const TAMPERED_SENTENCE = 'refused: card for "groot-tampered" is refused: signature does not verify — the card was tampered with or is not from the key it claims · remedy: the card is the callee\'s own signature over its own card — re-export the package from whoever signs it (a warning would scroll past; this is a refusal)';
// Verbatim from cmd/aeon/agent.go pluginsHandleFor — the ONE live refusal
// this panel can meet: the read refuses params, naming why (a filter would
// let a reader probe for agents it was not shown).
const PARAMS_REFUSAL = "refused: agent.plugins takes no params — a filter would let a reader probe for agents it was not shown; the listing is the surface (R6: this read can neither install nor move anything)";

function rpcRefusal(what: string): () => never {
  // S14's trap, honored: the shell serialises the structured JSON-RPC error
  // as a JSON string and toRpcError parses it back — a plain Error would
  // lose `code` and dress a refusal as a fault.
  return () => {
    throw JSON.stringify({
      code: -31600, // ErrAgentRefused — refusals are DATA at this surface
      message: "the agent-services boundary refused",
      data: { what, means: "the read that cannot install", todo: "the what names the thing refused", fatal: false },
      kind: "rpc",
    });
  };
}

function pluginsDaemon(extra: Record<string, unknown | ((p: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({ responses: {
    "routes.list": [{ name: "openai-luna", model: "gpt", hasKey: true }],
    "agent.plugins": PLUGINS,
    ...extra,
  } });
}

afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("S15 plugins panel", () => {
  it("lists imported agents as rows, live, through the one read the socket carries", async () => {
    const daemon = pluginsDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run plugins demo/ }));
    const rows = await screen.findAllByTestId("plugins-step");
    await waitFor(() => {
      // One met row carries the agent rows (both names below); the params
      // row is the refused one — asserted in test 3.
      const met = rows.filter((r) => r.textContent?.includes("met"));
      expect(met.length).toBeGreaterThanOrEqual(1);
    });
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toContain("groot-translator");
    expect(joined).toContain("self-restraining");
    expect(joined).toContain("disabled");
    expect(joined).toContain("25"); // first-party compiled bodies — a count, never rows the verbs apply to
    expect(joined).toContain("/tmp/aeon-s15-pkgs/groot-translator"); // provenance rides with the row
    // The state verbs are TEXT the operator types elsewhere — greyed rows
    // carrying commands, never buttons that mutate.
    expect(joined).toContain("aeon agent enable");
    expect(daemon.methods()).toContain("agent.plugins");
  });

  it("renders the consent as REQUESTS — displayed, never approvable", async () => {
    pluginsDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run plugins replay/ }));
    const rows = await screen.findAllByTestId("plugins-replay-step");
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    // The measured consent words (internal/plugin/consent.go), replayed:
    expect(joined).toContain("REQUESTS — this agent ASKED");
    expect(joined).toContain("Nothing below is granted");
    expect(joined).toContain("not granted by this consent");
    expect(joined).toContain("Nothing above enabled anything");
    // And nothing on this page can approve anything: no button reads like a
    // state verb or an approve.
    for (const verb of [/enable/i, /disable/i, /revoke/i, /import /i, /approve/i, /grant/i]) {
      expect(screen.queryAllByRole("button", { name: verb })).toEqual([]);
    }
  });

  it("renders refusals verbatim as data — never `error (`, from the replay and from the one live door", async () => {
    // The live refusal is real: agent.plugins answered WITH params is refused
    // by the daemon's own handle (cmd/aeon/agent.go) — this fake throws what
    // that handle returns.
    const daemon = pluginsDaemon({
      "agent.plugins": (p: unknown) => {
        const params = p as Record<string, unknown> | null | undefined;
        if (params && Object.keys(params).length > 0) rpcRefusal(PARAMS_REFUSAL)();
        return PLUGINS;
      },
    });
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run plugins demo/ }));
    const liveRows = await screen.findAllByTestId("plugins-step");
    await waitFor(() => {
      const refused = liveRows.map((r) => r.textContent ?? "").join("\n");
      expect(refused).toContain("refused");
      expect(refused).toContain("takes no params");
    });
    fireEvent.click(screen.getByRole("button", { name: /Run plugins replay/ }));
    const replayRows = await screen.findAllByTestId("plugins-replay-step");
    const joined = liveRows.map((r) => r.textContent ?? "").join("\n") + "\n"
      + replayRows.map((r) => r.textContent ?? "").join("\n");
    expect(joined).toContain(BLOCKLIST_SENTENCE);
    expect(joined).toContain(SELF_GRANT_SENTENCE);
    expect(joined).toContain(TAMPERED_SENTENCE);
    expect(joined).toContain(PARAMS_REFUSAL);
    // A refusal dressed as a fault is the second truth this panel exists to
    // avoid — and the fake daemon proves the code path: refusals arrive as
    // -31600 data, and the row must say `refused`, never `error (`.
    expect(joined).not.toContain("error (");
    expect(daemon.methods()).toContain("agent.plugins");
  });

  it("an empty registry reads the normal state, plainly, with DEMO §1 as the remedy", async () => {
    pluginsDaemon({ "agent.plugins": EMPTY_PLUGINS });
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run plugins demo/ }));
    await screen.findAllByTestId("plugins-step");
    const unavailable = await screen.findByTestId("plugins-unavailable");
    const text = unavailable.textContent ?? "";
    expect(text).toContain("not runnable here");
    // AP11 honesty: absence is NORMAL — the daemon's own note rides verbatim,
    // and the remedy names §1 (import prints the consent).
    expect(text).toContain("normal state");
    expect(text).toContain("nothing was fetched");
    expect(text).toContain("docs/aeon/S15-agent-plugins/DEMO.md §1");
    expect(text).not.toContain("error (");
  });

  it("no management verb EVER leaves the page — live plus replay", async () => {
    const daemon = pluginsDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run plugins demo/ }));
    await screen.findAllByTestId("plugins-step");
    fireEvent.click(screen.getByRole("button", { name: /Run plugins replay/ }));
    await screen.findAllByTestId("plugins-replay-step");
    const sent = daemon.methods();
    for (const banned of ["agent.import", "agent.enable", "agent.disable", "agent.revoke", "agent.install", "agent.uninstall", "agent.update", "agent.invoke", "agent.task.complete"]) {
      expect(sent).not.toContain(banned);
    }
    // Only reads: agent.plugins (and the page-wide routes.list probe the
    // other panels already share). Nothing else this panel sends.
    for (const m of sent) {
      expect(["agent.plugins", "routes.list"]).toContain(m);
    }
  });

  it("every replay row carries the replay label and a grep-verified Go citation", async () => {
    pluginsDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run plugins replay/ }));
    const rows = await screen.findAllByTestId("plugins-replay-step");
    expect(rows.length).toBeGreaterThanOrEqual(7);
    for (const row of rows) {
      const t = row.textContent ?? "";
      expect(t).toContain("replay");
      expect(t).toMatch(/_test\.go|agent_gate_test|Test[A-Z]/);
    }
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    // The seven case areas of the slice, each cited to a file that exists
    // (grep-verified at writing): the three-step path and the check live in
    // check_test/import_test/state, revocation in revoke_test and
    // cmd/aeon/agent_gate_test, the claim in provenance_test, deletability
    // in absent_test.
    expect(joined).toMatch(/revoke_test|check_test|provenance_test|absent_test/);
    expect(joined).toMatch(/agent_gate_test/);
    expect(joined).toMatch(/discard|refus/i);
    expect(joined).toMatch(/disabled/i); // the §7 contrast rides as data too
  });
});
