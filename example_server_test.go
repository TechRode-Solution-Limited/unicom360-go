package unicom360_test

// The examples call NewClient() with no options, so the client reads UNICOM360_API_KEY and
// UNICOM360_BASE_URL. TestMain points those at a local fake of the API, which lets every
// Example run (and check its output) without a network or an account. Real code points at
// https://api.unicom360.com, the default.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const exampleAPIKey = "example-api-key"

func TestMain(m *testing.M) {
	srv := httptest.NewServer(http.HandlerFunc(fakeAPI))
	_ = os.Setenv("UNICOM360_API_KEY", exampleAPIKey)
	_ = os.Setenv("UNICOM360_BASE_URL", srv.URL)
	code := m.Run()
	srv.Close()
	os.Exit(code)
}

func fakeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.URL.Path != "/health" && r.Header.Get("X-API-Key") != exampleAPIKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"AUTHENTICATION_FAILED","success":false,"api_reference":"1764184470578998354"}`)
		return
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	const ref = `"api_reference":"1764184470578998353"`
	recipients := func() []string {
		switch to := req["to"].(type) {
		case string:
			return []string{to}
		case []any:
			out := make([]string, len(to))
			for i, v := range to {
				out[i], _ = v.(string)
			}
			return out
		}
		return nil
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/v1/json/bulk/sms":
		if req["from"] != "Unicom360" {
			_, _ = io.WriteString(w, `{"message":"Invalid From Sender ID","success":false,`+ref+`}`)
			return
		}
		var items []string
		for i, n := range recipients() {
			n = strings.TrimPrefix(n, "0")
			items = append(items, fmt.Sprintf(`{"message_id":"dff31690-b450-400%d-a6b7-141d8523ef10","status":"ACKNOWLEDGED","mobile_number":%q}`, i, n))
		}
		_, _ = io.WriteString(w, `{`+ref+`,"data":[`+strings.Join(items, ",")+`],"message":"successful","success":true}`)
	case "POST /api/v1/json/bulk/email":
		var items []string
		for i, a := range recipients() {
			items = append(items, fmt.Sprintf(`{"message_id":"%d","status":"ACKNOWLEDGED","email_address":%q}`, 48213+i, a))
		}
		_, _ = io.WriteString(w, `{`+ref+`,"data":[`+strings.Join(items, ",")+`],"message":"successful","success":true}`)
	case "POST /api/v1/json/whatsapp/template":
		_, _ = io.WriteString(w, `{`+ref+`,"data":{"accepted":1,"rejected":1,"results":[`+
			`{"category":"UTILITY","message_id":"7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f","reason":null,"status":"ACKNOWLEDGED","to":"260971234567","user_reference_id":"inv-42"},`+
			`{"category":"UTILITY","message_id":null,"reason":"INVALID_NUMBER","status":"REJECTED","to":"12","user_reference_id":"inv-43"}]},"message":"successful","success":true}`)
	case "POST /api/v1/json/whatsapp/session":
		uref, _ := json.Marshal(req["user_reference_id"])
		to, _ := json.Marshal(req["to"])
		_, _ = io.WriteString(w, `{`+ref+`,"data":{"category":"SERVICE","message_id":"9b2f6c1e-0d4a-4c7b-8e21-5a3f00000001","status":"ACKNOWLEDGED","to":`+
			string(to)+`,"user_reference_id":`+string(uref)+`},"message":"successful","success":true}`)
	case "POST /api/v1/json/whatsapp/status":
		_, _ = io.WriteString(w, `{`+ref+`,"data":{"results":[`+
			`{"billable":true,"category":"UTILITY","delivered_at":"2026-10-24T08:16:02Z","error":null,"from":"260960123456","message_id":"7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f","read_at":"2026-10-24T08:20:45Z","refunded":false,"sent_at":"2026-10-24T08:15:31Z","status":"READ","to":"260971234567","user_reference_id":"inv-42"},`+
			`{"message_id":"00000000-0000-4000-8000-000000000000","status":"NOT_FOUND"}]},"message":"successful","success":true}`)
	case "GET /api/v1/json/whatsapp/templates":
		_, _ = io.WriteString(w, `{`+ref+`,"data":{"templates":[`+
			`{"authentication":null,"body":{"text":"Hi {{1}}, your order {{2}} is ready for collection.","variables":["1","2"]},"buttons":[{"index":0,"text":"Track order","type":"url","url":"https://example.com/orders/{{1}}"}],"category":"UTILITY","footer":"Reply STOP to opt out","from":"260960123456","header":{"type":"image","variables":[]},"language":"en_US","name":"order_ready"},`+
			`{"authentication":{"code_expiration_minutes":10,"otp_type":"COPY_CODE"},"body":{"text":"{{1}} is your verification code.","variables":[]},"buttons":[{"index":0,"otp_type":"COPY_CODE","type":"otp"}],"category":"AUTHENTICATION","footer":null,"from":"260960123456","header":null,"language":"en","name":"login_code"}]},"message":"successful","success":true}`)
	case "POST /api/v1/json/balance":
		bundle, _ := req["bundle_type"].(string)
		balance := map[string]string{"": "1250", "SMS": "1250", "WHATSAPP": "37.5", "EMAIL": "4000"}[bundle]
		if bundle == "" {
			bundle = "SMS"
		}
		fmt.Fprintf(w, `{"status":true,"message":"Balance retrieved successfully","balance":%s,"bundle_type":%q,"client_id":"UC-00042","client_name":"Acme Ltd"}`, balance, bundle)
	case "POST /api/v1/json/balance/all":
		_, _ = io.WriteString(w, `{"status":true,"message":"All balances retrieved successfully","balances":{"SMS":1250,"EMAIL":4000,"WHATSAPP":37.5},"client_id":"UC-00042","client_name":"Acme Ltd"}`)
	case "GET /health":
		_, _ = io.WriteString(w, `{"status":"ok","timestamp":"2026-10-22T08:15:30.412345Z","version":"0.1.0","environment":"production"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"errors":{"detail":"Not Found"}}`)
	}
}
