package daemon

// execenv_settings.go — S59 T6: the one daemon-wide number this plane owns,
// read the S44 way (declared in internal/config, reachable only through
// config.Reader, written only through `settings.set` on the human door).
//
// The value is how long a provision's container waits with no step using it
// before the reconcile sweep removes it (EV12). It lives in the vocabulary
// answer as well as in the sweep, because a person deciding whether their
// laptop will still have memory tomorrow reads it in the panel — and a panel
// that showed a number the daemon was not actually using would be the plane
// composing its own truth instead of reporting the store's.
//
// The three states matter (P3). `setting` is the ordinary case.
// `declared-default` means the declared default is what runs, which is a
// fact about the configuration, not a failure. `unreadable-default` is the
// third state: the value could not be read or would not parse, the default
// runs anyway, and the sentence says so — because a sweep interval silently
// reverted to its default is how a machine quietly fills up with containers.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/config"
)

const (
	// provisionIdleTTLSetting is the declared key (settings_decl_execenv.go).
	// Named once here and there, and the test in internal/config pins the two
	// agree — a key typed twice is a key that drifts.
	provisionIdleTTLSetting = "execenv.provisionIdleTTL"
	// defaultProvisionIdleTTL is the declared default (the slice's D7 ruling).
	// It is a constant because a fallback that itself reads a setting is a
	// circle; the declared default in the registry is the one a person edits.
	defaultProvisionIdleTTL = 24 * time.Hour

	idleTTLFromSetting   = "setting"
	idleTTLFromDeclared  = "declared-default"
	idleTTLFromUnreadabe = "unreadable-default"
)

// idleTTLAnswer is the interval with the account of where it came from.
type idleTTLAnswer struct {
	TTL        time.Duration
	From       string
	Refusal    string
	SettingKey string
}

// provisionIdleTTL reads the sweep interval. It never refuses: a daemon that
// cannot read its own setting still has to sweep, and the honest answer is the
// default with its reason attached, not an error that stops the plane.
func (s *environmentService) provisionIdleTTL(ctx context.Context) idleTTLAnswer {
	ans := idleTTLAnswer{SettingKey: provisionIdleTTLSetting}
	if s.settings == nil {
		// The storeless/configless daemon: the declared default runs, and the
		// answer says it is a default rather than letting "24h" read as a
		// decision somebody made.
		ans.TTL, ans.From = defaultProvisionIdleTTL, idleTTLFromUnreadabe
		ans.Refusal = "this daemon has no config store on the path, so the declared default (" + defaultProvisionIdleTTL.String() + ") is what the sweep will use — set " + provisionIdleTTLSetting + " once the settings store is wired"
		return ans
	}
	raw, err := s.settings.String(ctx, provisionIdleTTLSetting)
	if err != nil {
		ans.TTL, ans.From = defaultProvisionIdleTTL, idleTTLFromUnreadabe
		ans.Refusal = "the " + provisionIdleTTLSetting + " setting could not be read (" + err.Error() + "); the declared default (" + defaultProvisionIdleTTL.String() + ") is what the sweep will use"
		if s.log != nil {
			s.log.Infof("environment: %s", ans.Refusal)
		}
		return ans
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d <= 0 {
		ans.TTL, ans.From = defaultProvisionIdleTTL, idleTTLFromUnreadabe
		why := "it is not a duration"
		if err == nil {
			why = "it is not positive — a sweep interval of zero would remove a container the moment it stopped being used"
		}
		ans.Refusal = fmt.Sprintf("the %s setting is %q and %s; the declared default (%s) is what the sweep will use",
			provisionIdleTTLSetting, raw, why, defaultProvisionIdleTTL)
		if s.log != nil {
			s.log.Infof("environment: %s", ans.Refusal)
		}
		return ans
	}
	ans.TTL, ans.From = d, idleTTLFromSetting
	return ans
}

// config.Reader is the only surface a consumer takes (CS11); the alias keeps
// the service's field declaration honest about which half it may use.
type settingsReader = config.Reader
