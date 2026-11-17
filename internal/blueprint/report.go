package blueprint

type Report struct {
	Blueprint     string       `json:"blueprint"`
	SchemaVersion int          `json:"schemaVersion"`
	Version       int          `json:"version"`
	ValidCount    int          `json:"validCount"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

func (r Report) OK(strict bool) bool {
	for _, d := range r.Diagnostics {
		if d.State == Invalid || strict && d.State == Unverifiable {
			return false
		}
	}
	return true
}
func (r Report) Counts() (valid, invalid, unverifiable int) {
	valid = r.ValidCount
	for _, d := range r.Diagnostics {
		switch d.State {
		case Invalid:
			invalid++
		case Unverifiable:
			unverifiable++
		}
	}
	return
}
