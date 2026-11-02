//go:build contract

package unicom360_test

// The contract test sends every requests.json call to a Prism mock of openapi.yaml, which
// validates each request (and its own response) against the spec and reports any violation in
// an sl-violations header. Any violation fails the test.
//
// Run Prism, then the test (see CONTRIBUTING.md):
//
//	docker run -d --name prism -p 4013:4010 -v "$PWD/../unicom360-openapi:/spec:ro" \
//	  stoplight/prism:5 mock -h 0.0.0.0 --multiprocess=false --errors /spec/openapi.yaml
//	UNICOM360_CONTRACT_BASE_URL=http://127.0.0.1:4013 go test -tags contract -run Contract ./...
//
// --multiprocess=false: Prism 5's default multi-process mode crashes in its image.
// --errors: a request or response that breaks the spec gets an error answer, not just the
// sl-violations header; the test fails on either.
//
// The base URL is plain http on a loopback address, which the client allows. When the test
// runs in a container, share Prism's network namespace (docker run --network container:prism)
// and set UNICOM360_CONTRACT_BASE_URL=http://127.0.0.1:4010 (Prism's own port); no test-only
// exception to the base URL rules is needed. Without the variable the test uses
// http://127.0.0.1:4013.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

func contractBaseURL() string {
	if u := os.Getenv("UNICOM360_CONTRACT_BASE_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:4013"
}

// violationRecorder passes requests to the network and keeps every sl-violations header.
type violationRecorder struct {
	mu         sync.Mutex
	violations []string
	statuses   []int
}

func (v *violationRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if resp != nil {
		v.mu.Lock()
		v.statuses = append(v.statuses, resp.StatusCode)
		if h := resp.Header.Get("sl-violations"); h != "" {
			v.violations = append(v.violations, h)
		}
		v.mu.Unlock()
	}
	return resp, err
}

func (v *violationRecorder) take() ([]string, []int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out, st := v.violations, v.statuses
	v.violations, v.statuses = nil, nil
	return out, st
}

func contractClient(t *testing.T, rec *violationRecorder) *unicom360.Client {
	t.Helper()
	c, err := unicom360.NewClient(
		unicom360.WithAPIKey("contract-test-key"),
		unicom360.WithBaseURL(contractBaseURL()),
		unicom360.WithTransport(rec),
		unicom360.WithMaxRetries(0),
		unicom360.WithTimeout(30*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Fail fast with a clear message when Prism is not running.
	if _, err := c.Health(context.Background()); errors.Is(err, unicom360.ErrConnection) {
		t.Fatalf("Prism is not reachable at %s: %v", contractBaseURL(), err)
	}
	rec.take()
	return c
}

func TestContractEveryRequestMatchesTheSpec(t *testing.T) {
	rec := &violationRecorder{}
	c := contractClient(t, rec)
	ops := map[string]bool{}
	vectors := loadVectors(t, "requests.json", &requestsFile{})
	runVectors(t, "requests.json", vectors, func(v requestVector) string { return v.ID }, func(t *testing.T, v requestVector) {
		ops[v.Operation] = true
		result, err := invoke(context.Background(), c, v.Operation, v.Params)
		violations, statuses := rec.take()
		if len(violations) > 0 {
			t.Fatalf("Prism reported violations: %v", violations)
		}
		if err != nil {
			t.Fatalf("call failed (HTTP %v): %v", statuses, err)
		}
		if len(statuses) != 1 || statuses[0] < 200 || statuses[0] > 299 {
			t.Fatalf("HTTP %v, want one 2xx", statuses)
		}
		if b, _ := json.Marshal(canonicalResult(v.Operation, result)); len(b) < 3 {
			t.Fatalf("empty result: %s", b)
		}
	})
	for _, op := range []string{"sms.send", "email.send", "whatsapp.send_template", "whatsapp.send_session",
		"whatsapp.status", "whatsapp.templates", "balance.get", "balance.all", "health"} {
		if !ops[op] {
			t.Errorf("no contract call for %s", op)
		}
	}
}

// A request the spec forbids must be reported, or the test above proves nothing.
func TestContractDetectsViolations(t *testing.T) {
	rec := &violationRecorder{}
	c := contractClient(t, rec)
	_, err := c.SMS.Send(context.Background(), unicom360.SMSSendParams{From: "Unicom360", ToOne: "260971234567"})
	violations, _ := rec.take()
	if len(violations) == 0 {
		t.Fatal("Prism reported no violation for an SMS without a message")
	}
	if !errors.Is(err, unicom360.ErrInvalidRequest) {
		t.Fatalf("got %v, want an InvalidRequestError", err)
	}
}
