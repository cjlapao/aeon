package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cjlapao/aeon/internal/model"
	_ "github.com/cjlapao/aeon/internal/model/adapters" // registers the provider adapters
	"github.com/cjlapao/aeon/internal/protocol"
)

// The client verbs: `ping`, `version`, `watch`, `debug subscriptions`.
//
// Each goes through `internal/protocol`'s client, which performs `initialize`
// first, always — the same handshake a third party would. A CLI that shortcut
// it because it knows the daemon would prove nothing about the protocol.

// renderError prints an Aeon error in its three parts (T10, C9).
//
// **The three parts are the point.** A user seeing `-31002` has to go and look
// it up; a user seeing what happened, what it means and what to do can act.
// Anything that is not an Aeon error prints plainly, because inventing a
// "means" for a dial failure would be fiction.
func renderError(w io.Writer, err error) {
	var e *protocol.Error
	if !errors.As(err, &e) {
		fmt.Fprintf(w, "aeon: %v\n", err)
		return
	}

	fmt.Fprintf(w, "aeon: %s  (%d)\n", e.Message, e.Code)
	fmt.Fprintf(w, "  what   %s\n", e.Data.What)
	fmt.Fprintf(w, "  means  %s\n", e.Data.Means)
	fmt.Fprintf(w, "  todo   %s\n", e.Data.Todo)
	if e.Data.Fatal {
		// A client that cannot tell "retry" from "reconnect" will do the wrong
		// one, so the marker is rendered rather than left in the payload.
		fmt.Fprintf(w, "  fatal  yes — this connection is unusable\n")
	}
}

// protocolVersionFlag is shared by every client verb.
//
// It is a DEBUGGING flag, not a feature: it exists so the demo can prove the
// daemon refuses a client asking for too much, which is otherwise unobservable
// until the first real version bump.
func addProtocolVersionFlag(fs *flag.FlagSet) *int {
	return fs.Int("protocol-version", 0,
		"ask for this protocol version (0 = whatever the daemon supports). "+
			"A debugging flag: use it to see a refusal.")
}

// --- ping --------------------------------------------------------------------

func cmdPing(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	start := time.Now()
	if err := c.Ping(); err != nil {
		return err
	}
	// Milliseconds with one decimal: a round trip on a unix socket is well
	// under a millisecond, and `0ms` would read as "did not happen".
	fmt.Fprintf(w, "pong · %.1fms\n", float64(time.Since(start).Microseconds())/1000)
	return nil
}

// --- version -----------------------------------------------------------------

func cmdClientVersion(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The client's own build first, which needs no daemon — through S1's
	// printVersion, so the Go toolchain line survives. It is there because cgo
	// is on and the toolchain is part of what the binary IS, and S1's test
	// spec asserts it.
	printVersion(w)

	c, err := dial(*want)
	if err != nil {
		// A client that cannot reach the daemon still knows its own version,
		// and saying so is more useful than failing entirely.
		fmt.Fprintf(w, "  daemon  not connected — %v\n", err)
		return nil
	}
	defer c.Close()

	v, err := c.Version()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  daemon  %s\n", v.DaemonVersion)
	// "negotiated" is not decoration: a bare number reads as a constant, and a
	// constant proves nothing about whether a handshake happened.
	fmt.Fprintf(w, "  protocol  %d (negotiated)\n", v.ProtocolVersion)
	return nil
}

// --- watch -------------------------------------------------------------------

// repeatableString collects a flag given more than once, and also splits on
// commas, because both forms are what people type.
type repeatableString []string

func (r *repeatableString) String() string { return strings.Join(*r, ",") }
func (r *repeatableString) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*r = append(*r, part)
		}
	}
	return nil
}

func cmdWatch(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(w)
	var kinds repeatableString
	fs.Var(&kinds, "kinds", "event kinds to receive; repeatable or comma-separated (`loop.*`)")
	comp := fs.String("component", "", "only events for this component root")
	after := fs.Int64("after", -1, "resume after this seq (-1 = from now)")
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var afterPtr *uint64
	if *after >= 0 {
		v := uint64(*after)
		afterPtr = &v
	}

	filter := protocol.Filter{ComponentRoot: *comp, Kinds: kinds}
	sub, err := c.Subscribe(filter, afterPtr)
	if err != nil {
		return err
	}

	// The subscription line, printed on connect.
	//
	// **A client that silently waits is indistinguishable from one that
	// subscribed to nothing** — which is exactly the failure O1 spends an error
	// code avoiding, so `watch` says what it asked for and where it starts.
	fmt.Fprintf(w, "watching %s · kinds=%s component=%s · from seq %d\n",
		sub.SubscriptionID, describeKinds(kinds), describeComponent(*comp), sub.From)
	if afterPtr != nil && sub.From > *afterPtr+1 {
		// The honest-gap case: the daemon could not serve the whole cursor.
		fmt.Fprintf(w, "  note: resumed at %d, not %d — the daemon no longer retains that far "+
			"back, so %d events are missing\n", sub.From, *afterPtr+1, sub.From-(*afterPtr+1))
	}
	fmt.Fprintf(w, "  Ctrl-C to stop\n\n")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				return nil
			}
			fmt.Fprintf(w, "%6d  %-22s %s\n", ev.Seq, ev.Kind, compactPayload(ev))
		case <-c.Closed():
			fmt.Fprintln(w, "\nthe daemon closed the connection")
			return nil
		case <-stop:
			fmt.Fprintln(w)
			return nil
		}
	}
}

// plural keeps "1 connection" from reading as "1 connections". A small thing,
// and the kind of small thing that makes output look unmaintained.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func describeKinds(kinds []string) string {
	if len(kinds) == 0 {
		return "*"
	}
	return strings.Join(kinds, ",")
}

func describeComponent(p string) string {
	if p == "" {
		return "*"
	}
	return p
}

// compactPayload renders an event's payload on one line.
//
// One line per event because `watch` is read in a terminal while something else
// is happening: a pretty-printed payload would scroll the thing being watched
// off the screen.
func compactPayload(ev protocol.Event) string {
	var parts []string
	if ev.ComponentRoot != "" {
		parts = append(parts, "component="+ev.ComponentRoot)
	}
	if len(ev.Payload) > 0 {
		var fields map[string]any
		if err := json.Unmarshal(ev.Payload, &fields); err == nil {
			for _, key := range sortedKeys(fields) {
				parts = append(parts, key+"="+renderValue(fields[key]))
			}
		} else {
			parts = append(parts, string(ev.Payload))
		}
	}
	return strings.Join(parts, " ")
}

func sortStrings(s []string) { sort.Strings(s) }

// renderValue prints a decoded JSON value the way a person needs to read it.
//
// # Why this exists: a pid in scientific notation
//
// Found by running `aeon watch` and looking at the output:
// `peerPid=1.130661e+06`. Decoding into `map[string]any` makes every JSON
// number a float64, and `%v` on a large float64 is exponential — so the one
// field whose entire purpose is to be copied into a `kill` command was
// unusable. An integral float is therefore printed as an integer.
func renderValue(v any) string {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
	case map[string]any, []any:
		// Go's `%v` renders a map as `map[]`, which appeared verbatim in the
		// walked demo as `filter=map[]`. The value came off the wire as JSON
		// and a reader is thinking in JSON, so it goes back as JSON.
		if raw, err := json.Marshal(t); err == nil {
			return string(raw)
		}
	}
	return fmt.Sprintf("%v", v)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// --- debug -------------------------------------------------------------------

func cmdDebug(w io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("debug needs a subcommand: `subscriptions`, `roundtrip`, `hammer` or `receipts`")
	}
	switch args[0] {
	case "subscriptions":
		return cmdDebugSubscriptions(w, args[1:])
	case "roundtrip":
		return cmdDebugRoundtrip(w, args[1:])
	case "hammer":
		// S4's load generator, for the demo's concurrency step.
		return cmdDebugHammer(w, args[1:])
	case "goroutines":
		return cmdDebugGoroutines(w, args[1:])
	case "receipts":
		return cmdDebugReceipts(w, args[1:])
	default:
		return fmt.Errorf("unknown debug subcommand %q; the known ones are `subscriptions`, `hammer`, "+
			"`roundtrip` and `receipts`", args[0])
	}
}

func cmdDebugReceipts(w io.Writer, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("debug receipts needs one step id")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("%q is not a positive step id", args[0])
	}
	c, err := dial(0)
	if err != nil {
		return err
	}
	defer c.Close()
	var st protocol.StepView
	if err := c.Call(protocol.MethodStepShow, protocol.StepShowParams{ID: id}, &st); err != nil {
		return err
	}
	if len(st.Receipts) == 0 {
		fmt.Fprintf(w, "step %d has no unsafe tool receipts\n", id)
		return nil
	}
	for _, r := range st.Receipts {
		completed := "—"
		if r.CompletedAt != nil {
			completed = *r.CompletedAt
		}
		fmt.Fprintf(w, "%s  %s  digest %s  started %s  completed %s  result captured %t\n",
			r.Tool, r.ToolUseID, r.ArgDigest, r.StartedAt, completed, r.ResultCaptured)
	}
	return nil
}

// cmdDebugGoroutines exists so AS4 can be SHOWN, not only tested (S14 T3):
// `aeon debug goroutines | grep -c agentsvc` answering 0 is the demo of
// suspension-is-exiting, read straight off the daemon's stacks.
func cmdDebugGoroutines(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("debug goroutines", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.DebugGoroutinesResult
	if err := c.Call(protocol.MethodDebugGoroutines, nil, &res); err != nil {
		return err
	}
	// The stacks go to stdout raw — this command's consumer is a pipe into
	// grep, and a formatted summary would grep differently from the truth.
	fmt.Fprint(w, res.Stacks)
	return nil
}

// cmdDebugSubscriptions exists so D6 can be SHOWN, not only tested.
//
// A registry that empties is a claim; a command that prints it empty is a
// demonstration.
func cmdDebugSubscriptions(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("debug subscriptions", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	var res protocol.DebugSubscriptionsResult
	if err := c.Call(protocol.MethodDebugSubscriptions, nil, &res); err != nil {
		return err
	}

	if len(res.Subscriptions) == 0 {
		// Saying it is empty, rather than printing nothing. Printing nothing
		// looks like the command failed.
		fmt.Fprintf(w, "no subscriptions (%s initialised)\n", plural(res.Connections, "connection"))
		return nil
	}
	fmt.Fprintf(w, "%s, %s initialised\n\n",
		plural(len(res.Subscriptions), "subscription"), plural(res.Connections, "connection"))
	fmt.Fprintf(w, "%-12s %-10s %-24s %s\n", "SUBSCRIPTION", "CONN", "KINDS", "NEXT SEQ")
	for _, s := range res.Subscriptions {
		fmt.Fprintf(w, "%-12s %-10s %-24s %d\n",
			s.SubscriptionID, s.ConnID, describeKinds(s.Filter.Kinds), s.Next)
	}
	return nil
}

// --- debug roundtrip ---------------------------------------------------------

// cmdDebugRoundtrip reads a transcript, runs it through an adapter and back,
// and reports whether it survived.
//
// **The human-visible form of T8's fidelity test.** A person can see, with
// their own transcript, that a signed `thinking` block comes back byte for
// byte — and the signature's length, because a signature that silently lost
// its padding is the failure that surfaces in S7's recovery.
func cmdDebugRoundtrip(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("debug roundtrip", flag.ContinueOnError)
	fs.SetOutput(w)
	file := fs.String("file", "", "a transcript as JSON: either a message array or a stored envelope")
	provider := fs.String("provider", "anthropic", "which adapter to round-trip through")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required: `aeon debug roundtrip --file transcript.json`.\n" +
			"  Produce one with `aeon ask --show-transcript`")
	}

	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}

	msgs, err := parseTranscript(raw)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "transcript  %d message(s) from %s\n", len(msgs), *file)

	// Validate first: a transcript no provider would accept is worth knowing
	// about before asking whether it round-trips.
	if res := model.ValidateTranscript(msgs); !res.OK {
		fmt.Fprintf(w, "validation  REJECTED — %s", res.Reason)
		if res.Index >= 0 {
			fmt.Fprintf(w, " (message %d)", res.Index)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "validation  ok")
	}

	// Report every signature's length before and after, since that is the
	// thing most likely to change without anybody noticing.
	before := signatureLengths(msgs)
	if len(before) == 0 {
		fmt.Fprintln(w, "signatures  none in this transcript")
	}
	for i, length := range before {
		fmt.Fprintf(w, "signature   block %d: %d bytes\n", i, length)
	}

	// The round trip: canonical → wire → canonical, through the real adapter.
	adapter, err := model.AdapterFor(*provider)
	if err != nil {
		return err
	}
	route := model.Route{
		ID: "roundtrip", Provider: *provider, Model: "roundtrip-model",
		MaxOutputTokens: 1024, APIKey: "<redacted>",
	}
	httpReq, err := adapter.ToWire(model.ModelRequest{Route: route, Messages: msgs})
	if err != nil {
		return fmt.Errorf("the transcript could not be put on the wire: %w", err)
	}

	// What the wire refused to carry (S18 T5, M27): a thinking block whose
	// producer is not this provider stays in the record rather than going
	// out under someone else's seal. Reporting it HERE is what separates
	// "the fidelity broke" from "the rule fired" — without this line the
	// CHANGED verdict below would blame the adapter for doing its job.
	withheld := 0
	if td, ok := adapter.(interface{ ThinkingDropped() int }); ok {
		withheld = td.ThinkingDropped()
	}

	wireBytes, err := io.ReadAll(httpReq.Body)
	if err != nil {
		return err
	}

	// What came back out: the wire's own thinking blocks, read back.
	after, err := wireSignatures(wireBytes)
	if err != nil {
		return err
	}

	identical := len(before) == len(after)
	if identical {
		for i := range before {
			if before[i] != after[i] {
				identical = false
				break
			}
		}
	}

	fmt.Fprintln(w)
	switch {
	case identical && len(before) > 0:
		fmt.Fprintf(w, "roundtrip   IDENTICAL — %d signature(s) survived byte for byte\n", len(before))
	case len(before) == 0:
		fmt.Fprintln(w, "roundtrip   ok, but this transcript has no signature to test")
		fmt.Fprintln(w, "            Produce one with a route that has extended thinking enabled.")
	case withheld > 0 && len(after) == len(before)-withheld:
		// The rule, not the corruption: the difference between in and out is
		// exactly the withheld count, so nothing was ALTERED — some blocks
		// simply were not this provider's to receive (M27). The transcript
		// keeps every block (M1); this is a wire-direction fact.
		fmt.Fprintf(w, "roundtrip   WITHHELD — %d of %d signature(s) stayed in the record; "+
			"%d went out intact\n", withheld, len(before), len(after))
		fmt.Fprintf(w, "            %s replays thinking only to the provider that produced it (M27),\n", *provider)
		fmt.Fprintln(w, "            and a foreign signature is unverifiable by design. The blocks")
		fmt.Fprintln(w, "            are NOT lost: they are in the transcript, waiting for their own")
		fmt.Fprintln(w, "            provider's turn. Stamp the turn's provenance to send them home.")
	default:
		fmt.Fprintf(w, "roundtrip   CHANGED — %d signature(s) in, %d out\n", len(before), len(after))
		fmt.Fprintln(w, "            This provider rejects an altered thinking block, and the")
		fmt.Fprintln(w, "            failure would surface in a recovery rather than here.")
		return fmt.Errorf("the transcript did not survive the round trip")
	}
	return nil
}

// parseTranscript accepts either shape a person is likely to have.
func parseTranscript(raw []byte) ([]model.Message, error) {
	// A stored envelope, as S4 will write.
	if res := model.MigrateTranscript(raw); res.OK() {
		return res.Messages, nil
	}
	// A bare message array, as `--show-transcript` prints.
	var msgs []model.Message
	if err := json.Unmarshal(raw, &msgs); err == nil && len(msgs) > 0 {
		return msgs, nil
	}
	// Both failed: report the envelope's reason, which is the more specific of
	// the two — it distinguishes a version problem from malformed JSON.
	res := model.MigrateTranscript(raw)
	return nil, fmt.Errorf("the file is neither a message array nor a stored transcript: %s", res.Reason)
}

func signatureLengths(msgs []model.Message) []int {
	var out []int
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Kind == model.BlockThinking && b.Signature != "" {
				out = append(out, len(b.Signature))
			}
		}
	}
	return out
}

// wireSignatures reads the signatures back out of a provider body.
//
// The content is read as raw bytes rather than as a shape, because the two
// providers spell it differently: Anthropic's is an array of typed blocks,
// while OpenAI chat's is a plain string. Decoding the string form into the
// array shape fails with an unmarshal error, which meant the round trip
// could not be run against a transcript whose blocks were all withheld
// (S18 T5) — precisely the case that most wants reporting. Absent content
// carries no signatures, which is an answer, not a parse failure.
func wireSignatures(body []byte) ([]int, error) {
	var wire struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("the wire body could not be read back: %w", err)
	}
	var out []int
	for _, m := range wire.Messages {
		var blocks []struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			// A string — this provider puts text there, not blocks.
			continue
		}
		for _, b := range blocks {
			if b.Type == "thinking" && b.Signature != "" {
				out = append(out, len(b.Signature))
			}
		}
	}
	return out, nil
}
