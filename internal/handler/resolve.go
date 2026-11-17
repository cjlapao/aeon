package handler

// resolve.go — the closed dispatch registry (T10).
//
// `by-tech-tag` is a NAMED RESOLVER, not an expression: the same
// closed-registry reasoning as S8's conditions and predicates — adding a
// resolver is a code change, deliberately, because a resolver expressible in
// config is a config file that runs code (L5). Which AGENT serves a tag is the
// agents' own data now (tech.go), not a table here.

// resolvers is the closed resolver registry: names a blueprint may put in
// `agent:` that are not handlers. One entry, in code. There is no registry
// file and no config key that adds another — that absence is the product,
// not an omission (L5).
var resolvers = []string{"by-tech-tag"}

// Resolvers returns a COPY — a caller that sorted or appended to the result
// cannot reach the registry itself (D8's other half: the enumeration is
// handed out fresh, and the live list stays where it lives).
func Resolvers() []string { return append([]string(nil), resolvers...) }

// IsResolver is the real check D13 asks for: `agent: by-tech-tag` was
// `unverifiable, arrives at S13`; now the resolver registry EXISTS, so
// validation says so, and a name that is neither a handler nor this list is
// invalid, not unverifiable.
func IsResolver(name string) bool {
	for _, r := range resolvers {
		if r == name {
			return true
		}
	}
	return false
}
