package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/policy"
)

// validate.go — the per-kind payload validators, one per proposal kind, each
// calling EXISTING code (TD3). Both doors call the SAME entry on the same
// payload — the socket's create (through `daemon.Options.ValidateProposal`,
// which `cmd/aeon serve` wires to `Registry.ValidateProposal`) and the CLI's
// `aeon proposal commit` — so a rule fixed once is fixed at both doors, never
// two divergent checks (S35 criterion 4, pinned by
// TestDeskValidateIsTheCreateValidator).
//
// The two MCP kinds' validators are NOT here: they read and re-shape the MCP
// config, whose package is closed to this one (MC11's importer pin), so they
// arrive as `Seams` from `cmd/aeon/proposal.go` — the same function, one
// definition, registered rather than copied.

// MaxOpenProposalsPerAgent is the per-caller cap on OPEN (still `proposed`)
// proposals, S45 T7 (PM9) behaviour 4: a runaway loop must not bury the
// desk, so the records service's two propose tools refuse the 21st open
// row by a caller already holding this many. It is a named constant in the
// package that owns proposals — not a number hidden inside a switch — and
// the boundary test (internal/daemon) pins 20 accepted / 21 refused naming
// the limit. It counts the CALLER's own open rows (kind+id of the stamped
// actor): a proposal is inert, so the cap protects the desk's queue, not
// enforcement, and one caller cannot spend another's room.
const MaxOpenProposalsPerAgent = 20

// validateAgentImport is the `agent_import` registry entry (S37 T4) — the
// commit-time re-validation (TD3) of the same payload create validates. It
// re-runs the payload decode (AA2/AD2: one field, absolute, no URL beyond the
// plugin layer's own AP10 wording) so the commit door refuses the same
// malformed payload the create door would, and never stores a snapshot. The
// create-time preview (the consent + carried denials the desk shows) is a
// separate seam (`Table.AgentImportPreview`); this entry is the shared
// validation, not the preview.
func validateAgentImport(_ context.Context, payload []byte) error {
	var p struct {
		PkgPath string `json:"pkgPath"`
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("refused: agent_import payload is {\"pkgPath\": \"<absolute path>\"} and nothing else — %v", err)
	}
	if strings.TrimSpace(p.PkgPath) == "" {
		return fmt.Errorf("refused: agent_import needs pkgPath")
	}
	if !strings.Contains(p.PkgPath, "://") && !filepath.IsAbs(p.PkgPath) {
		return fmt.Errorf("refused: pkgPath must be absolute — a relative path resolves against whoever's working directory is running at the time, and create and commit are not guaranteed to be the same process")
	}
	return nil
}

// validatePolicyAdd is the boundary's own answer, run without writing: the
// closed sets, PL1's authorship check and the per-type validator, all through
// `policy.ValidateForAdd` — the function `Add` itself calls, so the create door
// cannot accept a row the commit door's boundary would refuse.
func (d Deps) validatePolicyAdd(_ context.Context, payload []byte) error {
	var p policy.Policy
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("refused: policy_add payload is not a policy row: %v", err)
	}
	if _, rej := policy.ValidateForAdd(p); rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}
	return nil
}

// validatePolicyRemove checks the one thing the CLI's own remove does not treat
// as a refusal (removing an unknown id is a benign no-op there) — but a
// PROPOSAL to remove something that is not there is exactly the kind of
// create-time mistake TD3 exists to catch before a human spends a commit on it.
func (d Deps) validatePolicyRemove(ctx context.Context, payload []byte) error {
	var in PolicyRemovePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return fmt.Errorf("refused: policy_remove payload is not valid JSON: %v", err)
	}
	if strings.TrimSpace(in.ID) == "" {
		return fmt.Errorf("refused: policy_remove needs a non-empty id")
	}
	b, closeFn, err := OpenBoundary(d.Boundary)
	if err != nil {
		return err
	}
	defer closeFn()
	rows, rej := b.ListPolicies(ctx, d.now())
	if rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}
	for _, p := range rows {
		if p.ID == in.ID {
			return nil
		}
	}
	return fmt.Errorf("refused: no policy called %q — nothing to remove", in.ID)
}

// validateSeedCorpus refuses at create the two shapes D4 welded shut: no
// agents named at all, and an agent the corpus does not carry. The second is
// the FILTER's answer, word for word the one the CLI verb prints, because both
// run `ResolveSeedTargets` — one definition, never a divergent copy (TD3).
func (d Deps) validateSeedCorpus(_ context.Context, payload []byte) error {
	var in SeedCorpusPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return fmt.Errorf("refused: seed_corpus payload is not valid JSON: %v", err)
	}
	if len(in.Agents) == 0 {
		return fmt.Errorf("refused: seed_corpus needs at least one agent — never the whole corpus by omission (D4)")
	}
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		return fmt.Errorf("%s", rej.Reason)
	}
	if _, err := ResolveSeedTargets(corpus, in.Agents); err != nil {
		return err
	}
	return nil
}
