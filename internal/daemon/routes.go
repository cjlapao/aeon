package daemon

import (
	"os"
	"strconv"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
)

// # Routes from the environment, until S23 has configuration
//
// A route names a provider, a model, a credential and its prices (M12). S23
// supplies them properly; this reads them from the environment so `ask` is
// reachable before then.
//
// # The credential comes from the environment and nowhere else
//
// Never a flag — a value in `argv` is in the process list and in shell history,
// and that failure is silent. Never a committed file. And a `Route` does not
// serialise its key (`json:"-"`), so a route reaching a log line or an event
// payload cannot leak it.
const (
	EnvAnthropicKey   = "ANTHROPIC_API_KEY"
	EnvOpenAIKey      = "OPENAI_API_KEY"
	EnvAnthropicModel = "AEON_ANTHROPIC_MODEL"
	EnvOpenAIModel    = "AEON_OPENAI_MODEL"

	// Base-URL overrides. Three uses, and the third is the one that justifies
	// them existing this early:
	//
	//  1. A proxy or a gateway in front of a provider.
	//  2. S18's OpenAI-compatible path — Ollama, LiteLLM, vLLM — which is
	//     exactly "the same wire at a different address".
	//  3. **Walking the demo with no credential at all**, against a local
	//     server replaying a committed fixture. Without this, every step of
	//     S3's demo needs a real key and a real spend, which means it does not
	//     get walked.
	EnvAnthropicBaseURL = "AEON_ANTHROPIC_BASE_URL"
	EnvOpenAIBaseURL    = "AEON_OPENAI_BASE_URL"
)

// Default models and prices.
//
// **The prices are USD per million tokens and are a snapshot, not a contract.**
// They are here so a cost line says something useful on a first run; a wrong
// price produces a wrong figure, which is why `aeon ask` prints the state
// alongside and why S23 moves these into configuration a person can correct.
var defaults = map[string]struct {
	model  string
	prices *model.Prices
}{
	"anthropic": {
		model: "claude-sonnet-5",
		prices: &model.Prices{
			InputUSDPerMTok:      model.Float64Ptr(3.00),
			OutputUSDPerMTok:     model.Float64Ptr(15.00),
			CacheReadUSDPerMTok:  model.Float64Ptr(0.30),
			CacheWriteUSDPerMTok: model.Float64Ptr(3.75),
		},
	},
	"openai": {
		model: "gpt-4o",
		prices: &model.Prices{
			InputUSDPerMTok:  model.Float64Ptr(2.50),
			OutputUSDPerMTok: model.Float64Ptr(10.00),
		},
	},
}

// RoutesFromEnvironment builds a route per provider whose key is present.
//
// A provider with no key gets **no route**, rather than a route that fails on
// use: `ask` then lists what IS configured (O6), which is more useful than a
// 401 from a provider the person never meant to call.
func RoutesFromEnvironment() *protocol.MutableRoutes {
	var routes []model.Route

	for provider, env := range map[string]string{
		"anthropic": EnvAnthropicKey,
		"openai":    EnvOpenAIKey,
	} {
		key := strings.TrimSpace(os.Getenv(env))
		if key == "" {
			continue
		}
		d := defaults[provider]

		modelName := d.model
		baseURL := ""
		switch provider {
		case "anthropic":
			if v := strings.TrimSpace(os.Getenv(EnvAnthropicModel)); v != "" {
				modelName = v
			}
			baseURL = strings.TrimSpace(os.Getenv(EnvAnthropicBaseURL))
		case "openai":
			if v := strings.TrimSpace(os.Getenv(EnvOpenAIModel)); v != "" {
				modelName = v
			}
			baseURL = strings.TrimSpace(os.Getenv(EnvOpenAIBaseURL))
		}

		routes = append(routes, model.Route{
			// The id is `<provider>` so `--route anthropic` is what a person
			// types. S23 allows several routes per provider and then ids need
			// to be chosen rather than derived.
			ID:              provider,
			Provider:        provider,
			Model:           modelName,
			BaseURL:         baseURL,
			APIKey:          key,
			MaxOutputTokens: outputCapFromEnv(),
			Prices:          d.prices,
		})
	}

	// Mutable, so the UI can add a provider without editing the
	// environment and restarting the daemon. Environment-seeded routes are
	// marked as such, so a reader can tell a key that came from their shell
	// from one they typed.
	return protocol.NewMutableRoutes(routes...)
}

// outputCapFromEnv is the default output cap. Required by one provider and a
// sensible bound on both (M16).
func outputCapFromEnv() int {
	if v := strings.TrimSpace(os.Getenv("AEON_MAX_OUTPUT_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 4096
}

// DescribeRoutes is one line per configured route, for the startup banner.
//
// It names the provider and model and **never the key** — a startup log is the
// most-copied text in any bug report.
func DescribeRoutes(routes *protocol.MutableRoutes) []string {
	ids := routes.RouteIDs()
	if len(ids) == 0 {
		return []string{
			"routes    none — set " + EnvAnthropicKey + " or " + EnvOpenAIKey + ", or add one",
			"          in the app's Test bench → Providers (nothing is persisted until S4)",
		}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		r, _ := routes.Route(id)
		line := "route     " + id + "  →  " + r.Provider + "/" + r.Model
		if r.BaseURL != "" {
			// Named, because a route pointed at a local server and one pointed
			// at a provider behave identically until the bill arrives.
			line += "  at " + r.BaseURL
		}
		out = append(out, line)
	}
	return out
}
