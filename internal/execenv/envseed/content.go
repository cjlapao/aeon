package envseed

import (
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/seed"
)

// content is one record's canonical content — the exact columns the environment
// row holds, in the order the fingerprint hashes them, so the round trip
// source → row → fingerprint is a comparison of CONTENTS and not of columns.
// `Origin` is in the hash on purpose: a row whose origin was changed by hand is
// a divergence the ledger reports, because `system` is the claim that the set
// wrote it.
//
// `BindWorkspace` is the set's intent, not a column: it says this record is the
// one the workspace scope defaults to under the first-binding rule. Keeping it
// in the content — instead of hard-coding the slug in the applier — means the
// record says what it does, and a second record could claim the same right.
type content struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Backend       string `json:"backend"`
	Spec          string `json:"spec"`
	Origin        string `json:"origin"`
	BindWorkspace bool   `json:"bindWorkspace"`
}

// systemScrubbed is the set's one record: the environment EV6 downgrades an
// unattended legacy binding ONTO, and the one a fresh workspace is bound to
// when nothing is bound at all. Its description is the honesty line (EV1's
// sentence, served from `execenv.Honesty` rather than restated) because it is
// what a person reads in the panel before they trust it.
//
// The three choices in its spec are decisions, and each is the reason it is
// written this way and not another:
//
//   - `Passthrough` is `execenv.DefaultPassthrough()`, the allowlist the core
//     publishes — `PATH`, `HOME`, locale and `TMPDIR`. It is STORED in the row
//     rather than rebuilt at spawn, so `environment.show` reads the same list
//     the child got; and the deny list still wins inside it (EV4, PL4), so a
//     name that looks like a credential is refused even here.
//   - `Set` carries `TERM=dumb` and nothing else. A literal is for behaviour,
//     never for a credential (EV7), and `TERM=dumb` is the one that keeps a
//     child from emitting escape sequences into a log the person reads.
//   - `Home` is `real` and `Limits` are absent. A host child needs the person's
//     real `~` (git's config lives there, and a scratch HOME turns every
//     `git commit` into an anonymous one); scratch is a container-only idea
//     (D5), which `Validate` refuses on a host anyway. An absent limit means
//     the backend's own default — and a system environment that killed a
//     long-running build would be a worse default than the one it replaced.
//   - `Egress.Mode` is `unenforced`, the host's honest word for "we do not try"
//     (EV13's staged half): a host allowlist needs an egress proxy this slice
//     does not build, and claiming a mode the code does not implement is the
//     false claim EV5 forbids.
func systemScrubbed() content {
	spec := execenv.Spec{
		Env: execenv.EnvSection{
			Passthrough: execenv.DefaultPassthrough(),
			Set:         map[string]string{"TERM": "dumb"},
		},
		Home:   execenv.HomeReal,
		Egress: execenv.Egress{Mode: "unenforced"},
	}
	return content{
		Slug: SystemScrubbedSlug, Name: "Host scrubbed",
		Description:   execenv.Honesty,
		Backend:       "host-scrubbed",
		Spec:          string(execenv.Canonical(spec)),
		Origin:        "system",
		BindWorkspace: true,
	}
}

// key is the record's stable identity: its slug. Slugs are unique, immutable
// and carry no colon or space (the store's pattern), so the key is reversible
// and survives a rename of the record's display name — which is what lets a
// renamed record fingerprint the same row instead of appearing as a new one.
func key(c content) string { return c.Slug }

// contentForKey is key's inverse: the fingerprint's way back from the ledger's
// key to the row it looks up. The slug is the whole key, so the way back is the
// slug; anything else is a key this set never wrote, said as such.
func contentForKey(k string) (content, error) {
	if k == "" {
		return content{}, fmt.Errorf("envseed: a record key is the environment's slug, got an empty one")
	}
	return content{Slug: k}, nil
}

// Source is the set's declared records — never nil (the engine's rule 3), and
// one record today. The content is the canonical JSON of the record and the
// version is the set's, so ANY change here (a passthrough edit arriving from
// `execenv`, a reworded honesty line) is a version bump and a golden-test
// failure, not a silent drift in what a fresh install ships.
func Source() []seed.Record {
	recs := []content{systemScrubbed()}
	out := make([]seed.Record, 0, len(recs))
	for _, c := range recs {
		b, err := seed.Canonical(c)
		if err != nil {
			panic("envseed: the standard set's content is not canonical JSON: " + err.Error())
		}
		out = append(out, seed.Record{Key: key(c), Version: SetVersion, Content: b})
	}
	return out
}

// decode parses a record's Content into its content.
func decode(b []byte) (content, error) {
	var c content
	if err := json.Unmarshal(b, &c); err != nil {
		return content{}, fmt.Errorf("envseed: the record's content is not the standard set's shape: %w", err)
	}
	return c, nil
}
