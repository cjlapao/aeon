package main

// loop_todos_verbs.go — S75 T3: `aeon loop todos` and `aeon loop todos close`.
// Both are clients of the socket (daemon first, CLI second): the list is read
// with `loop.todos` and closed with `loop.todos.close`, the person's door.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

func cmdLoopTodos(w io.Writer, args []string) error {
	if len(args) > 0 && args[0] == "close" {
		return cmdLoopTodosClose(w, args[1:])
	}
	fs := flag.NewFlagSet("loop todos", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop todos ID [--json] | aeon loop todos close ID — the loop's todo list")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.LoopTodosParams{ID: positional[0]}
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopTodos, params)
	}
	var res protocol.LoopTodosResult
	if err := c.Call(protocol.MethodLoopTodos, params, &res); err != nil {
		return err
	}
	renderLoopTodos(w, positional[0], res)
	return nil
}

func cmdLoopTodosClose(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop todos close", flag.ContinueOnError)
	fs.SetOutput(w)
	asJSON := fs.Bool("json", false, "print the wire result verbatim")
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 1)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return fmt.Errorf("usage: aeon loop todos close ID [--json] — close the loop's active todo list")
	}
	c, err := dialControl(*want, noDaemonStopSentence)
	if err != nil {
		return err
	}
	defer c.Close()
	params := protocol.LoopTodosParams{ID: positional[0]}
	if *asJSON {
		return callJSON(w, c, protocol.MethodLoopTodosClose, params)
	}
	var res protocol.LoopTodosResult
	if err := c.Call(protocol.MethodLoopTodosClose, params, &res); err != nil {
		return err
	}
	renderLoopTodos(w, positional[0], res)
	return nil
}

// renderLoopTodos prints the daemon's list: the status words and the counts are
// the daemon's own, and the empty case prints the daemon's statement.
func renderLoopTodos(w io.Writer, loop string, res protocol.LoopTodosResult) {
	state := "active"
	if !res.Active {
		state = "closed"
		if res.ClosedBy != "" {
			state = "closed (" + res.ClosedBy + ")"
		}
	}
	if len(res.Items) == 0 {
		fmt.Fprintf(w, "loop %s — %s\n", loop, res.Statement)
		return
	}
	fmt.Fprintf(w, "loop %s — todo list %s, revision %d\n", loop, state, res.Revision)
	for _, it := range res.Items {
		fmt.Fprintf(w, "  %s %q: %s\n", todoMark(it.Status), it.ID, it.Content)
	}
	fmt.Fprintf(w, "%d pending, %d in progress, %d done\n", res.Counts.Pending, res.Counts.InProgress, res.Counts.Done)
}

func todoMark(status string) string {
	switch status {
	case "done":
		return "[x]"
	case "in_progress":
		return "[~]"
	default:
		return "[ ]"
	}
}
