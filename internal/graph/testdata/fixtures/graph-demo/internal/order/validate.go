package order

import "errors"

// Validate is one HALF of the deliberately ambiguous name (DEMO.md §4):
// a second function of this exact name lives in internal/billing, and no
// traversal may happen past the ambiguity — explain("Validate") must list
// both candidates, never pick one.
func Validate(req *Request) error {
	if req.ID == "" || req.Total <= 0 {
		return errors.New("invalid order")
	}
	return nil
}

// validateOrder is the chain's third hop: it calls the package's own
// Validate, which keeps one same-file edge (extracted) in the picture
// beside the cross-file ones (inferred).
func validateOrder(req *Request) error {
	return Validate(req)
}
