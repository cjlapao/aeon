package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/agentsvc"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/handler"
	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/paths"
	"github.com/cjlapao/aeon/internal/plugin"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/store"
)

// agent_services.go is why cmd/aeon imports internal/agentsvc and the ONLY
// place it does (S14 T4) — plus the single production call of
// loop.SetSkillKnown (the loop package pins that count). This is AS11's
// deletability made structural: `rm -r internal/agentsvc` leaves the daemon,
// the loop, the store and the protocol building, with exactly these symbols
// as the red lines in cmd/aeon — and a daemon started from an entry point
// that never calls wireAgentServices keeps working, reporting that skill
// lanes cannot run rather than inventing an answer.
//
// Why here and not in the daemon: the daemon must build without agentsvc
// (the same rule S12 held for mcp — one entry-point import). The daemon owns
// the BRANCH (a skill step is not a model call); this file owns the WIRING
// (where cards live, what the caller's frozen policy set is, that the ask
// goes out async).

// agentCardsDir is where first-party agents publish their signed cards:
// <store dir>/agentsvc/cards/<agent>.json. A directory the operator can
// `ls` is deliberate — "which agents can I ask" must be answerable from the
// filesystem, not only from a socket (T9 makes it enumerable the same way).
func agentCardsDir() (string, error) {
	dir, err := paths.StoreDir()
	if err != nil {
		return "", fmt.Errorf("resolve the state directory for the card store: %w", err)
	}
	return filepath.Join(dir, "agentsvc", "cards"), nil
}

// wireAgentServices fills the two agent-services seams of a daemon Options:
// the pin's knowledge of what signed cards offer, and the Model branch that
// turns a skill step into a Task. Called once, from cmd/aeon serve — the
// loop package's test holds that count.
func wireAgentServices(st *store.Store, opts *daemon.Options) error {
	cardsDir, err := agentCardsDir()
	if err != nil {
		return err
	}
	iv := agentsvc.NewInvoker(st, agentsvc.OpenCardStore(cardsDir))
	// S28 T6 — publish each registered body's signed card to the card store,
	// so the board's ask (S28 T7) finds a card to invoke through. Mirrors the
	// cursor key's discipline: one stable key signs every card, so the
	// generation is idempotent across restarts (a re-serve reproduces the same
	// bytes and the ask door verifies them unchanged). Agent services are
	// optional at RUNTIME (AS1: a handler with no card runs unchanged), but a
	// store the daemon cannot write its cards to is the same store it needs for
	// everything else, so a failure is returned rather than swallowed — a
	// silently uncarded ask door would fail every question with a mystery
	// "no card" rather than the honest cause.
	if key, err := agentsvc.EnsureCardSigningKey(); err != nil {
		return fmt.Errorf("ensure the card signing key: %w", err)
	} else if err := agentsvc.GenerateCards(cardsDir, key); err != nil {
		return fmt.Errorf("generate the agent cards: %w", err)
	}
	// AS12's first-party boundary, read from the ONLY honest source: the
	// bodies compiled into this binary (the PR-reviewed set the design's
	// premise covers). A registry field is not that source — an imported
	// agent marking its own homework is exactly what S15 will attempt.
	// Exactly one SetFirstParty call in production, pinned by test.
	compiled := map[string]bool{}
	for _, name := range handler.RegisteredBodies() {
		compiled[name] = true
	}
	iv.Tasks().SetFirstParty(func(agent string) bool { return compiled[agent] })
	// S15's two gates, injected here and nowhere else — the SAME closures
	// the CLI binds through wirePluginSeams, so the daemon and `aeon agent`
	// decide plugin state with one mind. An unwired daemon (no call to this
	// function) leaves both nil: every task arrives unrefused, exactly as
	// before S15 (the inertness discipline).
	if err := wirePluginSeams(st); err != nil {
		return err
	}
	iv.Tasks().SetInvocationGate(plugin.InvocationGate())
	iv.Tasks().SetArrivalGate(plugin.ArrivalGate())
	loop.SetSkillKnown(iv.SkillKnown)
	opts.AgentInvoke = agentInvokeForStep(st, iv)
	// T10's socket seam: the seven agent.* methods forward here. The policy
	// reader bridges the SAME store the gate reads (the policy.* reads'
	// promise, carried to the invoke door: the app is never shown — nor
	// judged by — a set the runtime is not using).
	tp := agentsvc.NewTransport(st, iv, func(ctx context.Context, at time.Time) ([]policy.Policy, error) {
		rows, rj := policy.NewStore(policyRepo(st)).List(ctx, at)
		if rj != nil {
			return nil, fmt.Errorf("the policy set could not be read (%s)", rj.Reason)
		}
		return rows, nil
	})
	opts.Agents = tp.Handle
	// S21 T6: the INBOUND half of a remote call mounts on the same seam
	// discipline — built here (only cmd imports agentsvc), injected into
	// the daemon, which holds only the branch. No AEON_REMOTE_PEERS, no
	// wiring: the method then answers the absence by name (RA9's
	// off-by-default is an entry-point fact, not a daemon default).
	if err := wireRemoteCallbacks(opts, iv.Tasks()); err != nil {
		return err
	}
	// S15's ONE read for the socket. The four state verbs have no socket
	// door at all (R6) — this func, and only this func, crosses.
	opts.Plugins = pluginsHandleFor(st)
	// S37's ONE create-time preview for kind agent_import — the closure that
	// composes the three read-only plugin calls. The daemon/protocol layers
	// reach it through Table.AgentImportPreview (never importing internal/plugin).
	opts.AgentImportPreview = agentImportPreviewFor(st)
	// S34 T1's roster read — identity for every agent, a separate seam from
	// Plugins because it answers a different shape (TB1).
	opts.AgentRoster = agentRosterHandleFor(st)
	// S34 T2's grants read — what one named agent may use, already decided
	// once (mcp.Registry.GrantView, verbatim).
	opts.ToolGrants = toolGrantsHandleFor(st)
	return nil
}

// wirePluginSeams binds the two seams the plugin package speaks through:
// the ledger (the same store the daemon and the CLI both open — the rows
// are the shared truth, P2) and the card verifier (delegated to
// agentsvc.LoadCardFile HERE — the one implementation AS2 demands, reached
// through an injected loader so plugin never imports the agent-services
// layer; the sweep counts test binaries, which is why plugin could not
// have imported it even for its own fixtures). Called by wireAgentServices
// and by the CLI verbs; binding twice is harmless (the same two
// assignments), and NOT calling it is loud: every gate reads rows, and an
// unbound ledger refuses rather than guessing.
func wirePluginSeams(st *store.Store) error {
	plugin.BindStateStore(&plugin.StoreBinding{S: st})
	plugin.SetCardLoader(func(path, expectedAgent string) (*plugin.CardInfo, error) {
		c, err := agentsvc.LoadCardFile(path, expectedAgent)
		if err != nil {
			return nil, err
		}
		info := &plugin.CardInfo{Name: c.Name, Description: c.Description, Version: c.Version}
		for _, sk := range c.Skills {
			info.Skills = append(info.Skills, plugin.CardSkill{ID: sk.ID, Name: sk.Name, Description: sk.Description})
		}
		return info, nil
	})
	return nil
}

// agentInvokeForStep is the daemon's skill branch, reduced to its parts:
// read the lane's `agent/skill` target, create the delegating step row (the
// task points at it and the question hangs off it — the step that EXITS),
// take the policy set in force NOW as the task's frozen set (AS13: this is
// the creation instant, and the set is never re-derived later), and hand the
// whole ask to agentsvc.Invoke async. Nothing here waits: the step suspends
// inside T3's transaction and this returns while it has already exited (AS4,
// P2). A refusal becomes the loop's honest block reason through the ordinary
// Model-error path; the step that was refused its delegation settles `done`
// with outcome `reject` so S7's reconciler is handed an answer, not a
// mystery running row.
func agentInvokeForStep(s *store.Store, iv *agentsvc.Invoker) loop.ModelRunner {
	return func(ctx context.Context, st loop.LoopState, step blueprint.Step, prompt, _ string) (loop.ModelResult, error) {
		callee, skillID, ok := strings.Cut(step.Skill, "/")
		if !ok || callee == "" || skillID == "" {
			return loop.ModelResult{}, fmt.Errorf("the skill %q on the model step for %s is not `agent/skill` — a skill must name the agent asked", step.Skill, step.Agent)
		}
		stepRow, err := s.CreateStep(store.Scope{LoopSlug: st.Slug, ComponentRoot: st.Root},
			store.NewStep{Phase: "model", Agent: step.Agent, Attempt: 1, Status: store.StepRunning, Source: "skill-invocation"})
		if err != nil {
			return loop.ModelResult{}, fmt.Errorf("create the delegating step: %w", err)
		}
		// ONE instant for the whole creation: the set is read as of it, the
		// SkillAccess door judges as of it, and the row records it (T7's
		// EvaluatedAt is required precisely so no hidden clock re-answers
		// the question at a different moment than the freeze).
		now := time.Now()
		frozen, rj := policy.NewStore(policyRepo(s)).List(ctx, now)
		if rj != nil {
			return loop.ModelResult{}, fmt.Errorf("the policy set could not be read to freeze the task (%s) — a task created against a made-up set would be judged by rules that never existed (D12)", rj.Reason)
		}
		task, reject := iv.Invoke(ctx, agentsvc.Invocation{
			CallerLoopID: st.ID, CallerLane: st.Position.Lane, CallerAgent: step.Agent, CallerStepID: stepRow.ID,
			CalleeAgent: callee, SkillID: skillID, Text: prompt, Params: step.Params,
			Mode: agentsvc.TaskAsync, Frozen: frozen, EvaluatedAt: now,
		})
		if reject != nil {
			if err := s.SetStepStatus(stepRow.ID, store.StepDone, "reject"); err != nil {
				return loop.ModelResult{}, fmt.Errorf("settle the refused delegating step: %w", err)
			}
			return loop.ModelResult{}, fmt.Errorf("the delegation was refused: %s", reject.Detail)
		}
		return loop.ModelResult{StepID: stepRow.ID, Kind: "suspended", QuestionID: task.QuestionID}, nil
	}
}
