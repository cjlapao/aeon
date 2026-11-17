package main

// supervision_t6_test.go — S58 T6: the P5 parity both directions for the
// supervision plane (inbox · status · notify), the door proofs that keep the
// CLI a THIN client (verbatim daemon refusals, C17's no-secret-value flags,
// the --yes seatbelt refusing BEFORE any dial), and the doctor's honesty leg
// (hosts only — never a path, never a secret).
//
// The fixture is T0's table: every supervision method and the verb that
// reaches it. `notify subscriptions set` is the one verb the spec's list did
// not name; it is here because notify.subscription.set is a T5 method and a
// method with no verb fails P5 — the deviation is recorded in the task Notes.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

func supervisionMethodNames() []protocol.MethodName {
	return []protocol.MethodName{
		protocol.MethodInboxList, protocol.MethodInboxSummary,
		protocol.MethodInboxAck, protocol.MethodInboxSnooze, protocol.MethodInboxUnack,
		protocol.MethodMissionStatus,
		protocol.MethodNotifyClassList, protocol.MethodNotifyClassUpdate, protocol.MethodNotifyVocabulary,
		protocol.MethodNotifyChannelList, protocol.MethodNotifyChannelShow,
		protocol.MethodNotifyChannelCreate, protocol.MethodNotifyChannelUpdate,
		protocol.MethodNotifyChannelEnable, protocol.MethodNotifyChannelDisable,
		protocol.MethodNotifyChannelArchive, protocol.MethodNotifyChannelTest,
		protocol.MethodNotifySubscriptionSet, protocol.MethodNotifyDeliveryList,
	}
}

var supervisionParity = map[protocol.MethodName][]string{
	protocol.MethodInboxList:             {"inbox list"},
	protocol.MethodInboxSummary:          {"inbox summary"},
	protocol.MethodInboxAck:              {"inbox ack"},
	protocol.MethodInboxSnooze:           {"inbox snooze"},
	protocol.MethodInboxUnack:            {"inbox unack"},
	protocol.MethodMissionStatus:         {"status"},
	protocol.MethodNotifyClassList:       {"notify classes"},
	protocol.MethodNotifyClassUpdate:     {"notify class update"},
	protocol.MethodNotifyVocabulary:      {"notify vocabulary"},
	protocol.MethodNotifyChannelList:     {"notify channels"},
	protocol.MethodNotifyChannelShow:     {"notify channel show"},
	protocol.MethodNotifyChannelCreate:   {"notify channel create"},
	protocol.MethodNotifyChannelUpdate:   {"notify channel update"},
	protocol.MethodNotifyChannelEnable:   {"notify channel enable"},
	protocol.MethodNotifyChannelDisable:  {"notify channel disable"},
	protocol.MethodNotifyChannelArchive:  {"notify channel archive"},
	protocol.MethodNotifyChannelTest:     {"notify channel test"},
	protocol.MethodNotifySubscriptionSet: {"notify subscriptions set"},
	protocol.MethodNotifyDeliveryList:    {"notify deliveries"},
}

func TestEverySupervisionMethodHasAVerb(t *testing.T) {
	if len(supervisionParity) != len(supervisionMethodNames()) {
		t.Fatalf("the parity table holds %d methods, the plane has %d — one of them has no verb",
			len(supervisionParity), len(supervisionMethodNames()))
	}
	for _, m := range supervisionMethodNames() {
		if v, ok := supervisionParity[m]; !ok || len(v) == 0 {
			t.Errorf("%s has no CLI verb (P34: a capability only on the wire is not finished)", m)
		}
	}
}

func TestEverySupervisionVerbHasAMethod(t *testing.T) {
	family := map[protocol.MethodName]bool{}
	for _, m := range supervisionMethodNames() {
		family[m] = true
	}
	for m, verbs := range supervisionParity {
		if !family[m] {
			t.Errorf("verbs %v are filed under %s, which is not in the supervision plane", verbs, m)
		}
	}
}

// supervisionVerbDispatch runs every verb against a DEAD endpoint: the fixed
// no-daemon sentence proves the dispatch case exists. A missing case would
// read "unknown subcommand" or print usage — never the sentence. The
// vocabulary reads the daemon's live vocabulary (the watchdog precedent):
// the plane's closed set is answered by the door, not by a CLI copy — so
// it too answers the guard sentence with no daemon.
var supervisionVerbDispatch = []struct {
	verb     string
	args     []string
	sentence string
}{
	{"inbox list", []string{"inbox", "list"}, inboxNoDaemon},
	{"inbox summary", []string{"inbox", "summary"}, inboxNoDaemon},
	{"inbox ack", []string{"inbox", "ack", "questions:1"}, inboxNoDaemon},
	{"inbox snooze", []string{"inbox", "snooze", "questions:1", "--for", "1h"}, inboxNoDaemon},
	{"inbox unack", []string{"inbox", "unack", "questions:1"}, inboxNoDaemon},
	{"status", []string{"status"}, statusNoDaemon},
	{"notify classes", []string{"notify", "classes"}, notifyNoDaemon},
	{"notify class update", []string{"notify", "class", "update", "loop.blocked", "--enable"}, notifyNoDaemon},
	{"notify channels", []string{"notify", "channels"}, notifyNoDaemon},
	{"notify channel show", []string{"notify", "channel", "show", "ch1"}, notifyNoDaemon},
	{"notify channel create", []string{"notify", "channel", "create", "--name", "n", "--url", "https://example.com/h", "--yes"}, notifyNoDaemon},
	{"notify channel update", []string{"notify", "channel", "update", "ch1", "--name", "n"}, notifyNoDaemon},
	{"notify channel enable", []string{"notify", "channel", "enable", "ch1"}, notifyNoDaemon},
	{"notify channel disable", []string{"notify", "channel", "disable", "ch1"}, notifyNoDaemon},
	{"notify channel archive", []string{"notify", "channel", "archive", "ch1", "--reason", "done", "--yes"}, notifyNoDaemon},
	{"notify channel test", []string{"notify", "channel", "test", "ch1", "--yes"}, notifyNoDaemon},
	{"notify subscriptions set", []string{"notify", "subscriptions", "set", "--channel", "ch1"}, notifyNoDaemon},
	{"notify deliveries", []string{"notify", "deliveries", "--channel", "ch1"}, notifyNoDaemon},
	{"notify vocabulary", []string{"notify", "vocabulary"}, notifyNoDaemon},
}

func TestSupervisionVerbDispatch(t *testing.T) {
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "nothing.sock"))
	for _, tc := range supervisionVerbDispatch {
		var buf bytes.Buffer
		var err error
		switch tc.args[0] {
		case "inbox":
			err = cmdInbox(&buf, tc.args[1:])
		case "status":
			err = cmdStatus(&buf, tc.args[1:])
		case "notify":
			err = cmdNotify(&buf, tc.args[1:])
		default:
			t.Fatalf("unrouted family %q", tc.args[0])
		}
		if err == nil {
			t.Errorf("verb %q succeeded with no daemon — it should answer the fixed sentence", tc.verb)
			continue
		}
		if !strings.Contains(err.Error(), tc.sentence) {
			t.Errorf("verb %q answers %q, want %q", tc.verb, err, tc.sentence)
		}
		if strings.Contains(err.Error(), "unknown") {
			t.Errorf("verb %q reads as an unknown subcommand — the dispatch case is missing: %v", tc.verb, err)
		}
	}
}

// TestChannelVerbsRequireYes: create/archive/test are acts of authority or
// acts off the machine, and the seatbelt refuses BEFORE any dial — proven
// with NO daemon in reach at all: a verb that reached the socket guard
// sentence first would print notifyNoDaemon instead, and fail here.
func TestChannelVerbsRequireYes(t *testing.T) {
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "nothing.sock"))
	cases := []struct {
		verb string
		args []string
		act  string
	}{
		{"create", []string{"--name", "n", "--url", "https://example.com/h"}, "makes standing authority to send off this machine"},
		{"archive", []string{"ch1", "--reason", "done"}, "retires that authority for real"},
		{"test", []string{"ch1"}, "sends a real notification off this machine NOW"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		err := cmdNotifyChannel(&buf, append([]string{tc.verb}, tc.args...))
		if err == nil {
			t.Errorf("`notify channel %s` proceeded WITHOUT --yes", tc.verb)
			continue
		}
		if !strings.Contains(err.Error(), tc.act) || !strings.Contains(err.Error(), "pass --yes to mean it") {
			t.Errorf("`notify channel %s` refused with %q — want the seatbelt naming %q BEFORE any dial", tc.verb, err, tc.act)
		}
		if strings.Contains(err.Error(), notifyNoDaemon) {
			t.Errorf("`notify channel %s` dialed before refusing — the seatbelt must come first", tc.verb)
		}
	}
	// And archive without --reason is refused before even the seatbelt:
	// the delivery log keeps WHY forever.
	var buf bytes.Buffer
	if err := cmdNotifyChannel(&buf, []string{"archive", "ch1"}); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("`archive` without --reason: %v — want the refusal naming the flag", err)
	}
}

// TestNoSecretValueFlagExists is C17 read off the source, not trusted from
// a review: the supervision CLI defines NO flag whose name is a credential
// and whose value is a secret. `--secret-file` carries a PATH and is the
// spelled-out exception; `--url` carries an endpoint, which the daemon
// still treats as sensitive output (host views everywhere).
func TestNoSecretValueFlagExists(t *testing.T) {
	src, err := os.ReadFile("notify_channel_cli.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	s := string(src)
	for _, bad := range []string{`"secret"`, `"secret-value"`, `"secretValue"`, `"token"`, `"password"`, `"key"`, `"api-key"`, `"apikey"`, `"whsec"`} {
		if strings.Contains(s, "fs.String("+bad) || strings.Contains(s, "fs.Bool("+bad) || strings.Contains(s, `fs.Var(&`+bad) {
			t.Errorf("notify channel CLI defines a value flag %s — a credential in argv is in the process list and the shell history (C17)", bad)
		}
	}
	if !strings.Contains(s, `fs.String("secret-file"`) {
		t.Fatal(`there is no --secret-file — C17's path door is the ONLY secret door`)
	}
}

// TestDoctorShowsHostsOnly pins the doctor's notify leg against crafted
// door answers: the doctor repeats what the door shows — a HOST, never a
// full URL, never a path, never a secret — and raises FAILING at three in a
// row. (The goldens pin the real daemon's whole-page output; this pins the
// honesty rule itself so a golden drift cannot smuggle a leak in.)
func TestDoctorShowsHostsOnly(t *testing.T) {
	ch := protocol.NotifyChannelListResult{Channels: []protocol.NotifyChannelView{
		{Name: "ops", Kind: "slack-webhook", Host: "hooks.slack.com", Enabled: true,
			SecretPresent: true, LastOutcome: "ok", ConsecutiveFailures: 0,
			CreatedAt: "2026-11-03T09:00:00Z", UpdatedAt: "2026-11-03T09:00:00Z"},
		{Name: "ops2", Kind: "slack-webhook", Host: "hooks.slack.com", Enabled: false,
			SecretPresent: true, LastOutcome: "failing", ConsecutiveFailures: 3,
			CreatedAt: "2026-11-03T09:00:00Z", UpdatedAt: "2026-11-03T09:00:00Z"},
	}}
	var buf bytes.Buffer
	renderChannelFacts(&buf, ch, nil)
	out := buf.String()
	if !strings.Contains(out, "host hooks.slack.com") {
		t.Fatalf("doctor output lost the host:\n%s", out)
	}
	if !strings.Contains(out, "FAILING (3 in a row)") {
		t.Fatalf("three failures in a row did not raise the flag:\n%s", out)
	}
	for _, leak := range []string{"https://", "/services/", "whsec", "secret_file", "/home/"} {
		if strings.Contains(out, leak) {
			t.Fatalf("the doctor printed %q — hosts only, never paths or secrets:\n%s", leak, out)
		}
	}
	// The flag has teeth BOTH ways: two in a row is NOT yet failing.
	ch.Channels[1].ConsecutiveFailures = 2
	buf.Reset()
	renderChannelFacts(&buf, ch, nil)
	if strings.Contains(buf.String(), "FAILING") {
		t.Fatalf("two in a row already reads FAILING — the flag must mean what it says:\n%s", buf.String())
	}
	// A refusal is printed verbatim and stops there (no paraphrase, no
	// invented zero).
	buf.Reset()
	renderChannelFacts(&buf, protocol.NotifyChannelListResult{}, &protocol.Error{Code: protocol.ErrInboxScope.Code, Message: "the daemon said a thing"})
	if !strings.Contains(buf.String(), "the daemon said a thing") {
		t.Fatalf("the refusal was paraphrased:\n%s", buf.String())
	}
	// The census prints its states, `not-wired` included — never zero.
	buf.Reset()
	renderSourceCensus(&buf, protocol.InboxSummaryResult{Sources: []protocol.InboxSourceState{
		{Name: "questions", State: "reporting"}, {Name: "budget", State: "not-wired"},
	}}, nil)
	if got := buf.String(); !strings.Contains(got, "questions:reporting") || !strings.Contains(got, "budget:not-wired") {
		t.Fatalf("census lost a state:\n%s", got)
	}
}

// TestSupervisionCLIRefusalsVerbatim is P5's other half against a REAL daemon: the
// daemon's own sentence, byte for byte, out of the CLI — human path and
// --json wire path alike.
func TestSupervisionCLIRefusalsVerbatim(t *testing.T) {
	f := startDaemon(t)
	const sentence = `severity "notable" is not one of needs-you, stuck, blocked, stopped, info`
	out, err := f.run("inbox", "list", "--severity", "notable")
	if err == nil {
		t.Fatalf("a typo severity answered happily:\n%s", out)
	}
	if !strings.Contains(out, sentence) {
		t.Fatalf("human path paraphrased the daemon:\n%s\nwant it to carry: %s", out, sentence)
	}
	// --json is bytes on SUCCESS; a refusal is the daemon's own message,
	// and the renderer still carries it byte for byte (callJSON returns the
	// error untouched — no re-wording on any path).
	outJSON, errJSON := f.run("inbox", "list", "--severity", "notable", "--json")
	if errJSON == nil {
		t.Fatalf("--json swallowed the refusal:\n%s", outJSON)
	}
	if !strings.Contains(outJSON, sentence) {
		t.Fatalf("--json path paraphrased the daemon:\n%s", outJSON)
	}
	// The envelope ITSELF is the wire's: probe the socket in-process and
	// compare messages — the CLI and the door speak one sentence.
	raw, err := socketProbe(t, f.endpoint, "notable")
	if err != nil {
		t.Fatalf("socket probe: %v", err)
	}
	if raw != sentence {
		t.Fatalf("the door's What is not the sentence the CLI printed:\n  door: %s\n  want: %s", raw, sentence)
	}
}

// socketProbe dials the fixture daemon and asks inbox.list with the typo,
// returning the error's wire message.
func socketProbe(t *testing.T, endpoint, severity string) (string, error) {
	t.Helper()
	t.Setenv("LOCALRPC_ENDPOINT_PATH", endpoint)
	c, err := dial(0)
	if err != nil {
		return "", err
	}
	defer c.Close()
	var res json.RawMessage
	err = c.Call(protocol.MethodInboxList, map[string]string{"severity": severity}, &res)
	if err == nil {
		return string(res), nil
	}
	// The wire's three parts: the CLI prints Data.what, so that is the
	// byte the comparison must pin (Error() is just the code line).
	var perr *protocol.Error
	if errors.As(err, &perr) {
		return perr.Data.What, nil
	}
	return err.Error(), nil
}
