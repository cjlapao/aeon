package main

// ════════════════════════════════════════════════════════════════════════════
// `aeon proposal` — the door a proposal's effect is applied through, as a
// CLIENT of the daemon (S45 T8, PM4).
//
// # One door, two roads (S45 T8)
//
// list · show · commit · reject are CLIENTS of the socket methods
// (`proposal.list · proposal.get · proposal.commit · proposal.reject` — the
// decision pair is the human door S45 T5 put on the table, audited and
// peer-stamped). selectPolicyDoor picks the road: the socket when a daemon
// answers, the SAME internal/desk code over the store when nobody is home
// (saying so on stderr first). What stays true of the old R4/R6 ruling and
// is now proven positively (cmd/aeon/policy_amend_test.go): the decision
// pair is absent from every surface a NON-person reaches — the narrow HTTP
// projection, the MCP-stdio surface, the agent enumeration, the tool
// registry, the records catalog.
//
// # Commit re-validates, then applies through the EXISTING effect path (TD4)
//
// Never a second implementation: BOTH doors run desk.Commit — digest
// verify, AlreadyApplied, Validate, before, Apply, after, the claimed flip,
// the audit row. The CLI's own `--digest` binds the commit to the digest
// the read showed (PM7.1: there is no digest-less commit); omit it and the
// CLI passes the digest its own proposal.get just read — a terminal person
// commits what they read. Since S45 T10 EVERY kind, including
// `agent_import`, commits through the selected door: the socket's daemon
// runs the same desk over the same injected seam map (the consent print is
// captured there and shipped as data — a daemon never streams), and the
// offline road runs the desk over the store, streaming the print to the
// writer this verb hands down.
//
// # Idempotent recovery, per kind (TD4)
//
// A crash between "effect applied" and "flip recorded" must not reapply the
// effect on retry — for policy_add that would hit the policy table's PRIMARY
// KEY and refuse as a store failure; for mcp_server_add it would re-trigger
// MC12's "configured twice" refusal on the SAME server it already added.
// Both *_remove kinds have the opposite shape: re-validating against a
// server/policy that is ALREADY gone would refuse for the wrong reason (the
// membership check T2 wrote exists precisely to catch a MISTAKEN remove, not
// a retried one). So `effectAlreadyApplied` runs FIRST and, if true, skips
// straight to the flip — re-validation and apply never run a second time on
// an effect already standing. seed_corpus needs no such check: its own
// per-rule skip-if-exists discipline (D4, carried from the seed runner)
// makes re-running it on a partially-seeded set naturally convergent.
import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/mcp"
	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdProposal(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("refused: `aeon proposal` needs a verb — list [--status proposed] · show <id> · commit <id> [--digest HEX] · reject <id> [--reason \"...\"]")
	}
	switch args[0] {
	case "list":
		return cmdProposalList(w, args[1:])
	case "show":
		return cmdProposalShow(w, args[1:])
	case "commit":
		return cmdProposalCommit(w, args[1:])
	case "reject":
		return cmdProposalReject(w, args[1:])
	default:
		return fmt.Errorf("refused: unknown verb %q — the verbs are list, show, commit, reject", args[0])
	}
}

// proposalCommand is TD6/TD7's pattern, shared verbatim by the CLI's own
// rendering and the socket's — commandFor in internal/protocol/proposals.go
// is the other half of this same text, kept identical by
// TestCliListMatchesSocketList rather than by eye.
func proposalCommand(id int64) string {
	return fmt.Sprintf("aeon proposal commit %d", id)
}

func cmdProposalList(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("proposal list", flag.ContinueOnError)
	fs.SetOutput(w)
	status := fs.String("status", "", "filter by status: proposed|committed|rejected|discarded (omit for every row)")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	rows, err := door.proposalList(context.Background(), *status)
	if err != nil {
		return fmt.Errorf("list proposals: %w", err)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "no proposals — the normal state when nobody has staged one")
		return nil
	}
	fmt.Fprintf(w, "%-5s %-18s %-10s %-24s %s\n", "ID", "KIND", "STATUS", "CREATED BY", "COMMAND")
	for _, p := range rows {
		fmt.Fprintf(w, "%-5d %-18s %-10s %-24s %s\n", p.ID, p.Kind, p.Status, p.CreatedByKind+":"+p.CreatedByID, p.Command)
	}
	return nil
}

// cmdProposalShow reads one proposal whole — the row, the digest a commit
// must carry back (PM7.1's token, printed so a person can bind a later
// commit to exactly this reading), and the ADVISORY preview the daemon
// attaches (what would change if this landed — S37 AA4: it gates nothing,
// the commit re-reads and re-validates). Both doors answer through
// protocol.ProposalPreviewAnswer, so the preview is the same bytes either
// way; kinds the preview means nothing for (mcp_*, agent_import) simply
// print no preview section.
func cmdProposalShow(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("proposal show", flag.ContinueOnError)
	fs.SetOutput(w)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("refused: show takes exactly ONE id")
	}
	id, err := parseProposalID(fs.Arg(0))
	if err != nil {
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	res, found, err := door.proposalGet(context.Background(), id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("refused: proposal %d does not exist", id)
	}
	p := res.Proposal
	fmt.Fprintf(w, "proposal %d\n", p.ID)
	fmt.Fprintf(w, "  kind        %s\n", p.Kind)
	fmt.Fprintf(w, "  status      %s\n", p.Status)
	fmt.Fprintf(w, "  created     %s by %s:%s\n", p.CreatedAt, p.CreatedByKind, p.CreatedByID)
	if p.DecidedAt != "" {
		fmt.Fprintf(w, "  decided     %s by %s:%s\n", p.DecidedAt, p.DecidedByKind, p.DecidedByID)
		if p.DecideReason != "" {
			fmt.Fprintf(w, "  reason      %s\n", p.DecideReason)
		}
	}
	if p.AppliedError != "" {
		fmt.Fprintf(w, "  apply error %s\n", p.AppliedError)
	}
	fmt.Fprintf(w, "  payload     %s\n", string(p.Payload))
	fmt.Fprintf(w, "  digest      %s\n", res.Digest)
	fmt.Fprintf(w, "  commit with %s --digest %s\n", proposalCommand(p.ID), res.Digest)
	if res.Preview != nil {
		pv := res.Preview
		if pv.Target != "" {
			fmt.Fprintf(w, "  target      %s\n", pv.Target)
		}
		if pv.Describe != "" {
			fmt.Fprintf(w, "  describe    %s\n", pv.Describe)
		}
		if len(pv.Agents) > 0 {
			fmt.Fprintf(w, "  agents      %s\n", strings.Join(pv.Agents, ", "))
		}
		for _, a := range pv.InForce {
			fmt.Fprintf(w, "  in force    %-24s %s\n", a.PolicyID, a.Render)
		}
		if pv.Note != "" {
			fmt.Fprintf(w, "  note        %s\n", pv.Note)
		}
	}
	return nil
}

func cmdProposalReject(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("proposal reject", flag.ContinueOnError)
	fs.SetOutput(w)
	reason := fs.String("reason", "", "optional — rejecting loosens nothing, TD5, so no reason is required")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("refused: reject takes exactly ONE id")
	}
	id, err := parseProposalID(fs.Arg(0))
	if err != nil {
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	// S45 T5/T8: both doors are CALLERS of the one desk body (PM4) — the
	// socket's stamps the deciding actor from the peer, the offline door
	// names {user, cli}; two spellings of one person (fact 6). The CLI's
	// reject reason stays OPTIONAL (TD5's CLI ruling, golden-pinned); the
	// required-reason rule is the SOCKET door's, enforced at protocol +
	// adapter — over the wire that reads as a refusal, and it prints
	// verbatim. A reject runs no effect (PM7.5): nothing precedes the flip.
	if _, err := door.proposalReject(context.Background(), id, *reason); err != nil {
		return fmt.Errorf("%s", err.Error())
	}
	fmt.Fprintf(w, "proposal %d rejected\n", id)
	return nil
}

func cmdProposalCommit(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("proposal commit", flag.ContinueOnError)
	fs.SetOutput(w)
	digestFlag := fs.String("digest", "", "the digest proposal.get (or a prior commit prompt) showed for THIS row — "+
		"PM7.1's binding: commit what you read. Omit it to bind to the digest this command's own read returns")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("refused: commit takes exactly ONE id")
	}
	id, err := parseProposalID(fs.Arg(0))
	if err != nil {
		return err
	}
	door, err := selectPolicyDoor(os.Stderr)
	if err != nil {
		return err
	}
	ctx := context.Background()
	res, found, err := door.proposalGet(ctx, id)
	if err != nil {
		return err
	}
	row := res.Proposal
	if !found {
		return fmt.Errorf("refused: proposal %d does not exist", id)
	}
	if row.Status != "proposed" {
		// The CLI's own status sentence, kept byte-identical for the door
		// this file has always answered with (the golden's claim); the
		// desk's winner-naming sentence is the socket's.
		return fmt.Errorf("refused: proposal %d is %s, not proposed — a decided row is immutable history", id, row.Status)
	}

	// PM7.1 made executable: the digest is READ (proposal.get, both doors)
	// and handed BACK to the commit door — the flag when the operator
	// brought the one they were shown, the read's own value otherwise. No
	// digest-less commit exists at either door; the re-check against the
	// stored payload is desk.Commit's, and a drift refuses verbatim.
	digest := strings.TrimSpace(*digestFlag)
	if digest == "" {
		digest = res.Digest
	}

	// S45 T10: NO kind-specific routing is left in this verb. `agent_import`
	// commits through the selected door like every other kind (PM4: one
	// implementation at each road — the socket's daemon runs the same desk
	// over the same injected seam map; the offline road runs the desk here).
	// The verb hands the door its own writer: the consent print (S37 AA4)
	// reaches the person on the direct road, and comes back as data
	// (warnings) on the socket road — either way the SAME bytes.
	var cres protocol.ProposalCommitResult
	cres, err = door.proposalCommit(ctx, id, digest, w)
	if err != nil {
		return fmt.Errorf("%s", err.Error())
	}
	// PM7.2's before/after — the target's in-force answer either side of
	// the effect the door just applied, Render() sentences VERBATIM (D14).
	// They ride stderr like every mandatory operator line here: stdout
	// stays the answer the goldens pinned, the daemon's words say what
	// changed. Both doors carry the same fields (the desk computes them in
	// its single commit window; the socket ships them as the wire's
	// before/after), so both print the same bytes.
	printCommitWindow(cres)
	fmt.Fprintf(w, "proposal %d committed\n", id)
	return nil
}

// printCommitWindow renders the commit door's before/after views and its
// advisory warnings to os.Stderr — sentences verbatim, additive lines,
// empty views silent (kinds with no policy target answer nothing about
// in-force, and a header with no rows under it is the confident wrong
// answer D9 exists to stop).
func printCommitWindow(res protocol.ProposalCommitResult) {
	for _, v := range []struct {
		label string
		view  protocol.InForceView
	}{{"before", res.Before}, {"after", res.After}} {
		if v.view.Target == "" && len(v.view.Answers) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s · %s\n", v.label, v.view.Target)
		for _, a := range v.view.Answers {
			fmt.Fprintf(os.Stderr, "  %-24s %s\n", a.PolicyID, a.Render)
		}
	}
	for _, warn := range res.Warnings {
		fmt.Fprintln(os.Stderr, warn)
	}
}

// cliProposalActor's job moved with S45 T8 to the doors themselves
// (policy_client.go): the direct door decides as {user, cli} — a person at
// a terminal, named the shape a CLI-only door always named it, OS_USERNAME-
// free and deliberately so — while the socket door's actor is the kernel
// peer the daemon stamps (table.ActorFor). The CLI reads no actor from a
// flag at either door, which is why no actor literal lives in this file
// any more.

func parseProposalID(s string) (int64, error) {
	var id int64
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &id); err != nil || id <= 0 {
		return 0, fmt.Errorf("refused: %q is not a positive proposal id", s)
	}
	return id, nil
}

// --- the seams (S45 T1 Phase B) -------------------------------------------------
//
// Three effects are REGISTERED into desk.Seams rather than moved into
// internal/desk, and the reason is a package boundary, not a preference:
// internal/mcp and internal/plugin are closed to the desk (S34's MC11
// deletability pin names its importers, and the desk's own
// TestDeskImportsNothingForbidden keeps it out of that list), the MCP effects
// re-shape an `mcp.Config` and the import effect reaches the plugin layer and
// prints a consent. This file is additionally the ONLY one the sibling guard
// (TestOnlyProposalIsSocketWritable) admits as a caller of `mcp.SaveConfig`,
// which is why the registration lives here rather than in `serve.go`: the seam
// values are package-level, so `aeon proposal commit` and `aeon serve` link the
// ONE registry and cannot drift apart — the daemon's seam list and the CLI's
// are the same map literal.
//
// An unwired seam is never a silent skip: the registry refuses the kind by name
// and names the half nobody supplied (`desk.Registry.Missing`).
//
// Behaviour is what S35 T5 wrote, moved verbatim out of the two switches this
// file used to hold — the goldens in policy_golden_test.go are the proof.

// S46 T6 — the three MCP effects write ROWS through T5's doors. The file
// mirror (load → reshape → mcp.SaveConfig) is retired: the tables are the
// configuration, THE validator is one (Rows.With + Assemble through
// admin.candidateValidated — the create door and the commit door run the
// same function the registry holds, TD3), and the audit's actor is the
// COMMITTING person (the desk's CommitActor — never the proposer, whose
// identity stays in the row's created_by_*).

// mcpAddParamsFromPayload is the one decode: the stored mcp_server_add
// payload as T5's params. No second shape, no second validator.
func mcpAddParamsFromPayload(payload []byte) (protocol.MCPServerAddParams, error) {
	var in mcpServerAddPayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return protocol.MCPServerAddParams{}, fmt.Errorf("refused: mcp_server_add payload is not valid JSON: %v", err)
	}
	return protocol.MCPServerAddParams{
		Name: in.Server.Name, Tier: in.Tier, Agent: in.Agent,
		Transport: protocol.MCPTransportInput{Type: in.Server.Transport.Type, Command: in.Server.Transport.Command, Args: in.Server.Transport.Args, Env: in.Server.Transport.Env, EnvFrom: in.Server.Transport.EnvFrom, URL: in.Server.Transport.URL},
		Tools:     toolsInputFromConfigs(in.Server.Tools),
		Enabled:   in.Enabled,
	}, nil
}

func toolsInputFromConfigs(ts []mcp.ToolConfig) []protocol.MCPToolInput {
	if ts == nil {
		return nil
	}
	out := make([]protocol.MCPToolInput, 0, len(ts))
	for _, tl := range ts {
		out = append(out, protocol.MCPToolInput{Name: tl.Name, Rerun: tl.Rerun, Effect: tl.Effect})
	}
	return out
}

func seamValidateMCPServerAdd(ctx context.Context, payload []byte) error {
	p, err := mcpAddParamsFromPayload(payload)
	if err != nil {
		return err
	}
	return withMCPStore(ctx, func(a *mcpAdmin) error {
		// A private add owns its agent: the seam refuses a ghost by NAME
		// here, because Apply would refuse it there — TD3 says the create
		// door answers everything the commit door will.
		if p.Tier == "private" && strings.TrimSpace(p.Agent) != "" {
			if _, rerr := a.resolveAgentSlugForProposal(ctx, p.Agent); rerr != nil {
				return rerr
			}
		}
		all, rerr := a.st.McpRows(ctx)
		if rerr != nil {
			return rerr
		}
		refusal, werr := a.candidateValidated(all, mcp.AddServer{Server: serverRowFromAddParams(p)})
		if werr != nil {
			return werr
		}
		if refusal != "" {
			return fmt.Errorf("%s", refusal)
		}
		return nil
	})
}

// seamAlreadyAppliedMCPServerAdd answers "is this exact server, under this
// exact definition, already configured". A same-name server with a
// DIFFERENT definition answers false, so validate+apply run and the
// validator's "configured twice" refusal speaks the genuine conflict
// loudly — this gate never papers over it (TD4's discipline).
func seamAlreadyAppliedMCPServerAdd(ctx context.Context, payload []byte) (bool, error) {
	p, err := mcpAddParamsFromPayload(payload)
	if err != nil {
		return false, err
	}
	var applied bool
	err = withMCPStore(ctx, func(a *mcpAdmin) error {
		all, rerr := a.st.McpRows(ctx)
		if rerr != nil {
			return rerr
		}
		for _, r := range all.Servers {
			if r.Name != p.Name {
				continue
			}
			agent := ""
			if r.AgentSlug != nil {
				if k, ok := a.mapKey(*r.AgentSlug); ok {
					agent = k
				}
			}
			identical := r.Tier == p.Tier && agent == p.Agent &&
				r.TransportType == p.Transport.Type && r.Command == p.Transport.Command && r.URL == p.Transport.URL &&
				r.Args == mcpJSONArrayOrEmpty(p.Transport.Args) && r.Env == mcpJSONArrayOrEmpty(p.Transport.Env) &&
				toolRowsMatch(r.Tools, p.Tools)
			applied = identical
			return nil
		}
		return nil
	})
	return applied, err
}

// seamApplyMCPServerAdd is T5's door, not a file write: actor = the
// committing person (CommitActor — absent is a refusal, never a
// placeholder), validator re-run inside, one transaction for rows+audit.
func seamApplyMCPServerAdd(ctx context.Context, _ io.Writer, payload []byte) error {
	p, err := mcpAddParamsFromPayload(payload)
	if err != nil {
		return err
	}
	actor, ok := desk.CommitActor(ctx)
	if !ok || actor.Kind == "" || actor.ID == "" {
		return fmt.Errorf("refused: mcp_server_add commits need the deciding person on the context — the audit stamps them, and a placeholder is forbidden (S46 T5/T6)")
	}
	return withMCPStore(ctx, func(a *mcpAdmin) error {
		_, refusal, werr := a.writeServerAdd(ctx, p, actor.Kind, actor.ID)
		if werr != nil {
			return werr
		}
		if refusal != "" {
			return fmt.Errorf("%s", refusal)
		}
		return nil
	})
}

func seamValidateMCPServerRemove(ctx context.Context, payload []byte) error {
	var in mcpServerRemovePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return fmt.Errorf("refused: mcp_server_remove payload is not valid JSON: %v", err)
	}
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("refused: mcp_server_remove needs a non-empty name")
	}
	return withMCPStore(ctx, func(a *mcpAdmin) error {
		all, rerr := a.st.McpRows(ctx)
		if rerr != nil {
			return rerr
		}
		found := false
		for _, r := range all.Servers {
			if r.Name == in.Name {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("refused: no MCP server called %q — nothing to remove", in.Name)
		}
		if in.CascadeDeclares {
			return nil
		}
		var holders []string
		prefix := in.Name + "."
		for _, d := range all.Declares {
			if strings.HasPrefix(d.Qualified, prefix) {
				key := d.AgentSlug
				if k, ok := a.mapKey(d.AgentSlug); ok {
					key = k
				}
				holders = append(holders, key)
			}
		}
		if len(holders) > 0 {
			sort.Strings(holders)
			return fmt.Errorf("refused: %q is declared by %s — a proposal to remove it names cascadeDeclares:true to mean the revokes, or the declarations are revoked first (S46, never a silent revoke)", in.Name, strings.Join(holders, ", "))
		}
		return nil
	})
}

// seamAlreadyAppliedMCPServerRemove is the remove shape of TD4: the server
// is gone from the rows, and so are any declarations naming it (a half-
// cascaded world is NOT already applied).
func seamAlreadyAppliedMCPServerRemove(ctx context.Context, payload []byte) (bool, error) {
	var in mcpServerRemovePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return false, err
	}
	var applied bool
	err := withMCPStore(ctx, func(a *mcpAdmin) error {
		all, rerr := a.st.McpRows(ctx)
		if rerr != nil {
			return rerr
		}
		gone := true
		for _, r := range all.Servers {
			if r.Name == in.Name {
				gone = false
			}
		}
		if !gone {
			applied = false
			return nil
		}
		for _, d := range all.Declares {
			if strings.HasPrefix(d.Qualified, in.Name+".") {
				applied = false
				return nil
			}
		}
		applied = true
		return nil
	})
	return applied, err
}

func seamApplyMCPServerRemove(ctx context.Context, _ io.Writer, payload []byte) error {
	var in mcpServerRemovePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return err
	}
	actor, ok := desk.CommitActor(ctx)
	if !ok || actor.Kind == "" || actor.ID == "" {
		return fmt.Errorf("refused: mcp_server_remove commits need the deciding person on the context — the audit stamps them, and a placeholder is forbidden (S46 T5/T6)")
	}
	return withMCPStore(ctx, func(a *mcpAdmin) error {
		_, refusal, werr := a.writeServerRemove(ctx, protocol.MCPServerRemoveParams{Name: in.Name, CascadeDeclares: in.CascadeDeclares}, actor.Kind, actor.ID)
		if werr != nil {
			return werr
		}
		if refusal != "" {
			return fmt.Errorf("%s", refusal)
		}
		return nil
	})
}

// --- mcp_grant_revoke (S46 T6, the seventh kind) ---------------------------------------

// seamValidateMCPGrantRevoke is the create door AND the commit door — one
// function (TD3). Three refusals, all at create, never carried inert
// (S35 TD3): no agent named / agent does not resolve (naming it, never
// guessing), no qualified tool, and the pair NOT declared — a proposal to
// revoke nothing is nonsense, and nonsense does not reach a human.
func seamValidateMCPGrantRevoke(ctx context.Context, payload []byte) error {
	in, err := decodeMCPGrantRevokePayload(payload)
	if err != nil {
		return err
	}
	return withMCPStore(ctx, func(a *mcpAdmin) error {
		slug, serr := a.resolveAgentSlugForProposal(ctx, in.Agent)
		if serr != nil {
			return serr
		}
		all, rerr := a.st.McpRows(ctx)
		if rerr != nil {
			return rerr
		}
		if contains(a.currentDeclares(all, slug), in.Qualified) {
			return nil
		}
		return fmt.Errorf("refused: mcp_grant_revoke — %s does not declare %q; a proposal to revoke nothing is refused at create (S35 TD3), never carried inert to a human", in.Agent, in.Qualified)
	})
}

// seamAlreadyAppliedMCPGrantRevoke answers TD4's question for the
// narrowing kind: the pair is no longer declared (crash between effect and
// flip ⇒ the retry completes the flip instead of tripping the validator
// for the wrong reason).
func seamAlreadyAppliedMCPGrantRevoke(ctx context.Context, payload []byte) (bool, error) {
	in, err := decodeMCPGrantRevokePayload(payload)
	if err != nil {
		return false, err
	}
	var applied bool
	err = withMCPStore(ctx, func(a *mcpAdmin) error {
		all, rerr := a.st.McpRows(ctx)
		if rerr != nil {
			return rerr
		}
		slug, serr := a.resolveAgentSlugForProposal(ctx, in.Agent)
		if serr != nil {
			// An unresolvable agent is not "already applied" — validate
			// will refuse it by name.
			return nil
		}
		applied = !contains(a.currentDeclares(all, slug), in.Qualified)
		return nil
	})
	return applied, err
}

func seamApplyMCPGrantRevoke(ctx context.Context, _ io.Writer, payload []byte) error {
	in, err := decodeMCPGrantRevokePayload(payload)
	if err != nil {
		return err
	}
	actor, ok := desk.CommitActor(ctx)
	if !ok || actor.Kind == "" || actor.ID == "" {
		return fmt.Errorf("refused: mcp_grant_revoke commits need the deciding person on the context — the audit stamps them, and a placeholder is forbidden (S46 T5/T6)")
	}
	return withMCPStore(ctx, func(a *mcpAdmin) error {
		_, refusal, werr := a.writeGrantRevoke(ctx, protocol.MCPGrantRevokeParams{Agent: in.Agent, Qualified: in.Qualified}, actor.Kind, actor.ID)
		if werr != nil {
			return werr
		}
		if refusal != "" {
			return fmt.Errorf("%s", refusal)
		}
		return nil
	})
}

func decodeMCPGrantRevokePayload(payload []byte) (mcpGrantRevokePayload, error) {
	var in mcpGrantRevokePayload
	if err := json.Unmarshal(payload, &in); err != nil {
		return in, fmt.Errorf("refused: mcp_grant_revoke payload is not valid JSON: %v", err)
	}
	if strings.TrimSpace(in.Agent) == "" {
		return in, fmt.Errorf("refused: mcp_grant_revoke needs the agent whose declaration is taken back")
	}
	if strings.TrimSpace(in.Qualified) == "" {
		return in, fmt.Errorf("refused: mcp_grant_revoke needs qualified as SERVER.TOOL")
	}
	return in, nil
}

// resolveAgentSlugForProposal resolves the payload's agent against the
// agent table and refuses by NAME — never a guess (Behaviour 4: a payload
// written in the name era still names a real agent or still dies naming
// it).
func (a *mcpAdmin) resolveAgentSlugForProposal(ctx context.Context, agent string) (string, error) {
	ag, found, err := a.st.AgentByName(agent)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("refused: mcp proposal names agent %q — no such agent (names are never guessed; propose with a known agent)", agent)
	}
	return ag.Slug, nil
}

// seamAlreadyAppliedAgentImport is Q14's honest answer: there is no reliable way
// to tell "my earlier commit imported it" from "the CLI imported it" — both
// leave a live plugin_agent_live row. The gate must NOT answer true (that would
// skip the criterion-4 refusal and flip the row without re-running the import);
// it answers false so validate+apply run and the unique index refuses the
// second commit with its own text, the same refusal a second CLI import
// already produces.
func seamAlreadyAppliedAgentImport(_ context.Context, _ []byte) (bool, error) {
	return false, nil
}

// seamApplyAgentImport is AD4's one body, two callers: it re-reads the disk
// (AA3) and prints the fresh consent to the writer the door handed it (AA4) —
// the reason `desk.Effect.Apply` carries an io.Writer at all.
func seamApplyAgentImport(_ context.Context, w io.Writer, payload []byte) error {
	var p struct {
		PkgPath string `json:"pkgPath"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("refused: stored agent_import payload is not {\"pkgPath\":…}: %v", err)
	}
	// The stored path may predate the absolute-path rule (a row proposed
	// before AD2); re-check at the commit door, never a relative resolve.
	if !strings.Contains(p.PkgPath, "://") && !filepath.IsAbs(p.PkgPath) {
		return fmt.Errorf("refused: stored pkgPath %q is not absolute — the proposal predates the absolute-path rule; discard it and propose again", p.PkgPath)
	}
	return importAgentPackage(w, p.PkgPath)
}

// deskEffects is the single effect registry the CLI's doors run through — the
// registry `aeon serve` hands the daemon too, because it is this package's one
// value and the daemon links the same package. Every per-kind decision (what to
// validate, what "already applied" means, what to write) now has exactly one
// definition, in internal/desk or in the seams above.
var deskEffects = desk.New(
	desk.Deps{
		// The boundary arrives as an opener over policy.go's own
		// `policyBoundary` variable — read per call, so the fixtures that
		// point the verbs at a temp store or a spy repo still do, through
		// this registry. The clock is the process clock: the desk reads the
		// one it is handed and never invents an instant (D5).
		Boundary: openDeskBoundary,
		Clock:    time.Now,
	},
	deskSeams(),
)

// loadCurrentMCPConfig lives in mcp_admin.go since S46 T3 (the tables are
// the source; the signature and the (zero, false, nil) folding of MC11's
// legitimate absence are unchanged — the same three-state discipline S34 T3
// established for mcp.status, now over rows).
