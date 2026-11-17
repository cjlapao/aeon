// proposals_agent_import.go — S37 T3: the `preview` payload the
// `proposal.create` response carries for kind `agent_import` (design §2).
//
// These are pure data. They import nothing from `internal/plugin` — the
// absent test (TestNoUnconditionalImportOfPlugin, internal/plugin/absent_test.go)
// forbids any package other than cmd/aeon from importing internal/plugin, and
// this file is in internal/protocol. The preview is composed in cmd/aeon
// (agent_admission.go, which the entry point threads in through a seam — T4),
// and this file is only the shape that shape fills. `make protogen` turns the
// typed structs into a real TS type for the desk, instead of `unknown`.
package protocol

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
)

// AgentImportPreview is `proposal.create`'s `preview` for kind agent_import
// (design §2). Advisory only (AA4): `aeon proposal commit` re-reads the
// package and re-renders the consent from the fresh read; this preview is what
// the desk shows BEFORE anyone commits, and may be stale — a stale preview is
// never authoritative.
type AgentImportPreview struct {
	Agent       string               `json:"agent"`
	Consent     string               `json:"consent"`
	CarriedDeny []AgentImportCarried `json:"carriedDeny"`
}

// AgentImportCarried is one carried Deny the package would add if committed —
// shown so a proposer sees what self-restriction the package carries, before
// the human door at commit (AA5: the preview writes nothing; commit is the
// only place the denial is ever added).
type AgentImportCarried struct {
	ID     string            `json:"id"`
	Target AgentImportTarget `json:"target"`
}

// AgentImportTarget is the carried denial's target (kind + id), rendered the
// way every surface renders it.
type AgentImportTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// --- S37 T4: the agent_import create half -----------------------------------

// agentImportPayload is the create-time payload: ONE field (AA2). The stored
// payload is the bare absolute path, re-read at create AND commit — never a
// snapshot of the package, card or prompt.
type agentImportPayload struct {
	PkgPath string `json:"pkgPath"`
}

// decodeAgentImportPayload enforces AA2: the payload is ONE field. Unknown
// fields (a snapshot of the package, a card, a prompt) are refused, never
// stored. A URL is passed through unchanged (the plugin layer's refusal to
// word, AP10) so the reason is byte-identical to the CLI's; anything else
// must be absolute (AD2). The path is then normalised (filepath.Clean) so
// create and commit read the same path.
//
// The one-field rule is enforced by counting keys, NOT by
// DisallowUnknownFields — TestNothingCallsDisallowUnknownFields (T3) forbids
// any decoder in this package from rejecting unknown fields, because a decoder
// that refuses them turns every additive change into a breaking one and this
// package's decoders never do that. A payload that decodes cleanly but carries
// a second key is refused below, exactly where DisallowUnknownFields would have
// refused it, and the refusal is this slice's own words (never the decoder's).
func decodeAgentImportPayload(raw json.RawMessage) (agentImportPayload, string) {
	var p agentImportPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, "refused: agent_import payload is {\"pkgPath\": \"<absolute path>\"} and nothing else — " + err.Error()
	}
	// AA2: the payload is exactly one field, pkgPath. The JSON is re-read as a
	// bag of keys; any bag that is not exactly the one key pkgPath is a snapshot
	// (a card, a prompt, the whole package) the record must not carry. This is
	// the package's stand-in for DisallowUnknownFields (see above) — the same
	// refusal, by the same key.
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(raw, &bag); err != nil {
		return p, "refused: agent_import payload is {\"pkgPath\": \"<absolute path>\"} and nothing else — " + err.Error()
	}
	if len(bag) != 1 || bag["pkgPath"] == nil {
		return p, "refused: agent_import payload is {\"pkgPath\": \"<absolute path>\"} and nothing else — a second field (or any field but pkgPath) is a snapshot the record must not carry"
	}
	if strings.TrimSpace(p.PkgPath) == "" {
		return p, "refused: agent_import needs pkgPath"
	}
	// A URL is the plugin layer's refusal to word (AP10) — pass it through
	// unchanged so the reason is byte-identical to the CLI's. Anything else
	// must be absolute (AD2): a relative path resolves against whoever's
	// working directory is running at the time, and create and commit are
	// not guaranteed to be the same process.
	if !strings.Contains(p.PkgPath, "://") && !filepath.IsAbs(p.PkgPath) {
		return p, "refused: pkgPath must be absolute — a relative path resolves against whoever's working directory is running at the time, and create and commit are not guaranteed to be the same process"
	}
	if !strings.Contains(p.PkgPath, "://") {
		p.PkgPath = filepath.Clean(p.PkgPath)
	}
	return p, ""
}

// validateAgentImport is S37's create-time validation: it decodes + normalises
// the payload (AA2/AD2), then calls the Table seam (AgentImportPreview) to
// get the preview + the refusal. The refusal is the plugin layer's verbatim
// text (criterion 2), never wrapped or prefixed. nil seam answers
// ErrNotWired by name (never silently permissive).
func (t *Table) validateAgentImport(ctx context.Context, raw json.RawMessage) (preview *AgentImportPreview, p agentImportPayload, refusal string, err error) {
	p, refusal = decodeAgentImportPayload(raw)
	if refusal != "" {
		return nil, p, refusal, nil
	}
	if t.AgentImportPreview == nil {
		return nil, p, "", ErrNotWired.WithWhat("agent admission is not wired — the daemon started without the entry point's AgentImportPreview seam (cmd/aeon did not set it); `aeon serve` wires it, a hand-built Table does not")
	}
	prev, refusal, err := t.AgentImportPreview(ctx, p.PkgPath)
	return prev, p, refusal, err
}
