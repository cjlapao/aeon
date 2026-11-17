// Package policyseed is the domain side of the policy.corpus seed set
// (S45 T12, PM15/PM16): the shipped command-access corpus, declared as a
// versioned seed set the S43-extra engine applies — so the operator's rule
// ("every record is in the database; system ones defined in code are
// seeded") holds of policy the way it holds of agents.
//
// The set is class GUARD (SD5): its rows add `allow`s, so it is never
// applied unasked. The engine's class gate refuses it on the startup path,
// no daemon or serve path calls it at boot, and D4 stays true by
// construction rather than by grep alone (TestStartupNeverAppliesPolicyCorpus
// in internal/daemon is the proof, not the comment).
//
// The record shape is the POLICY, not a shadow of it (PM15): Content is the
// canonical JSON of the full policy — id, type, target, action, spec,
// source operator, not_after null — and the seed identity is unchanged from
// S11: desk.SeedPolicyID(agent, pattern), `corpus.<agent>.<hash>`, the
// agent component the corpus NAME (T0 §4: S43 left seedPolicyID alone, so a
// re-seed adopts the old verb's rows instead of orphaning them).
//
// The write goes through the ONE boundary: Applier.Upsert calls
// policy.Store.AddInTx — the same ValidateForAdd, the same row builder, the
// same PL1 as Add — and pairs each stored row with its RecordAuthored audit
// row, exactly as the CLI verb does. There is no second policy write path in
// this package because there is no raw write at all.
package policyseed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/policy"
	"github.com/cjlapao/aeon/internal/seed"
)

// recordVersion is every corpus rule's version. 1 is the release the corpus
// arrived in; a rule whose CONTENT changes (pattern, verdict, anything in
// the canonical JSON) must bump its record's version, and the golden
// (testdata/corpus.versions.golden, TestCorpusVersionsBumpWithContent) is
// what makes an unversioned edit red rather than a silent drift between the
// file and the ledger. The set's version is the maximum of its records'.
const recordVersion = 1

// content is the canonical record content — the whole policy, spelled in
// lowercase keys (the wire's own vocabulary), which the fingerprint and the
// applier share so a row read back hashes to exactly the bytes the source
// wrote. not_after is a rendered string or null: null means no expiry
// (PL7's rule, restated in the seed's own format), never a far-future
// sentinel.
type content struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Target   contentTarget   `json:"target"`
	Action   string          `json:"action"`
	Spec     json.RawMessage `json:"spec"`
	Source   string          `json:"source"`
	NotAfter *string         `json:"not_after"`
}

// contentTarget is the target as the record spells it — kind and the corpus
// agent's name, the identity seedPolicyID has always carried.
type contentTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PolicyFor builds the full policy one corpus rule seeds — the same shape
// the S11 verb wrote (the desk owns the id and the spec construction, so
// this is the one definition, not a copy): id desk.SeedPolicyID(agent,
// pattern), type command_access, target agent:<corpus name>, action the
// corpus verdict, spec {command,state} with the state the action restated
// (the validator's own pairing rule), source operator (PL1's answer for an
// allow, and the seed's honesty: the operator ran the door), not_after none.
func PolicyFor(agent string, rule policy.CorpusRule) (policy.Policy, error) {
	spec, err := desk.SeedSpecJSON(rule)
	if err != nil {
		return policy.Policy{}, fmt.Errorf("policyseed: the spec for agent %q pattern %q cannot be built: %w", agent, rule.Pattern, err)
	}
	return policy.Policy{
		ID:     desk.SeedPolicyID(agent, rule.Pattern),
		Type:   policy.CommandAccess,
		Target: policy.Target{Kind: policy.AgentKind, ID: agent},
		Action: rule.Verdict,
		Spec:   spec,
		Source: policy.Operator,
	}, nil
}

// contentFor is the record content of one corpus rule — PolicyFor's shape
// in the record's canonical spelling. One builder, two readers (the source
// and the tests), so the bytes the engine hashes are the bytes the applier
// would write.
func contentFor(agent string, rule policy.CorpusRule) (content, error) {
	p, err := PolicyFor(agent, rule)
	if err != nil {
		return content{}, err
	}
	return contentFromPolicy(p), nil
}

// contentFromPolicy is the policy→content half of the round trip. The
// spec rides raw: it was built sorted-key by the desk and the canonicalizer
// re-sorts it anyway, so a re-decode is byte-identical by construction.
func contentFromPolicy(p policy.Policy) content {
	var notAfter *string
	if p.NotAfter != nil {
		s := p.NotAfter.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		notAfter = &s
	}
	spec := p.Spec
	if len(spec) == 0 {
		spec = json.RawMessage("{}")
	}
	return content{
		ID:       p.ID,
		Type:     string(p.Type),
		Target:   contentTarget{Kind: string(p.Target.Kind), ID: p.Target.ID},
		Action:   string(p.Action),
		Spec:     spec,
		Source:   string(p.Source),
		NotAfter: notAfter,
	}
}

// policyFromContent is the content→policy half: what the applier hands the
// write boundary. It arrives as any other authoring input does — bytes
// through DecodePolicy, past the closed sets and the per-type validator —
// because a record the boundary will not accept is a source fault this
// package surfaces as an error, never a raw write that skips the rule.
func policyFromContent(raw []byte) (policy.Policy, *policy.Reject) {
	return policy.DecodePolicy(raw)
}

// Source is the set's declared records — one per corpus rule, in corpus
// order (the engine sorts its decisions; the source preserves authoring
// order so the golden reads like the corpus). The code (the embedded
// corpus) is the seed source, never the truth (SD1). A corpus that will not
// parse, or a rule that cannot clear the boundary's parser on the way IN,
// is a source fault: the set would be silently incomplete, so it panics
// (the agentseed stance — broken at test time, not at a customer's apply).
func Source() []seed.Record {
	base := sourceRecords()
	out := make([]seed.Record, len(base))
	copy(out, base)
	for i := range out {
		out[i].Content = bytes.Clone(base[i].Content)
	}
	return out
}

// The embedded corpus is immutable. Validate and canonicalize it once, while
// Source still returns private bytes so a caller cannot poison another apply.
var sourceRecords = sync.OnceValue(buildSource)

func buildSource() []seed.Record {
	corpus, rej := policy.ParseCorpus(policy.EmbeddedCorpus())
	if rej != nil {
		panic("policyseed: the committed corpus will not parse — " + rej.Reason)
	}
	var out []seed.Record
	for _, a := range corpus.Agents {
		for _, r := range a.Rules {
			c, err := contentFor(a.Agent, r)
			if err != nil {
				panic("policyseed: a corpus rule cannot become a record — " + a.Agent + "/" + r.Pattern + ": " + err.Error())
			}
			// Canonical TWICE is the point: the first pass renders the
			// struct, the second re-encodes it through the generic
			// canonicaliser, so the bytes the engine stores and hashes
			// carry sorted keys (the struct's declaration order is not the
			// canonical order, and a content whose bytes are canonical
			// re-encodes identically — the contract seed.Hash rests on).
			raw, err := seed.Canonical(c)
			if err != nil {
				panic("policyseed: a record content will not canonicalise — " + a.Agent + "/" + r.Pattern + ": " + err.Error())
			}
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				panic("policyseed: a record content will not re-parse — " + a.Agent + "/" + r.Pattern + ": " + err.Error())
			}
			b, err := seed.Canonical(v)
			if err != nil {
				panic("policyseed: a record content will not canonicalise — " + a.Agent + "/" + r.Pattern + ": " + err.Error())
			}
			// The boundary accepts its own record: a content that will not
			// DecodePolicy is a broken record in the registry, so check it
			// here at the source (the engine checks JSON; this checks the
			// DOMAIN half of the contract).
			if _, drej := policyFromContent(b); drej != nil {
				panic("policyseed: a record content will not decode at the boundary — " + a.Agent + "/" + r.Pattern + ": " + drej.Reason)
			}
			out = append(out, seed.Record{
				Key:     desk.SeedPolicyID(a.Agent, r.Pattern),
				Version: recordVersion,
				Content: b,
			})
		}
	}
	if len(out) == 0 {
		panic("policyseed: the corpus produced no records — the set would be empty (rule 4)")
	}
	return out
}

// maxRecordVersion is the set version the declaration carries — the maximum
// of its records' versions, computed rather than remembered so a bumped
// record bumps the set with it (SD2's rule: the set's version moves when ANY
// record's content changes).
func maxRecordVersion() int {
	max := 0
	for _, r := range Source() {
		if r.Version > max {
			max = r.Version
		}
	}
	return max
}
