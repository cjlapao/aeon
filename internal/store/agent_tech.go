package store

import (
	"encoding/json"
	"sort"
)

// agent_tech.go — the tech tags an agent serves (what `agent: by-tech-tag`
// dispatches on). They ride the agent's Metadata blob under "tech", so a card
// export and import carry them and no schema changes; the closed vocabulary
// they are checked against lives in internal/handler (tech.go).

// TechTags returns the agent's tech tags, sorted; none when it declares none or
// the blob cannot be read.
func (a Agent) TechTags() []string {
	if a.Metadata == "" {
		return nil
	}
	var blob struct {
		Tech []string `json:"tech"`
	}
	if err := json.Unmarshal([]byte(a.Metadata), &blob); err != nil || len(blob.Tech) == 0 {
		return nil
	}
	out := append([]string(nil), blob.Tech...)
	sort.Strings(out)
	return out
}

// WithTech returns the agent's metadata blob with its tech tags set to tags
// (the caller has normalized them), every other key kept. An empty list removes
// the key.
func WithTech(metadata string, tags []string) (string, error) {
	blob := map[string]json.RawMessage{}
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &blob); err != nil {
			return "", err
		}
	}
	if len(tags) == 0 {
		delete(blob, "tech")
	} else {
		b, err := json.Marshal(tags)
		if err != nil {
			return "", err
		}
		blob["tech"] = b
	}
	if len(blob) == 0 {
		return "", nil
	}
	b, err := json.Marshal(blob)
	return string(b), err
}

// WithMinion returns the agent's metadata blob with its minion — which of the minion characters it
// wears (pip, bop, noodle) — set, every other key kept. It rides the metadata under "minion", like
// the tech tags, so a card carries it and no schema changes. An empty value removes the key: the
// agent is then drawn from a hash of its name, as every agent was before it could choose.
func WithMinion(metadata, minion string) (string, error) {
	blob := map[string]json.RawMessage{}
	if metadata != "" {
		if err := json.Unmarshal([]byte(metadata), &blob); err != nil {
			return "", err
		}
	}
	if minion == "" {
		delete(blob, "minion")
	} else {
		b, err := json.Marshal(minion)
		if err != nil {
			return "", err
		}
		blob["minion"] = b
	}
	if len(blob) == 0 {
		return "", nil
	}
	b, err := json.Marshal(blob)
	return string(b), err
}
