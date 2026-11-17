package blueprint

type DiagState string

const (
	Invalid      DiagState = "invalid"
	Unverifiable DiagState = "unverifiable"
	Valid        DiagState = "valid"
)

type Diagnostic struct {
	State DiagState `json:"state"`
	Kind  string    `json:"kind"`
	Path  string    `json:"path"`
	Msg   string    `json:"message"`
	Hint  string    `json:"hint,omitempty"`
	Slice string    `json:"slice,omitempty"`
}
