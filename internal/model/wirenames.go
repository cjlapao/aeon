package model

import (
	"regexp"
	"strings"
)

// Providers accept tool names of letters, digits, underscore and hyphen only
// (OpenAI and Anthropic both: ^[a-zA-Z0-9_-]{1,64}$). The canonical names are
// not that: an MCP tool is `server.tool` and the dot is how a call is routed.
// The translation lives here, at the one place a request leaves for a
// provider, so the transcript, the policy and the router keep the canonical
// name and the provider only ever sees one it accepts.

var wireNameOK = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// wireNames is one request's mapping, built from the names the request
// carries and used to turn the provider's answer back.
type wireNames struct {
	toWire   map[string]string
	fromWire map[string]string
}

func newWireNames(req ModelRequest) *wireNames {
	w := &wireNames{toWire: map[string]string{}, fromWire: map[string]string{}}
	add := func(name string) {
		if name == "" || w.toWire[name] != "" {
			return
		}
		if wireNameOK.MatchString(name) {
			w.toWire[name] = name
			w.fromWire[name] = name
			return
		}
		enc := strings.Map(func(r rune) rune {
			if r == '.' {
				return -1 // replaced below, as two underscores
			}
			if r < 128 && (r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
				return r
			}
			return '_'
		}, strings.ReplaceAll(name, ".", "__"))
		if len(enc) > 64 {
			enc = enc[:64]
		}
		// A different canonical name that lands on the same wire name gets a suffix.
		base := enc
		for i := 2; w.fromWire[enc] != "" && w.fromWire[enc] != name; i++ {
			suffix := "_" + string(rune('0'+i%10))
			enc = base
			if len(enc)+len(suffix) > 64 {
				enc = enc[:64-len(suffix)]
			}
			enc += suffix
		}
		w.toWire[name] = enc
		w.fromWire[enc] = name
	}
	for _, t := range req.Tools {
		add(t.Name)
	}
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if b.Kind == BlockToolUse {
				add(b.Name)
			}
		}
	}
	return w
}

// changes reports whether any name differs on the wire.
func (w *wireNames) changes() bool {
	for name, enc := range w.toWire {
		if name != enc {
			return true
		}
	}
	return false
}

// encode returns a copy of the request whose tool names are wire-safe.
func (w *wireNames) encode(req ModelRequest) ModelRequest {
	out := req
	out.Tools = append([]ToolSpec(nil), req.Tools...)
	for i := range out.Tools {
		out.Tools[i].Name = w.wire(out.Tools[i].Name)
	}
	out.Messages = make([]Message, len(req.Messages))
	for i, m := range req.Messages {
		out.Messages[i] = m
		out.Messages[i].Content = append([]Block(nil), m.Content...)
		for j, b := range out.Messages[i].Content {
			if b.Kind == BlockToolUse {
				out.Messages[i].Content[j].Name = w.wire(b.Name)
			}
		}
	}
	return out
}

func (w *wireNames) wire(name string) string {
	if enc, ok := w.toWire[name]; ok {
		return enc
	}
	return name
}

// decode turns a tool call's wire name back into the canonical one.
func (w *wireNames) decode(d Delta) Delta {
	if d.Kind == DeltaToolUse && d.Block != nil {
		if name, ok := w.fromWire[d.Block.Name]; ok && name != d.Block.Name {
			b := *d.Block
			b.Name = name
			d.Block = &b
		}
	}
	return d
}
