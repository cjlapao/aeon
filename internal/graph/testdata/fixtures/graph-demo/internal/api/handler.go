package api

import (
	"net/http"

	"graph-demo/internal/order"
)

// Handler is the chain's head: HandleOrderRequest → ProcessOrder → …
// The DEMO's path question starts here.
type Handler struct {
	Router *http.ServeMux // a plain field, not embedded — mixes_in must not fire on it
	Order  *order.Request
}

// Base is embedded by Server below: the fixture's Go-side mixes_in case
// (struct embedding — design.md's "closest structural match" for the
// relation, and parse_test asserts it is never tagged inherits, because
// Go has no inheritance at all, D3).
type Base struct {
	Name string
}

// Server embeds Base — the mixes_in edge, extracted within this file.
type Server struct {
	Base
	Handler
}

// HandleOrderRequest is DEMO §3's path start.
func HandleOrderRequest(w http.ResponseWriter, r *http.Request) {
	req := &order.Request{ID: "x", Total: 1}
	if err := order.ProcessOrder(req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Start wires the server through the embedded Base.
func (s *Server) Start() {
	_ = s.Base.Name
	if s.Handler.Router != nil {
		s.Handler.Router.HandleFunc("/order", HandleOrderRequest)
	}
}
