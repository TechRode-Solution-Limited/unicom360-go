package unicom360_test

// Shared helpers for the conformance harness: fixture loading, strict decoding, the
// canonical form of results, canonical comparison and a fake transport.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

const fixtureDir = "testdata/conformance"

// readFixture reads a vendored fixture file. When the whole testdata directory is absent the
// tests are running from a module zip (testdata is kept out of the published module), so they
// skip with a message; a partial copy fails.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	if _, err := os.Stat("testdata"); os.IsNotExist(err) {
		if os.Getenv("UNICOM360_REQUIRE_FIXTURES") != "" {
			t.Fatal("testdata/ is missing and UNICOM360_REQUIRE_FIXTURES is set")
		}
		t.Skip("testdata/ is not part of the published module: run the conformance suite from a checkout of the repository")
	}
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// strictDecode decodes b into v, failing on any field v does not declare: a vector the
// harness does not understand is a failure, never a silent skip.
func strictDecode(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

// decodeAny decodes JSON keeping numbers as json.Number.
func decodeAny(b []byte) any {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Sprintf("<invalid JSON: %v>", err)
	}
	return v
}

// fixtureFile is the common shape of every fixture file.
type fixtureFile struct {
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Vectors     []json.RawMessage `json:"vectors"`
}

// ---------------------------------------------------------------------------------------
// Canonical form
// ---------------------------------------------------------------------------------------

var (
	rawMessageType = reflect.TypeOf(json.RawMessage(nil))
	decimalType    = reflect.TypeOf(unicom360.Decimal(""))
)

// canonical builds the canonical JSON value of a model from its typed fields (named by their
// json tags, the API's names), adding every unknown field the model kept in Raw. fixRaw, for a
// top-level result, turns the envelope into the shape the typed fields describe (drop the
// success or status flag, flatten an object data).
func canonical(v any, fixRaw func(map[string]any)) any {
	return canonValue(reflect.ValueOf(v), fixRaw)
}

func canonValue(v reflect.Value, fixRaw func(map[string]any)) any {
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return canonValue(v.Elem(), fixRaw)
	case reflect.Struct:
		return canonStruct(v, fixRaw)
	case reflect.Slice:
		if v.Type() == rawMessageType {
			if v.IsNil() {
				return nil
			}
			return decodeAny(v.Bytes())
		}
		if v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := range out {
			out[i] = canonValue(v.Index(i), nil)
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := map[string]any{}
		for _, k := range v.MapKeys() {
			out[k.String()] = canonValue(v.MapIndex(k), nil)
		}
		return out
	case reflect.String:
		if v.Len() == 0 {
			return nil // "" is how the models carry an absent or null string
		}
		if v.Type() == decimalType {
			return json.Number(v.String())
		}
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int64, reflect.Int32:
		return json.Number(strconv.FormatInt(v.Int(), 10))
	case reflect.Float64, reflect.Float32:
		return json.Number(strconv.FormatFloat(v.Float(), 'g', -1, 64))
	}
	panic("canonical: unsupported kind " + v.Kind().String())
}

func canonStruct(v reflect.Value, fixRaw func(map[string]any)) any {
	t := v.Type()
	out := map[string]any{}
	known := map[string]bool{}
	var raw json.RawMessage
	hasRaw := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Name == "Raw" && f.Type == rawMessageType {
			raw, hasRaw = v.Field(i).Interface().(json.RawMessage), true
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		known[name] = true
		if val := canonValue(v.Field(i), nil); val != nil {
			out[name] = val
		}
	}
	if hasRaw {
		if raw == nil {
			return nil // the model was not parsed from an object: absent
		}
		if m, ok := decodeAny(raw).(map[string]any); ok {
			if fixRaw != nil {
				fixRaw(m)
			}
			for k, x := range m {
				if !known[k] {
					out[k] = x
				}
			}
		}
	}
	return out
}

// envelopeFix flattens a result envelope: success and data go, an object data's members move
// up (a list data is the typed Results).
func envelopeFix(m map[string]any) {
	delete(m, "success")
	data := m["data"]
	delete(m, "data")
	if obj, ok := data.(map[string]any); ok {
		for k, x := range obj {
			m[k] = x
		}
	}
}

func balanceFix(m map[string]any) { delete(m, "status") }

// canonicalResult is the canonical form of an operation's result.
func canonicalResult(op string, result any) any {
	switch op {
	case "health":
		return canonical(result, nil)
	case "balance.get", "balance.all":
		return canonical(result, balanceFix)
	}
	return canonical(result, envelopeFix)
}

// ---------------------------------------------------------------------------------------
// Canonical comparison
// ---------------------------------------------------------------------------------------

func dropNulls(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if e != nil {
				out[k] = dropNulls(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = dropNulls(e)
		}
		return out
	}
	return v
}

// canonicalEqual compares canonical JSON values: nulls dropped, key order ignored, arrays by
// order, numbers by exact value.
func canonicalEqual(a, b any) bool {
	return deepEqualJSON(dropNulls(a), dropNulls(b))
}

func deepEqualJSON(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, e := range x {
			f, ok := y[k]
			if !ok || !deepEqualJSON(e, f) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEqualJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		p, ok1 := new(big.Rat).SetString(string(x))
		q, ok2 := new(big.Rat).SetString(string(y))
		return ok1 && ok2 && p.Cmp(q) == 0
	}
	return reflect.DeepEqual(a, b)
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------------------------------------------------------------------------------------
// Fake transport
// ---------------------------------------------------------------------------------------

// recorded is a request the fake transport saw, its body already read.
type recorded struct {
	req  *http.Request
	body []byte
}

// fakeTransport answers each request with the next reply (the last one repeats) and records
// what it was sent.
type fakeTransport struct {
	mu       sync.Mutex
	replies  []func(*http.Request) (*http.Response, error)
	requests []recorded
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	f.mu.Lock()
	n := len(f.requests)
	f.requests = append(f.requests, recorded{req: req, body: body})
	reply := f.replies[min(n, len(f.replies)-1)]
	f.mu.Unlock()
	return reply(req)
}

func (f *fakeTransport) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// httpReply is a canned HTTP response as the fixtures write it.
type httpReply struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	BodyText string            `json:"body_text"`
}

func (h httpReply) respond(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	for k, v := range h.Headers {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode: h.Status,
		Status:     fmt.Sprintf("%d %s", h.Status, http.StatusText(h.Status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(h.BodyText)),
		Request:    req,
		Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
	}, nil
}

// ---------------------------------------------------------------------------------------
// Fixture tree hash (as scripts/sync-conformance computes it)
// ---------------------------------------------------------------------------------------

func treeHash(dir string) (string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "VERSION" {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files) // byte order, like Buffer.compare
	var listing strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(b)
		listing.WriteString(hex.EncodeToString(sum[:]) + "  " + f + "\n")
	}
	sum := sha256.Sum256([]byte(listing.String()))
	return hex.EncodeToString(sum[:]), nil
}
