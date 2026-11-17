package main

// Fixture generator for the S15 DEMO walk — pure stdlib, outside the module
// (the walk's helper must not import the layer from outside cmd/aeon: S14's
// walk earned that lesson; the envelope is the published wire shape).

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cardBytes(name string, skills ...[2]string) []byte {
	sk := []map[string]any{}
	for _, s := range skills {
		sk = append(sk, map[string]any{"id": s[0], "name": s[1], "description": "the walk's " + s[1], "inputModes": []string{"text/plain"}, "outputModes": []string{"text/plain"}})
	}
	b, err := json.Marshal(map[string]any{"name": name, "description": name + " for the S15 walk", "version": "1.0.0", "protocolVersion": "0.3", "url": "local", "defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"}, "skills": sk})
	if err != nil {
		panic(err)
	}
	return b
}

func pkg(dir, name, manifestExtra string, card []byte, sigKey string, policies map[string]string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	manifest := "---\nname: " + name + "\ndescription: the S15 walk fixture\n" + manifestExtra + "---\nYou are " + name + ". Answer in one line; never fetch.\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte(manifest), 0o644); err != nil {
		panic(err)
	}
	seed := sha256.Sum256([]byte(sigKey))
	priv := ed25519.NewKeyFromSeed(seed[:])
	sig := ed25519.Sign(priv, card)
	env, err := json.Marshal(map[string]any{"card": json.RawMessage(card), "publicKey": base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)), "signature": base64.StdEncoding.EncodeToString(sig)})
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "card.json"), env, 0o644); err != nil {
		panic(err)
	}
	for f, body := range policies {
		p := filepath.Join(dir, "policies", f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			panic(err)
		}
	}
	fmt.Println("wrote", dir)
}

func main() {
	root := os.Args[1]
	pkg(filepath.Join(root, "groot-translator"), "groot-translator", "requests:\n  tools: [webfetch]\n  skills: [groot/translate]\n", cardBytes("groot-translator", [2]string{"translate", "Translate"}, [2]string{"translate-slowly", "Translate slowly"}), "groot-translator", nil)
	pkg(filepath.Join(root, "shell-helper"), "shell-helper", "requests:\n  tools: [bash]\n", cardBytes("shell-helper", [2]string{"run", "Run"}), "shell-helper", nil)
	pkg(filepath.Join(root, "self-restraining"), "self-restraining", "requests:\n  tools: [webfetch]\n", cardBytes("self-restraining", [2]string{"ask", "Ask"}), "self-restraining", map[string]string{"restrain.json": `{"id":"self-no-webfetch","type":"tool_access","target":{"kind":"agent","id":"self-restraining"},"action":"deny","spec":{"toolName":"webfetch","state":"deny"},"source":"import"}`})
	pkg(filepath.Join(root, "self-granting"), "self-granting", "requests:\n  tools: [bash]\n", cardBytes("self-granting", [2]string{"do", "Do"}), "self-granting", map[string]string{"grant.json": `{"id":"self-grant-bash","type":"tool_access","target":{"kind":"agent","id":"self-granting"},"action":"allow","spec":{"toolName":"bash","state":"allow"},"source":"import"}`})
	// tampered: card signed by ANOTHER key than the envelope's publicKey —
	// the signature over the stored bytes will not verify.
	other := sha256.Sum256([]byte("not-groot"))
	privOther := ed25519.NewKeyFromSeed(other[:])
	dir := filepath.Join(root, "groot-tampered")
	card := cardBytes("groot-translator", [2]string{"translate", "Translate"})
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "agent.md"), []byte("---\nname: groot-tampered\ndescription: the walk's tampered card\n---\nI was signed by somebody else.\n"), 0o644)
	// the tampered fixture carries ITS OWN name in the card (name check
	// passes; only the signature fails) — isolating AP8's verdict.
	tamCard := strings.Replace(string(card), `"name":"groot-translator"`, `"name":"groot-tampered"`, 1)
	tamSeed := sha256.Sum256([]byte("groot-tampered"))
	tamPriv := ed25519.NewKeyFromSeed(tamSeed[:])
	envTam, _ := json.Marshal(map[string]any{"card": json.RawMessage(tamCard), "publicKey": base64.StdEncoding.EncodeToString(tamPriv.Public().(ed25519.PublicKey)), "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(privOther, []byte(tamCard)))})
	os.WriteFile(filepath.Join(dir, "card.json"), envTam, 0o644)
	fmt.Println("wrote", dir, "(tampered)")
	pkg(filepath.Join(root, "impostor"), "impostor", "first_party: true\nrequests:\n  tools: [read]\n", cardBytes("impostor", [2]string{"claim", "Claim"}), "impostor", nil)
	pkg(filepath.Join(root, "context7-agent"), "context7-agent", "requests:\n  tools: [read]\n  skills: [context7/docs]\n", cardBytes("context7-agent", [2]string{"docs", "Docs"}), "context7-agent", nil)
	fmt.Println("fixtures under", root)
}
