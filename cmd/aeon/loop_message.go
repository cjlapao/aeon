package main

// loop_message.go — `aeon loop message ID "TEXT" [--attach FILE]... [--route ID]`
// (S69 T5): the human's terminal path into a resting loop's conversation,
// with files riding the same turn.
//
// The flags follow the door's own doctrine (AT3): each --attach file is PUT
// once through attachment.put — the only place bytes ever cross — and the
// message then carries REFERENCES ({document, rev}) and nothing else. The
// receipt printed here is the STORE's answer (its size, its sha, the type
// its magic bytes proved), not a claim this binary made up. A file the door
// refuses (over cap, lying extension, off the allow-list) stops the command
// BEFORE the message is sent — a half-attached turn is a turn that lies
// about what rode it.
//
// The declared mediaType is guessed from the extension and the store decides
// whether the bytes agree; a name with no guessable type declares
// application/octet-stream and hears the allow-list refusal by name, which
// is the honest outcome rather than a guess at the operator's intent.

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjlapao/aeon/internal/protocol"
)

// repeatFlag is the repeatable --attach: order is the operator's order, and
// the row keeps it.
type repeatFlag []string

func (r *repeatFlag) String() string { return strings.Join(*r, ",") }
func (r *repeatFlag) Set(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("--attach needs a path")
	}
	*r = append(*r, v)
	return nil
}

func cmdLoopMessage(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("loop message", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	var attaches repeatFlag
	fs.Var(&attaches, "attach", "a file to attach to this turn (repeatable): bytes are put ONCE into the loop's frozen @attachments/ revisions and the message carries the reference")
	route := fs.String("route", "", "the route for this turn's model call (default: the loop's own)")
	positional, rest := splitPositional(args, 2)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	positional = append(positional, fs.Args()...)
	if len(positional) != 2 {
		return fmt.Errorf("usage: aeon loop message ID \"TEXT\" [--attach FILE]... [--route ID]")
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()

	// The scope is the loop's own row's — read from the loop, never
	// invented from a flag: an attachment lives where its loop lives.
	var lv protocol.LoopView
	if err := c.Call(protocol.MethodLoopShow, protocol.LoopShowParams{ID: positional[0]}, &lv); err != nil {
		return err
	}

	var refs []protocol.AttachmentRef
	for _, path := range attaches {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("--attach %s: %w", path, err)
		}
		name := filepath.Base(path)
		mt := mime.TypeByExtension(filepath.Ext(path))
		if mt == "" {
			mt = "application/octet-stream"
		}
		var res protocol.AttachmentPutResult
		if err := c.Call(protocol.MethodAttachmentPut, protocol.AttachmentPutParams{
			Slug: lv.Slug, ComponentRoot: lv.ComponentRoot,
			Name: name, MediaType: mt, Data: base64.StdEncoding.EncodeToString(raw),
		}, &res); err != nil {
			// The door's sentence names the file, the number and the limit
			// (or the claim versus the bytes). Nothing was sent; the
			// operator fixes the file or drops it, and NOTHING half-rode.
			return fmt.Errorf("--attach %s: %w", path, err)
		}
		r := res.Receipt
		fmt.Fprintf(w, "put %s · %s rev %d · %s · %d bytes · %s…\n",
			r.Name, r.Document, r.Rev, r.MediaType, r.Size, r.SHA256[:8])
		refs = append(refs, protocol.AttachmentRef{Document: r.Document, Rev: r.Rev})
	}

	var out map[string]any
	if err := c.Call(protocol.MethodLoopMessage, protocol.LoopMessageParams{
		ID: positional[0], Text: positional[1], Route: *route, Attachments: refs,
	}, &out); err != nil {
		return err
	}
	if refused, ok := out["refused"]; ok {
		fmt.Fprintf(w, "refused: %v\n", refused)
		return nil
	}
	if len(refs) > 0 {
		fmt.Fprintf(w, "loop %s · turn started with %d attachment(s)\n", positional[0], len(refs))
		return nil
	}
	fmt.Fprintf(w, "loop %s · turn started\n", positional[0])
	return nil
}
