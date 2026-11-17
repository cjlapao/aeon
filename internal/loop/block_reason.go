package loop

// reasonOrNone keeps a blocked or failed step from stopping the loop in silence:
// the step gave no reason, and a banner reading "no reason recorded" tells a
// person nothing about where to look. Ok and reject keep an absent reason absent.
func reasonOrNone(outcome, reason string) string {
	if reason != "" || outcome == "ok" || outcome == "reject" {
		return reason
	}
	return "the step finished " + outcome + " and gave no reason — read its last message"
}
