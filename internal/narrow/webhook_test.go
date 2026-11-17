package narrow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/protocol"
)

// webhook_test.go — the mounting (TR11/R-5/R-6): the key is not this
// door's business (R-5), the signature is a PARAM (R-6, stdio has no
// headers), and the headers are sugar that may never fork the params.

// webhookCaller answers deliveries with a canned outcome and remembers
// the exact params the daemon would have seen.
type webhookCaller struct {
	calls    []webhookCall
	response protocol.WebhookDeliverResult
}

type webhookCall struct {
	method protocol.MethodName
	params map[string]any
}

func (c *webhookCaller) Call(method protocol.MethodName, params, result any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	c.calls = append(c.calls, webhookCall{method: method, params: m})
	b, err := json.Marshal(c.response)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}

func deliver(t *testing.T, c *webhookCaller, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	m := NewMux(c, Actions())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	m.ServeHTTP(rec, req)
	return rec
}

func TestDeliveryIsOneCallWithParams(t *testing.T) {
	c := &webhookCaller{response: protocol.WebhookDeliverResult{TriggerID: "ship", Outcome: "fired", LoopID: "loop-9", HTTPStatus: 200}}
	rec := deliver(t, c, "/v1/webhooks?id=ship&signature=sha256=aa", `{"ref":"abc"}`, nil)
	if len(c.calls) != 1 {
		t.Fatalf("%d calls", len(c.calls))
	}
	if c.calls[0].method != protocol.MethodWebhookDeliver {
		t.Fatalf("method %q", c.calls[0].method)
	}
	got := c.calls[0].params
	if got["id"] != "ship" || got["signature"] != "sha256=aa" {
		t.Fatalf("params lost the id or the signature: %v", got)
	}
	// The payload is the EXACT body bytes — the sender signed those, no
	// re-serialization may stand between a signature and its proof.
	if got["payload"] != `{"ref":"abc"}` {
		t.Fatalf("payload was rewritten: %v", got["payload"])
	}
	if rec.Code != 200 {
		t.Fatalf("a fired delivery answered %d", rec.Code)
	}
}

func TestRefusedSignatureAnswersWithItsOwnStatus(t *testing.T) {
	// A 200-on-a-refusal would teach senders to stop reading bodies; the
	// outcome carries the status because the outcome IS the answer.
	c := &webhookCaller{response: protocol.WebhookDeliverResult{
		TriggerID: "ship", Outcome: "did_not_fire",
		Reason:     "the signature did not verify",
		HTTPStatus: 401,
	}}
	rec := deliver(t, c, "/v1/webhooks?id=ship", `x`, nil)
	if rec.Code != 401 {
		t.Fatalf("refused signature answered %d; the body says 401 — the wire must agree with the answer", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "did_not_fire") {
		t.Fatalf("the five-answer vocabulary must ride the refusal: %s", rec.Body.String())
	}
}

func TestHeaderSugarFoldsIntoTheSameParams(t *testing.T) {
	// R-6: stdio has no headers, so params are the protocol and headers
	// are sugar. Sugar may FILL a missing param and never fork one.
	c := &webhookCaller{response: protocol.WebhookDeliverResult{TriggerID: "ship", Outcome: "fired", HTTPStatus: 200}}
	deliver(t, c, "/v1/webhooks?id=ship", "body", map[string]string{
		"X-Hub-Signature-256": "sha256:beef",
		"X-GitHub-Delivery":   "del-77",
		"X-Aeon-Timestamp":    "2026-10-27T09:14:02Z",
	})
	got := c.calls[0].params
	if got["signature"] != "sha256:beef" || got["deliveryId"] != "del-77" || got["timestamp"] != "2026-10-27T09:14:02Z" {
		t.Fatalf("headers did not fold into the params stdio speaks: %v", got)
	}
	// Param beats header — two sources that may disagree is a fork.
	c2 := &webhookCaller{response: protocol.WebhookDeliverResult{TriggerID: "ship", Outcome: "fired", HTTPStatus: 200}}
	deliver(t, c2, "/v1/webhooks?id=ship&signature=param-wins", "body", map[string]string{"X-Hub-Signature-256": "sha256:header-loses"})
	if c2.calls[0].params["signature"] != "param-wins" {
		t.Fatalf("the header overrode the param — the protocol forked: %v", c2.calls[0].params["signature"])
	}
}

func TestOversizedWebhookBodyRefusedAsTransport(t *testing.T) {
	c := &webhookCaller{}
	rec := deliver(t, c, "/v1/webhooks?id=ship", `{"pad":"`+strings.Repeat("x", maxBody+16)+`"}`, nil)
	if len(c.calls) != 0 {
		t.Fatal("an oversized body reached the daemon")
	}
	if !strings.Contains(rec.Body.String(), "over") {
		t.Fatalf("oversize must be the stated reason: %s", rec.Body.String())
	}
}

func TestWebhookIsTheOnlyRouteWithoutTheKey(t *testing.T) {
	// R-5 as a table fact: the keyless set is ENUMERATED by name. It grew
	// exactly once — S21 T6's completion route, by the same argument (a
	// peer knows its own pinned key and never AEON_HTTP_KEY; its handler
	// authenticates by HMAC before anything else) — and BOTH members are
	// still named here, because the name of the function (webhookLevel)
	// and the enumeration of its users are what keep an exemption an
	// exemption rather than a policy.
	svc := NewHTTPService(newFakeCaller(), Actions())
	seen := map[string]int{}
	for _, r := range svc.HTTPRoutes() {
		if r.SecurityRequirement != nil && string(r.SecurityRequirement.SecurityLevel) == "none" {
			seen[r.Path]++
		}
	}
	if len(seen) != 2 || seen["/v1/webhooks"] != 1 || seen["/v1/completions"] != 1 {
		t.Fatalf("none-level routes: %v — the keyless set is exactly the webhook delivery and the remote completion, each once, each HMAC-first", seen)
	}
}

func TestBoundaryStatementNamesTheWebhookAsWhatItIs(t *testing.T) {
	// The statement a human reads when they knock for operational control
	// must not file a signed POST under "Available:" as if it were a read.
	line := BoundaryStatement()
	if !strings.Contains(line, "aeon_webhook_deliver (webhook)") {
		t.Fatalf("the webhook must be named as a webhook in the boundary statement:\n%s", line)
	}
}
