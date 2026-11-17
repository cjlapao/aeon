import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Hero, Pill } from "../../components/kit";
import {
  connect,
  disconnect,
  endpointPath,
  onClosed,
  RpcError,
  listenerUnavailable,
  toRpcError,
} from "../../lib/bridge";
import { connected, connectingTo, failed, type Connection } from "../../lib/connection";
import { ErrorPanel } from "../../components/ErrorPanel";
import { StatusPage } from "./pages/StatusPage";
import { ProvidersPage } from "./pages/ProvidersPage";
import { AskPage } from "./pages/AskPage";
import { ChatPage } from "./pages/ChatPage";
import { StorePage } from "./pages/StorePage";
import { StepPage } from "./pages/StepPage";
import { RecoveryPage } from "./pages/RecoveryPage";
import { BlueprintPage } from "./pages/BlueprintPage";
import { QueuePage } from "./pages/QueuePage";
import { RoadmapPage } from "./pages/RoadmapPage";
import { TriagePage } from "./pages/TriagePage";
import { PolicyPage } from "./pages/PolicyPage";
import { AgentsPage } from "./pages/AgentsPage";
import { McpPage } from "./pages/McpPage";
import { ProposalsPage } from "./pages/ProposalsPage";
import { AutomationPage } from "./pages/AutomationPage";
import { FilesPage } from "./pages/FilesPage";
import { AgentChatPage } from "./pages/AgentChatPage";
import { NotificationsPage } from "./pages/NotificationsPage";
import { DemosPage } from "./pages/DemosPage";
import { TabBoundary } from "../../components/TabBoundary";
import type { BenchPage } from "../../app/routes";
import { getBenchPage } from "./lib/benchRegistry";

/**
 * The app, as a **test bench**.
 *
 * # What this is, and what it is not
 *
 * **S9 builds the real app** — loops, the ladder, the thread, questions
 * answered inline, a designed experience. This is not an early draft of that.
 *
 * It is the surface each slice adds in order to exercise what it built *where
 * it will be consumed*, rather than only from a terminal. So it is labelled a
 * test bench in the UI itself: a page here is for driving the daemon and seeing
 * the truth, not for living in. That labelling is the thing that stops it
 * quietly becoming the product and having to be undone.
 *
 * The rule it follows: **a slice that adds a capability adds the surface that
 * exercises it.** S2 added the connection and the protocol version; S3 added
 * providers and ask; S4 added the store — the path, the ledger, the derived
 * awaiting query, and a document path being refused rather than normalised; S5
 * added the step — the loop running where somebody can watch it, the grant list
 * as a decision rather than a default, and four outcomes that look different.
 * S6 added suspension, saved-step reopen, answer, and compaction inspection to
 * that same Step page. S10 added Policy: a **read-only** window onto the policy
 * table, the why-sentence, the three in-force states and the refusal log — the
 * authoring half stayed in the CLI (ruling R4), so this page has no form.
 */
export function TestBench({ managed, activePage = "status" }: { managed?: { connection: Connection; reconnect: () => void };
  activePage?: BenchPage } = {}) {
  const [connection, setConnection] = useState<Connection>({ kind: "connecting" });
  // Only the failure is tracked. A boolean alongside it would be a second
  // source of truth for one fact, and the two would disagree eventually.
  const [eventsError, setEventsError] = useState<RpcError | null>(null);

  const attempt = useCallback(async () => {
    // The path first, so a failure can name it. Asking afterwards would mean
    // the one case that needs the path is the one case that cannot get it.
    const where = await endpointPath();
    setConnection(connectingTo(where));
    try {
      const negotiated = await connect();
      setConnection(
        connected(negotiated.daemonVersion, negotiated.protocolVersion, negotiated.endpoint || where),
      );
    } catch (e) {
      const err = toRpcError(e);
      setConnection(failed(err.message, where));
    }
  }, []);

  // **One session per window, and StrictMode must not double it.**
  //
  // `main.tsx` renders App inside `<StrictMode>`, which in dev runs every effect
  // mount → cleanup → mount within one commit. An effect that connected on mount
  // and disconnected on cleanup therefore asked for **two sockets per window** —
  // the daemon log's two `client connected` lines in the same second, one of
  // them replaced in the shell's AppState the moment the other arrived.
  //
  // The guard is a **ref**, not a piece of state and not a module variable,
  // because the question it answers — *has this window already begun its one
  // connect?* — lives exactly as long as the instance that owns the session.
  // React preserves refs across StrictMode's simulated unmount (measured, not
  // assumed: the second mount finds `true` and does not connect), so dev opens
  // one socket and production — where the effect simply runs once — opens one
  // socket. The guard does not depend on StrictMode existing.
  //
  // And the cleanup does **not** disconnect. A teardown the remount is about to
  // undo is not a teardown, and releasing the socket there is exactly what forced
  // the second connect. The socket is released where the window genuinely ends,
  // below — which is also the honest place to hang it: the session belongs to the
  // window, not to React's effect bookkeeping.
  const sessionStarted = useRef(false);

  useEffect(() => {
    if (managed) return;
    if (sessionStarted.current) return;
    sessionStarted.current = true;
    void attempt();
  }, [attempt, managed]);

  // Release the socket when the window really goes away.
  //
  // `pagehide` is the document's own teardown notice, so it cannot fire inside
  // StrictMode's churn — which is the whole reason the release moved off the
  // effect above. Add and remove are symmetric and idempotent, the shape a
  // double-invoked cleanup is supposed to have. No timer is involved either way:
  // I4 forbids an interval driving a read, and nothing here reads.
  //
  // **Unverified from the web layer:** whether wry fires `pagehide` when Tauri
  // destroys the window. This is best-effort on purpose — the shell's `Drop for
  // Session` is what guarantees the socket and the reader thread go away with the
  // process; this call is the web layer not leaving its side lying when it can
  // see the window closing.
  useEffect(() => {
    if (managed) return;
    const release = () => void disconnect();
    window.addEventListener("pagehide", release);
    return () => window.removeEventListener("pagehide", release);
  }, [managed]);

  // The daemon going away must make the UI go honest rather than appear to
  // work. Without this, every page would keep showing its last good state.
  useEffect(() => {
    if (managed) return;
    let unlisten: (() => void) | undefined;
    void (async () => {
      try {
        unlisten = await onClosed(async (payload) => {
          const reason = typeof payload === "string" ? payload : "the connection closed";
          const where = await endpointPath();
          setConnection(failed(reason, where));
        });
        setEventsError(null);
      } catch (e) {
        // **Not swallowed.** A window that cannot receive events can still make
        // calls, which is the most confusing possible half-working state: every
        // button works and every stream is silent. Saying so here is what makes
        // the cause findable.
        setEventsError(listenerUnavailable(e));
      }
    })();
    return () => unlisten?.();
  }, [managed]);

  const shownConnection = managed?.connection ?? connection;
  const isConnected = shownConnection.kind === "connected";
  const Container = managed ? "div" : "main";

  return (
    <Container className={managed ? "min-h-full" : "min-h-screen p-6"}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <Hero title="Aeon" subtitle="a loop-native agent harness" />
        {/* Said in the UI, not only in a comment. A test bench that looks like
            a product is one somebody builds on. */}
        <Pill tone="violet" data-testid="test-bench-badge">
          test bench
        </Pill>
      </div>

      <div className="mt-4 w-full space-y-4">
        <Alert
          tone="slate"
          title="This is a test bench, not the app"
          description="These pages exist to exercise each slice where it will be consumed, rather than only from a terminal. The designed app — loops, the ladder, the thread — is S9."
          data-testid="test-bench-note"
        />

        {/* First, because everything else depends on it: a window that cannot
            receive events makes every call work and every stream silent. */}
        {eventsError && (
          <div data-testid="events-unavailable">
            <ErrorPanel error={eventsError} />
          </div>
        )}

        {/* Each page is wrapped, so a render error in one does not blank the
            app. The Store page did exactly that on a fresh store — one null
            field, and the whole window went empty with the reason only in the
            console. The boundary SHOWS the error rather than apologising for
            it; see TabBoundary. */}
        {activePage === "demos" && isConnected && (
          <TabBoundary name="Demos">
            <DemosPage />
          </TabBoundary>
        )}
        {activePage === "status" && (
          <TabBoundary name="Status">
            <StatusPage connection={shownConnection} onReconnect={managed?.reconnect ?? (() => void attempt())} />
          </TabBoundary>
        )}
        {activePage === "providers" && isConnected && (
          <TabBoundary name="Providers">
            <ProvidersPage />
          </TabBoundary>
        )}
        {activePage === "ask" && isConnected && (
          <TabBoundary name="Ask">
            <AskPage />
          </TabBoundary>
        )}
        {activePage === "chat" && isConnected && (
          <TabBoundary name="Chat">
            <ChatPage />
          </TabBoundary>
        )}
        {activePage === "store" && isConnected && (
          <TabBoundary name="Store">
            <StorePage />
          </TabBoundary>
        )}
        {activePage === "step" && isConnected && (
          <TabBoundary name="Step">
            <StepPage />
          </TabBoundary>
        )}
        {activePage === "recovery" && isConnected && (
          <TabBoundary name="Recovery">
            <RecoveryPage />
          </TabBoundary>
        )}
        {activePage === "blueprints" && isConnected && (
          <TabBoundary name="Blueprints">
            <BlueprintPage />
          </TabBoundary>
        )}
        {/* S24 T8 — the queue bench (after blueprints, before the trust
            bench): the backlog before any of it runs. */}
        {activePage === "queue" && isConnected && (
          <TabBoundary name="Queue">
            <QueuePage />
          </TabBoundary>
        )}
        {/* S26 — the roadmap bench, the queue's sibling: intentions not yet
            shaped into loop payloads. Full CRUD over the nine roadmap.*
            verbs through the one generic bridge. */}
        {activePage === "roadmap" && isConnected && (
          <TabBoundary name="Roadmap">
            <RoadmapPage />
          </TabBoundary>
        )}
        {/* S29 — the triage bench (the user-requested CRUD): the bug
            backlog's lab door, the roadmap's sibling — a validated,
            prioritized bug hands off to a roadmap item. Full CRUD over the
            eight triage.* verbs through the one generic bridge. */}
        {activePage === "triage" && isConnected && (
          <TabBoundary name="Triage">
            <TriagePage />
          </TabBoundary>
        )}
        {activePage === "policy" && isConnected && (
          <TabBoundary name="Policy">
            <PolicyPage />
          </TabBoundary>
        )}
        {/* S34 T5 — immediately after policy, matching BENCH_PAGES' own
            order (TB11): the authorization story reads Policy → Agents → MCP. */}
        {activePage === "agents" && isConnected && (
          <TabBoundary name="Agents">
            <AgentsPage />
          </TabBoundary>
        )}
        {activePage === "mcp" && isConnected && (
          <TabBoundary name="MCP">
            <McpPage />
          </TabBoundary>
        )}
        {activePage === "proposals" && isConnected && (
          <TabBoundary name="Proposals">
            <ProposalsPage />
          </TabBoundary>
        )}
        {/* S40 T9 — the automation bench, copied from the policy branch: its
            own boundary, so a render error blanks this page and nothing else. */}
        {activePage === "automation" && isConnected && (
          <TabBoundary name="Automation">
            <AutomationPage />
          </TabBoundary>
        )}
        {activePage === "files" && isConnected && (
          <TabBoundary name="Files"><FilesPage /></TabBoundary>
        )}
        {/* Scenario mode needs no daemon, so this page renders disconnected too;
            its Live mode says so itself when the calls fail. */}
        {activePage === "agent-chat" && (
          <TabBoundary name="Agent chat"><AgentChatPage /></TabBoundary>
        )}
        {/* The toast and OS-notification services are purely local (lib/notifications.ts,
            lib/osNotify.ts) — no daemon call involved, so this needs no connection either. */}
        {activePage === "notifications" && (
          <TabBoundary name="Notifications"><NotificationsPage /></TabBoundary>
        )}

        {/* A page added through the registry (see lib/benchRegistry.ts and
            pages/registerAll.ts) renders here — no import, no JSX branch
            to add for it. Gated the same way every branch above is: a
            page that needs a connection waits for one, and none of this
            runs for an id one of the branches above already claimed. */}
        {(() => {
          const def = getBenchPage(activePage);
          if (!def) return null;
          if (def.needsConnection && !isConnected) return null;
          return (
            <TabBoundary key={def.id} name={def.label}>
              {def.render({ connected: isConnected })}
            </TabBoundary>
          );
        })()}

        {activePage !== "status" && activePage !== "agent-chat" && activePage !== "notifications"
          && getBenchPage(activePage)?.needsConnection !== false && !isConnected && (
          <Alert
            tone="amber"
            title="Not connected"
            description="There is nothing to configure or call. The Status page names the path it tried and offers Reconnect."
            data-testid="tab-needs-connection"
          />
        )}
      </div>
    </Container>
  );
}

export type { RpcError };
