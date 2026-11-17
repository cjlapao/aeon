package billing

import "errors"

// Validate is the second of the two functions this fixture deliberately
// names alike (DEMO.md §4). Same name, different package, different
// signature — the exact case D8's `ambiguous` state exists for.
func Validate(total int) error {
	if total <= 0 {
		return errors.New("invalid amount")
	}
	return nil
}
