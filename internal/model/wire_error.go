package model

import (
	"fmt"
	"net/http"
	"strings"
)

// withCall says WHICH request failed: the method, the address (credentials in it hidden), the route and the model name.
// A provider's own answer ("Not Found") says nothing about what was asked for, and the address is the first thing a
// person checks. A 404 from a path with no /v1 in it also gets the usual cause named: these endpoints are appended to the
// route's base URL as configured, so a server that serves /v1/... needs a base URL that ends in /v1.
func withCall(err error, req *http.Request, route Route) error {
	if err == nil || req == nil || req.URL == nil {
		return err
	}
	where := req.URL.Redacted()
	out := fmt.Errorf("%w (called %s %s, route %q, model %q)", err, req.Method, where, route.ID, route.Model)
	if strings.Contains(err.Error(), "HTTP 404") && !strings.Contains(req.URL.Path, "/v1/") {
		out = fmt.Errorf("%w — the address has no /v1 in it; if the server serves it under /v1 (as OpenAI-compatible servers usually do), set this model's Base URL to end in /v1", out)
	}
	return out
}
