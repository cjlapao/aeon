package loop

import "github.com/cjlapao/aeon/internal/blueprint"

type ActionKind string

const (
	ActRunModelStep  ActionKind = "run-model-step"
	ActRunCodeAction ActionKind = "run-code-action"
	ActAskGate       ActionKind = "ask-gate"
	ActSkip          ActionKind = "skip"
	ActWaiting       ActionKind = "waiting"
	ActBlocked       ActionKind = "blocked"
	ActDone          ActionKind = "done"
	// ActIdle is the resting loop's decision (S39 CR1): Decide answers idle
	// with nothing-to-do, and Run treats it as a stop. It is a loop status,
	// never a run state (DB6).
	ActIdle ActionKind = "idle"
)

type BlockedReason struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
	Slice  string `json:"slice,omitempty"`
}
type Action struct {
	Kind     ActionKind          `json:"kind"`
	Position Position            `json:"position"`
	Step     *blueprint.Step     `json:"step,omitempty"`
	Question *blueprint.Question `json:"question,omitempty"`
	SkipAs   string              `json:"skipAs,omitempty"`
	Blocked  *BlockedReason      `json:"blocked,omitempty"`
}

func block(kind, detail string) Action {
	return Action{Kind: ActBlocked, Blocked: &BlockedReason{Kind: kind, Detail: detail}}
}
