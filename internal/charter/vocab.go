package charter

// AutonomyVocab is the S56 seam (OD12): the autonomy enum is READ from
// S56's descriptor and never typed here. Until S56 wires it, the seam is
// nil and a body that carries proposed.autonomy is refused with the
// sentence that says why — a typed vocabulary nobody granted would be the
// charter granting something (GI2).
type AutonomyVocab func() []string

// Caps is every bound the charter admits, as one answer — the doors render
// these words, they do not re-spell the numbers.
type Caps struct {
	BodyBytes   int `json:"bodyBytes"`
	Goal        int `json:"goal"`
	Item        int `json:"item"`
	List        int `json:"list"`
	Acceptance  int `json:"acceptance"`
	Tags        int `json:"tags"`
	Tag         int `json:"tag"`
	Verify      int `json:"verify"`
	VerifyBytes int `json:"verifyBytes"`
	EnvRef      int `json:"envRef"`
	Note        int `json:"note"`
}

// Vocab is what Validate reads: the closed check kinds and the autonomy
// words the seam currently answers (nil ⇒ none, which refuses the field).
type Vocab struct {
	CheckKinds []string `json:"checkKinds"`
	Caps       Caps     `json:"caps"`
	Autonomy   []string `json:"autonomy"`
}

// Vocabulary answers the charter.vocabulary door (T7). The autonomy half is
// always the seam's live answer — never a cache: a vocabulary that went
// stale would be a second truth.
func Vocabulary(seam AutonomyVocab) Vocab {
	var autonomy []string
	if seam != nil {
		autonomy = seam()
	}
	return Vocab{
		CheckKinds: append([]string(nil), CheckKinds...),
		Caps: Caps{
			BodyBytes:   MaxBodyBytes,
			Goal:        MaxGoal,
			Item:        MaxItem,
			List:        MaxList,
			Acceptance:  MaxAcceptance,
			Tags:        MaxTags,
			Tag:         MaxTag,
			Verify:      MaxVerify,
			VerifyBytes: MaxVerifyBytes,
			EnvRef:      MaxEnvRef,
			Note:        MaxNote,
		},
		Autonomy: autonomy,
	}
}
