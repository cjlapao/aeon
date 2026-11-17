package main

// history_client.go — S53 T4 (HV6, P5, the roadmap RM9 shape). The two verbs
// this slice adds — coverage and vocabulary — reach the history plane through
// ONE door abstraction with two arms. The socket arm is the default; the
// offline arm runs the SAME answer functions the socket's handlers run
// (protocol.AnswerHistoryCoverage, history.Vocabulary) over a directly opened
// store — never a second implementation: the CLI re-validates nothing,
// invents no sentence, computes no coverage and no lane client-side (T4's
// do-not list). The three S25 verbs (list/show/export) keep their own road,
// goldens byte-identical; P34 will pull them onto this door when a slice
// owns their output as a change of its own.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cjlapao/aeon/internal/history"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// historyDoor is the two doors onto the one history plane. Both arms answer
// in the SAME Go types the socket carries (history.CoverageView,
// history.VocabularyView), so the printers below are one printer, and a
// person sees the same bytes whoever answered.
type historyDoor interface {
	coverage(ctx context.Context, p protocol.HistoryCoverageParams) (history.CoverageView, error)
	vocabulary(ctx context.Context) (history.VocabularyView, error)
}

// historyDial is the dial the selection uses; a var so a test can stand in a
// dial that refuses (a handshake fault) or answers "no daemon" without
// touching the real endpoint. Production is the one dial(0) every other verb
// uses, so the endpoint resolution is never forked.
var historyDial = dial

// historyErrOut is where the fixed direct-door line goes; a var for the same
// reason the roadmap's is one — the parity test runs the verbs in-process and
// reads the notice, and production writes os.Stderr.
var historyErrOut io.Writer = os.Stderr

// historyDirectNotice is the fixed stderr line the offline door says BEFORE
// any output — a person must never wonder which door answered. It is the same
// fixed sentence the blueprint, roadmap and policy roads print.
const historyDirectNotice = "daemon not running — applied directly to the store"

// selectHistoryDoor is the selection (P5's rule): a dial success is the
// socket door; a "no daemon" is the direct door, with the fixed line on
// errOut before any output; any OTHER dial failure is a REFUSAL. A daemon
// that exists and locks you out, or disagrees about the protocol, is a
// state the operator sees — the CLI never routes around it.
func selectHistoryDoor(errOut io.Writer) (historyDoor, error) {
	c, err := historyDial(0)
	if err == nil {
		return &historySocketDoor{c: c}, nil
	}
	if isNoDaemon(err) {
		fmt.Fprintln(errOut, historyDirectNotice)
		return historyDirectDoor{}, nil
	}
	return nil, err
}

// historyVerbatim maps a wire refusal to its `what` sentence VERBATIM — the
// daemon's own words are the refusal (the both-roots sentence, DB19);
// anything else (a transport fault, an unknown method) travels as the error
// itself.
func historyVerbatim(err error) error {
	var pe *protocol.Error
	if errors.As(err, &pe) {
		sentence := pe.Data.What
		if sentence == "" {
			sentence = pe.Message
		}
		if sentence != "" {
			return errors.New(sentence)
		}
	}
	return err
}

// --- the socket door ------------------------------------------------------------

// historySocketDoor is the default door: it calls the registered methods and
// relays what the wire said. It validates nothing.
type historySocketDoor struct{ c *protocol.Client }

func (d *historySocketDoor) coverage(_ context.Context, p protocol.HistoryCoverageParams) (history.CoverageView, error) {
	var res history.CoverageView
	err := d.c.Call(protocol.MethodHistoryCoverage, p, &res)
	if err != nil {
		err = historyVerbatim(err)
	}
	return res, err
}

func (d *historySocketDoor) vocabulary(_ context.Context) (history.VocabularyView, error) {
	var res history.VocabularyView
	err := d.c.Call(protocol.MethodHistoryVocabulary, struct{}{}, &res)
	if err != nil {
		err = historyVerbatim(err)
	}
	return res, err
}

// --- the direct door --------------------------------------------------------------

// historyDirectStoreOpen opens the offline door's store; a var so the parity
// test hands the arm a temp store (tests never point at the real one).
// Production is openStoreDirectly — the justified second access path, the
// same road list/show/export already walk.
var historyDirectStoreOpen = func() (*store.Store, error) { return openStoreDirectly(false) }

// historyDirectDoor is the offline arm. Its whole body is one store open
// plus a call into the SAME exported answers the daemon's handlers run
// (protocol.AnswerHistoryCoverage; history.Vocabulary, which needs no store
// at all — the vocabulary is code, and the offline door carries the very
// same bytes because they are the same function). No count, no word, no
// refusal sentence is computed here.
type historyDirectDoor struct{}

func (historyDirectDoor) coverage(_ context.Context, p protocol.HistoryCoverageParams) (history.CoverageView, error) {
	s, err := historyDirectStoreOpen()
	if err != nil {
		return history.CoverageView{}, err
	}
	defer func() { _ = s.Close() }()
	res, err := protocol.AnswerHistoryCoverage(history.NewStore(s), p)
	return res, historyVerbatim(err)
}

func (historyDirectDoor) vocabulary(context.Context) (history.VocabularyView, error) {
	return history.Vocabulary(), nil
}

// --- the one printer ---------------------------------------------------------------

// renderHistoryCoverage prints the view. The three states print as the three
// DISTINCT words the spec names — shown, not shown (with its reason), cannot
// say — never merged into one number, and the P32 floor statement rides
// VERBATIM (H6: this printer adds no cost computation; the view arrived with
// the sentence already worded by internal/history). One printer, both arms:
// the door that answered is not part of what is printed — the stderr notice
// said it once, before any output.
func renderHistoryCoverage(w io.Writer, v history.CoverageView) {
	scope := v.Scope
	if scope == "" {
		scope = "every component"
	}
	fmt.Fprintf(w, "history coverage — scope: %s\n", scope)
	fmt.Fprintf(w, "  shown       live: %d loop(s)", v.Live.Loops)
	printSpan(w, v.Live)
	fmt.Fprintf(w, "\n  shown       archived: %d archive(s)", v.Archived.Loops)
	printSpan(w, v.Archived)
	fmt.Fprintln(w)
	if v.Undecodable.Count > 0 {
		fmt.Fprintf(w, "  cannot say  %d archive(s) whose content could not be read", v.Undecodable.Count)
		for i, id := range v.Undecodable.IDs {
			if i == 0 {
				fmt.Fprintf(w, " — %s", id)
			} else {
				fmt.Fprintf(w, ", %s", id)
			}
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "  cannot say  none")
	}
	if len(v.NotShown) == 0 {
		fmt.Fprintln(w, "  not shown   none")
	}
	for _, ns := range v.NotShown {
		fmt.Fprintf(w, "  not shown   %d — %s\n", ns.Count, ns.Reason)
	}
	fmt.Fprintf(w, "  floor       %s\n", v.Floor.Statement)
}

func printSpan(w io.Writer, c history.Counted) {
	if c.Oldest != "" || c.Newest != "" {
		fmt.Fprintf(w, " (%s → %s)", c.Oldest, c.Newest)
	}
}

// renderHistoryVocabulary prints the closed sets verbatim — the client renders
// words it is served and types none (acceptance criterion 2).
func renderHistoryVocabulary(w io.Writer, v history.VocabularyView) {
	fmt.Fprintf(w, "lanes:     %v\n", v.Lanes)
	fmt.Fprintf(w, "outcomes:  %v\n", v.Outcomes)
	fmt.Fprintf(w, "filters:   %v\n", v.Filters)
}

// printHistoryJSON is --json: the view's own wire shape, indented, nothing
// else on stdout (the same law S47's JSON verbs keep).
func printHistoryJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
