// Package adapters holds one file per provider, and the one place that names
// them all.
//
// # M12, structurally rather than aspirationally
//
// **A third provider is one file in this directory plus one line below.**
// Nothing outside `adapters/` changes — not the client, not the protocol, not
// the CLI — which is what S18's cost estimate depends on. The test for that is
// `registry_test.go`'s fictional provider, driven end to end: if it passes
// while touching only this directory, the promise holds.
//
// # Why registration rather than a map the client reads
//
// The adapter contract lives in `model` because its signatures are made of
// `model`'s types, and `model.Client` needs an adapter. A registry in this
// package would mean `model` imports `adapters` while `adapters` imports
// `model` — an import cycle, demonstrated before this shape was chosen.
//
// So implementations register themselves, exactly as `database/sql` drivers do
// and as this project already does for `sqlite_vec`. The cost is one blank
// import somewhere outside `model`; the `init` below is the only wiring.
package adapters

import "github.com/cjlapao/aeon/internal/model"

// Provider ids, as a Route names them.
//
// Constants rather than string literals at the registration sites, so a typo is
// a compile error and so `grep` finds every use of a provider id.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

// init registers every adapter this build ships.
//
// **This is the line a new provider adds.** Adding Gemini is `gemini.go` plus
// one `RegisterAdapter` call here — and nothing else, anywhere.
func init() {
	// ONE id for OpenAI, two wire formats behind it — `/v1/chat/completions`
	// and `/v1/responses`, chosen per call from the route's `openaiEndpoint`.
	// See the dispatcher's note in openai.go for why this is not two providers.
	model.RegisterAdapter(ProviderOpenAI, func() model.Adapter { return newOpenAIDispatch() })
	model.RegisterAdapter(ProviderAnthropic, func() model.Adapter { return newAnthropic() })
}
