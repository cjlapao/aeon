package billing

import "fmt"

// ChargePayment is the chain's billing-side leaf. It calls the billing
// package's own Validate (the second of the two same-named functions),
// so resolving "Validate" MUST stay ambiguous at the query layer while
// the charge→validate edge still resolves inside this file.
func ChargePayment(total int) error {
	if err := Validate(total); err != nil {
		return fmt.Errorf("billing: %w", err)
	}
	return nil
}
