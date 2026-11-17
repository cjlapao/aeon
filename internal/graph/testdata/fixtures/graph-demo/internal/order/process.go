package order

import (
	"fmt"

	"graph-demo/internal/billing"
)

// Request is what flows through the chain. A type, so `uses` has real
// material: signatures reference it rather than inventing edges.
type Request struct {
	ID    string
	Total int
}

// ProcessOrder sits in the middle of the fixture's four-hop chain
// (DEMO.md §2–3):
//
//	HandleOrderRequest → ProcessOrder → validateOrder → Validate
//	                                  → billing.ChargePayment
//
// The chain's depth is load-bearing — do not flatten it.
func ProcessOrder(req *Request) error {
	if err := validateOrder(req); err != nil {
		return err
	}
	if err := billing.ChargePayment(req.Total); err != nil {
		return fmt.Errorf("charge: %w", err)
	}
	return nil
}

// Handle is a second entry point, used by query-mode tests.
func Handle(req *Request) error {
	return ProcessOrder(req)
}
