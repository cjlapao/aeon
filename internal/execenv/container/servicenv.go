package container

// The names a loop's app container receives for its services (EV14). They are
// DERIVED: the operator declares `name` and `port`, the daemon spells the
// variables. A spec that could name its own variable is a spec that could set
// `PATH`, `LD_PRELOAD` or one of the credential names the deny list protects,
// and the whole point of the derived shape is that no typed string reaches the
// child's environment as a name.

import (
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/execenv"
)

// serviceNamePattern is the declared service name's charset. It is narrow on
// purpose: it must survive upper-casing into a shell-variable name, appear in
// `docker --network-alias` (which allows [a-zA-Z0-9][a-zA-Z0-9-_]*), and be
// unambiguous when the derived names are compared for collisions.
var serviceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ServiceHostVar / ServicePortVar are the derived names for one service.
func ServiceHostVar(name string) string { return servicePrefix + strings.ToUpper(name) + "_HOST" }
func ServicePortVar(name string) string { return servicePrefix + strings.ToUpper(name) + "_PORT" }

const servicePrefix = "AEON_SERVICE_"

// derivedNameRefusal is the collision check the spec asks for. Two services
// whose names differ only in a character that upper-casing or the `-`→`_`
// mapping swallows would write the SAME variable, and the second one to be
// derived would silently win — a loop talking to the wrong database is not a
// failure anyone would suspect on a spelling.
func derivedNameRefusal(services []execenv.Service) *execenv.Refusal {
	seen := map[string]int{}
	names := map[int]string{}
	for i, svc := range services {
		if !serviceNamePattern.MatchString(svc.Name) {
			return execenv.NewRefusal(execenv.RefusalSpecInvalid,
				"the service name "+svc.Name+" is not a service name",
				"a service name becomes a shell-variable name and a network alias, so it must be a lowercase letter "+
					"followed by up to 31 lowercase letters, digits or hyphens (EV14)",
				"rename the service, for example db, queue-1, search-a")
		}
		for _, v := range []string{ServiceHostVar(svc.Name), ServicePortVar(svc.Name)} {
			// A different ENTRY writing the same variable — including a second
			// entry with the identical name, which is the same ambiguity wearing
			// a spelling.
			if other, ok := seen[v]; ok && other != i {
				return execenv.NewRefusal(execenv.RefusalSpecInvalid,
					"the services "+names[other]+" and "+svc.Name+" would both write "+v,
					"the derived names are the only way the app reaches a service; two services writing one variable "+
						"means one of them is unreachable and nothing says which (EV14)",
					"rename one of the services so its upper-cased name differs")
			}
			seen[v] = i
			names[i] = svc.Name
		}
	}
	return nil
}

// ServicePortCheck is the one numeric fact the DRIVER needs (it dials the port
// in a health probe). Everything else about a service declaration — name
// charset, duplicates, image, health shape — is refused once at bind time by
// execenv.Validate; a second copy here would be a second source of truth that
// can drift.

func intString(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
