package mcp

// candidate.go — the candidate operations every write runs through (S46
// T2, design MG1). A change is applied to the CURRENT rows to produce
// CANDIDATE rows; the caller then Assembles the candidate with
// IncludeDisabled:true — and the ONE validator is the gate. No change
// function validates content itself (no second validator); what it refuses
// is only what is structural to the change itself: a duplicate name, a
// missing server, a revoke that would silently strip a declaration.
//
// Purity: With deep-copies its receiver, so the current rows survive every
// refusal and every success — the caller keeps both sides for the audit's
// before/after (T5) without defensive copying of its own.
//
// Closedness: Change has an unexported method, so the sum is closed to
// this package — an `mcp_grant_revoke`-style addition is a reviewed diff
// here, never an anonymous implementation from a caller.

import (
	"fmt"
	"sort"
	"strings"
)

// Change is one proposed edit to the rows. The set is closed (doc above):
// AddServer, UpdateServer, RemoveServer, SetEnabled, Declare, Revoke,
// SetCore — every human-door write (T5) and every committed proposal
// effect (T6) is exactly one of these.
type Change interface {
	applyOn(rows *Rows) error
}

// AddServer offers a new server. The name must be free ACROSS the whole
// namespace — public or any agent's private (MC8/one-namespace); the
// refusal names the server and where it already lives.
type AddServer struct {
	Server ServerRow
}

// UpdateServer replaces a server's transport and tools whole-field (the
// name is the identity — renaming is not a thing, MG5). Tier, owner and
// enabled-ness belong to other changes and are never patched here.
type UpdateServer struct {
	Server ServerRow // Name selects the row; Transport and Tools replace
}

// RemoveServer deletes a server. While declarations name its tools the
// default REFUSES, naming the declaring agents (never a silent revoke —
// T2.md law 6); CascadeDeclares is the caller saying it checked and means
// it (T5's handler offers the cascade as its own reviewed answer, and the
// proposal path, T6, never passes it).
type RemoveServer struct {
	Name            string
	CascadeDeclares bool
}

// SetEnabled offers or un-offers a server (MG4): the row, its tools and
// every declaration that names them all STAND; the server is simply not
// handed out until it is enabled again.
type SetEnabled struct {
	Name    string
	Enabled bool
}

// Declare grants an agent a public tool by qualified name (MC5: declaring
// is the grant). Idempotent: declaring what is already declared is a
// no-op, not an error (the handler reports changed:false).
type Declare struct {
	AgentKey  string
	Qualified string
}

// Revoke takes a declaration back — the narrowing direction only (T8:
// agents may propose these, never the converse). Revoking what was never
// declared is a no-op with a reason, never an error (MG5's result shape).
type Revoke struct {
	AgentKey  string
	Qualified string
}

// SetCore narrows (or restores) an agent's core grant: Core nil is the
// WHOLE core set, a non-nil pointer is the narrowing it names — including
// a pointer to an empty list, "none", which is a different fact (the two
// empties).
type SetCore struct {
	AgentKey string
	Core     *[]string
}

// With returns the candidate rows this change produces, or a refusal
// naming the missing or duplicate thing. It never consults a validator —
// Assemble the result with IncludeDisabled:true and the one validator
// speaks for both paths.
func (r Rows) With(change Change) (Rows, error) {
	out := r.deepCopy()
	if err := change.applyOn(&out); err != nil {
		return Rows{}, err
	}
	return out, nil
}

func (r Rows) deepCopy() Rows {
	out := Rows{
		Servers: append([]ServerRow(nil), r.Servers...),
		Agents:  append([]AgentRow(nil), r.Agents...),
	}
	for i := range out.Servers {
		out.Servers[i].Transport = copyTransport(r.Servers[i].Transport)
		out.Servers[i].Tools = copyTools(r.Servers[i].Tools)
	}
	for i := range out.Agents {
		out.Agents[i].Declares = append([]string(nil), r.Agents[i].Declares...)
		if r.Agents[i].Core != nil {
			core := append([]string(nil), *r.Agents[i].Core...)
			if core == nil {
				core = []string{}
			}
			out.Agents[i].Core = &core
		}
	}
	return out
}

// --- placement helpers over the rows themselves

// findServer locates a server by name across the one namespace (T1's
// UNIQUE index says at most one; the refusal texts assume the net held).
func (r *Rows) findServer(name string) int {
	for i, s := range r.Servers {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func (r *Rows) findAgent(key string) int {
	for i, a := range r.Agents {
		if a.AgentKey == key {
			return i
		}
	}
	return -1
}

func (r *Rows) declarersOf(server string) []string {
	prefix := server + "."
	var out []string
	for _, a := range r.Agents {
		for _, q := range a.Declares {
			if strings.HasPrefix(q, prefix) {
				out = append(out, a.AgentKey)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// --- the closed set

func (c AddServer) applyOn(r *Rows) error {
	if i := r.findServer(c.Server.Name); i >= 0 {
		where := "public"
		if r.Servers[i].Tier == TierPrivate {
			where = fmt.Sprintf("agent %q's private servers", r.Servers[i].AgentKey)
		}
		return fmt.Errorf("mcp: server %q already exists in %s — server names are one namespace across every tier (MC8)", c.Server.Name, where)
	}
	s := c.Server
	s.Transport = copyTransport(c.Server.Transport)
	s.Tools = copyTools(c.Server.Tools)
	r.Servers = append(r.Servers, s)
	return nil
}

func (c UpdateServer) applyOn(r *Rows) error {
	i := r.findServer(c.Server.Name)
	if i < 0 {
		return fmt.Errorf("mcp: server %q does not exist — update names an existing server (the name is the identity, MG5)", c.Server.Name)
	}
	// Whole-field replacement of the provided fields ONLY: tier, owner and
	// enabled-ness ride out untouched.
	r.Servers[i].Transport = copyTransport(c.Server.Transport)
	r.Servers[i].Tools = copyTools(c.Server.Tools)
	return nil
}

func (c RemoveServer) applyOn(r *Rows) error {
	i := r.findServer(c.Name)
	if i < 0 {
		return fmt.Errorf("mcp: server %q does not exist", c.Name)
	}
	declarers := r.declarersOf(c.Name)
	if len(declarers) > 0 && !c.CascadeDeclares {
		return fmt.Errorf("mcp: server %q is declared by %s — removing it would revoke their grants; that is never silent (declare it away first, or pass the cascade explicitly)",
			c.Name, strings.Join(declarers, ", "))
	}
	r.Servers = append(r.Servers[:i], r.Servers[i+1:]...)
	if c.CascadeDeclares {
		prefix := c.Name + "."
		for j := range r.Agents {
			var kept []string
			for _, q := range r.Agents[j].Declares {
				if !strings.HasPrefix(q, prefix) {
					kept = append(kept, q)
				}
			}
			r.Agents[j].Declares = kept
		}
	}
	return nil
}

func (c SetEnabled) applyOn(r *Rows) error {
	i := r.findServer(c.Name)
	if i < 0 {
		return fmt.Errorf("mcp: server %q does not exist — enable/disable names an existing server", c.Name)
	}
	r.Servers[i].Enabled = c.Enabled
	return nil
}

func (c Declare) applyOn(r *Rows) error {
	i := r.findAgent(c.AgentKey)
	if i < 0 {
		r.Agents = append(r.Agents, AgentRow{AgentKey: c.AgentKey})
		i = len(r.Agents) - 1
	}
	for _, q := range r.Agents[i].Declares {
		if q == c.Qualified {
			return nil // idempotent no-op; the handler reports changed:false
		}
	}
	r.Agents[i].Declares = append(r.Agents[i].Declares, c.Qualified)
	sort.Strings(r.Agents[i].Declares)
	return nil
}

func (c Revoke) applyOn(r *Rows) error {
	i := r.findAgent(c.AgentKey)
	if i < 0 {
		return nil // never declared — no-op with a reason (MG5)
	}
	var kept []string
	found := false
	for _, q := range r.Agents[i].Declares {
		if q == c.Qualified {
			found = true
			continue
		}
		kept = append(kept, q)
	}
	if !found {
		return nil
	}
	r.Agents[i].Declares = kept
	return nil
}

func (c SetCore) applyOn(r *Rows) error {
	i := r.findAgent(c.AgentKey)
	if i < 0 {
		r.Agents = append(r.Agents, AgentRow{AgentKey: c.AgentKey})
		i = len(r.Agents) - 1
	}
	if c.Core == nil {
		r.Agents[i].Core = nil
		return nil
	}
	core := append([]string(nil), *c.Core...)
	if core == nil {
		core = []string{} // "none" is a fact, not an accident of append
	}
	r.Agents[i].Core = &core
	return nil
}
