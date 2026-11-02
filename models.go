package unicom360

import (
	"encoding/json"
	"strings"
)

// Model conventions, shared by every result type:
//
//   - Field names follow the API's snake_case names in Go form (message_id is MessageID).
//   - A string field the API sent as null, or did not send, is "". Fields whose null carries
//     meaning beyond "none" (booleans, integers, nested objects) are pointers and nil.
//   - Raw is the JSON object the model was parsed from, unmodified, so a field the gateway adds
//     later is readable before the SDK knows it.
//   - Status, category, reason and type values are open sets: the constants below name the
//     known ones, and any other string is passed through as is.
//   - message_id is always a string, even when the gateway sends a JSON number.

// Known status values. Statuses are open: compare with these, but expect others.
const (
	// SMS, email and WhatsApp template results.
	StatusAcknowledged        = "ACKNOWLEDGED"
	StatusRejected            = "REJECTED"
	StatusInvalidMobileNumber = "Invalid mobile number"
	StatusInvalidEmailAddress = "Invalid email address"

	// WhatsApp message status (WhatsApp.Status).
	StatusAccepted  = "ACCEPTED"
	StatusSent      = "SENT"
	StatusDelivered = "DELIVERED"
	StatusRead      = "READ"
	StatusFailed    = "FAILED"
	StatusNotFound  = "NOT_FOUND"
)

// Bundle types for Balance.Get. The gateway reports 0 for a type it does not know.
const (
	BundleSMS              = "SMS"
	BundleSMSInternational = "SMS_INTERNATIONAL"
	BundleEmail            = "EMAIL"
	BundleWhatsApp         = "WHATSAPP"
)

// ---------------------------------------------------------------------------------------
// SMS
// ---------------------------------------------------------------------------------------

// SMSSendResult is the result of SMS.Send: one entry per recipient. Results are not in request
// order past the tenth recipient; match them with ByNumber.
type SMSSendResult struct {
	APIReference string          `json:"api_reference"`
	Message      string          `json:"message"`
	Results      []SMSResult     `json:"results"`
	Raw          json.RawMessage `json:"-"`
}

// SMSResult is one recipient's result.
type SMSResult struct {
	MessageID string `json:"message_id"`
	// Status is StatusAcknowledged, StatusInvalidMobileNumber or another value.
	Status string `json:"status"`
	// MobileNumber is the number as the gateway normalised it (digits, country code first),
	// or as sent when it could not be parsed.
	MobileNumber string          `json:"mobile_number"`
	Raw          json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *SMSResult) UnmarshalJSON(b []byte) error {
	var a struct {
		MessageID    flexString `json:"message_id"`
		Status       flexString `json:"status"`
		MobileNumber flexString `json:"mobile_number"`
	}
	*r = SMSResult{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		r.MessageID, r.Status, r.MobileNumber = string(a.MessageID), string(a.Status), string(a.MobileNumber)
	}
	return nil
}

// Accepted returns the results whose status is exactly ACKNOWLEDGED, in response order.
func (r *SMSSendResult) Accepted() []*SMSResult {
	return filter(r.Results, func(x *SMSResult) bool { return x.Status == StatusAcknowledged })
}

// Invalid returns every result that was not ACKNOWLEDGED, in response order.
func (r *SMSSendResult) Invalid() []*SMSResult {
	return filter(r.Results, func(x *SMSResult) bool { return x.Status != StatusAcknowledged })
}

// ByNumber returns the first result whose mobile number has the same digits as number (every
// non-digit is ignored on both sides, so "+260 97 123 4567" finds "260971234567"), or nil.
// Local formats are not expanded: pass the international form.
func (r *SMSSendResult) ByNumber(number string) *SMSResult {
	return findNumber(r.Results, number, func(x *SMSResult) string { return x.MobileNumber })
}

// ---------------------------------------------------------------------------------------
// Email
// ---------------------------------------------------------------------------------------

// EmailSendResult is the result of Email.Send: one entry per address. Results are not in
// request order past the tenth address; match them with ByAddress.
type EmailSendResult struct {
	APIReference string          `json:"api_reference"`
	Message      string          `json:"message"`
	Results      []EmailResult   `json:"results"`
	Raw          json.RawMessage `json:"-"`
}

// EmailResult is one address's result.
type EmailResult struct {
	// MessageID is the email's id: decimal digits. The delivery report sends the same id.
	MessageID string `json:"message_id"`
	// Status is StatusAcknowledged, StatusInvalidEmailAddress or another value.
	Status       string          `json:"status"`
	EmailAddress string          `json:"email_address"`
	Raw          json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *EmailResult) UnmarshalJSON(b []byte) error {
	var a struct {
		MessageID    flexString `json:"message_id"`
		Status       flexString `json:"status"`
		EmailAddress flexString `json:"email_address"`
	}
	*r = EmailResult{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		r.MessageID, r.Status, r.EmailAddress = string(a.MessageID), string(a.Status), string(a.EmailAddress)
	}
	return nil
}

// Accepted returns the results whose status is exactly ACKNOWLEDGED, in response order.
func (r *EmailSendResult) Accepted() []*EmailResult {
	return filter(r.Results, func(x *EmailResult) bool { return x.Status == StatusAcknowledged })
}

// Invalid returns every result that was not ACKNOWLEDGED, in response order.
func (r *EmailSendResult) Invalid() []*EmailResult {
	return filter(r.Results, func(x *EmailResult) bool { return x.Status != StatusAcknowledged })
}

// ByAddress returns the first result whose address equals address, ignoring surrounding
// whitespace and case, or nil.
func (r *EmailSendResult) ByAddress(address string) *EmailResult {
	want := strings.ToLower(trimJSSpace(address))
	for i := range r.Results {
		if strings.ToLower(trimJSSpace(r.Results[i].EmailAddress)) == want {
			return &r.Results[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------------------
// WhatsApp template sends
// ---------------------------------------------------------------------------------------

// TemplateSendResult is the result of WhatsApp.SendTemplate. A send with some rejected
// recipients is still a result (success: true); see Rejected and each result's Reason.
type TemplateSendResult struct {
	APIReference string `json:"api_reference"`
	Message      string `json:"message"`
	// AcceptedCount is the API's data.accepted: how many recipients were queued.
	AcceptedCount int `json:"accepted"`
	// RejectedCount is the API's data.rejected.
	RejectedCount int `json:"rejected"`
	// Results has one entry per recipient, in request order.
	Results []TemplateResult `json:"results"`
	Raw     json.RawMessage  `json:"-"`
}

// TemplateResult is one template recipient's result.
type TemplateResult struct {
	// MessageID is the queued message's id. For a DUPLICATE_MSG_REF rejection it is the
	// message the reference already went out as; otherwise "" for a rejected recipient.
	MessageID string `json:"message_id"`
	// To is the normalised number, or for a rejected recipient the value as sent.
	To              string `json:"to"`
	UserReferenceID string `json:"user_reference_id"`
	// Status is StatusAcknowledged or StatusRejected (open).
	Status string `json:"status"`
	// Reason is "" when accepted, else a code such as INVALID_NUMBER (open).
	Reason string `json:"reason"`
	// Category is the template's Meta category (MARKETING, UTILITY, AUTHENTICATION, SERVICE).
	Category string          `json:"category"`
	Raw      json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *TemplateResult) UnmarshalJSON(b []byte) error {
	var a struct {
		MessageID       flexString `json:"message_id"`
		To              flexString `json:"to"`
		UserReferenceID flexString `json:"user_reference_id"`
		Status          flexString `json:"status"`
		Reason          flexString `json:"reason"`
		Category        flexString `json:"category"`
	}
	*r = TemplateResult{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		r.MessageID, r.To, r.UserReferenceID = string(a.MessageID), string(a.To), string(a.UserReferenceID)
		r.Status, r.Reason, r.Category = string(a.Status), string(a.Reason), string(a.Category)
	}
	return nil
}

// Accepted returns the results whose status is exactly ACKNOWLEDGED, in response order.
func (r *TemplateSendResult) Accepted() []*TemplateResult {
	return filter(r.Results, func(x *TemplateResult) bool { return x.Status == StatusAcknowledged })
}

// Rejected returns the results whose status is exactly REJECTED, in response order.
func (r *TemplateSendResult) Rejected() []*TemplateResult {
	return filter(r.Results, func(x *TemplateResult) bool { return x.Status == StatusRejected })
}

// ByNumber returns the first result whose To has the same digits as number, or nil.
func (r *TemplateSendResult) ByNumber(number string) *TemplateResult {
	return findNumber(r.Results, number, func(x *TemplateResult) string { return x.To })
}

type templateSendData struct {
	Accepted flexInt          `json:"accepted"`
	Rejected flexInt          `json:"rejected"`
	Results  []TemplateResult `json:"results"`
}

// ---------------------------------------------------------------------------------------
// WhatsApp session sends
// ---------------------------------------------------------------------------------------

// SessionSendResult is the result of WhatsApp.SendSession.
type SessionSendResult struct {
	APIReference    string `json:"api_reference"`
	Message         string `json:"message"`
	MessageID       string `json:"message_id"`
	To              string `json:"to"`
	UserReferenceID string `json:"user_reference_id"`
	// Status is StatusAcknowledged (open).
	Status string `json:"status"`
	// Category is SERVICE (open).
	Category string          `json:"category"`
	Raw      json.RawMessage `json:"-"`
}

type sessionSendData struct {
	MessageID       flexString `json:"message_id"`
	To              flexString `json:"to"`
	UserReferenceID flexString `json:"user_reference_id"`
	Status          flexString `json:"status"`
	Category        flexString `json:"category"`
}

// ---------------------------------------------------------------------------------------
// WhatsApp status
// ---------------------------------------------------------------------------------------

// StatusResult is the result of WhatsApp.Status: one entry per distinct id asked for.
type StatusResult struct {
	APIReference string          `json:"api_reference"`
	Message      string          `json:"message"`
	Results      []MessageStatus `json:"results"`
	Raw          json.RawMessage `json:"-"`
}

// ByID returns the status of messageID, or nil.
func (r *StatusResult) ByID(messageID string) *MessageStatus {
	for i := range r.Results {
		if r.Results[i].MessageID == messageID {
			return &r.Results[i]
		}
	}
	return nil
}

// MessageStatus is one WhatsApp message's status. A NOT_FOUND entry carries only MessageID
// and Status.
type MessageStatus struct {
	MessageID string `json:"message_id"`
	// Status is one of ACCEPTED, SENT, DELIVERED, READ, FAILED, REJECTED, NOT_FOUND (open).
	Status          string `json:"status"`
	UserReferenceID string `json:"user_reference_id"`
	To              string `json:"to"`
	From            string `json:"from"`
	// SentAt, DeliveredAt and ReadAt are ISO 8601 timestamps as received, or "".
	SentAt      string        `json:"sent_at"`
	DeliveredAt string        `json:"delivered_at"`
	ReadAt      string        `json:"read_at"`
	Error       *MessageError `json:"error"`
	Category    string        `json:"category"`
	// Billable is whether the message was charged when sent (nil when unknown).
	Billable *bool `json:"billable"`
	// Refunded is whether that charge was given back (nil on NOT_FOUND).
	Refunded *bool           `json:"refunded"`
	Raw      json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *MessageStatus) UnmarshalJSON(b []byte) error {
	var a struct {
		MessageID       flexString    `json:"message_id"`
		Status          flexString    `json:"status"`
		UserReferenceID flexString    `json:"user_reference_id"`
		To              flexString    `json:"to"`
		From            flexString    `json:"from"`
		SentAt          flexString    `json:"sent_at"`
		DeliveredAt     flexString    `json:"delivered_at"`
		ReadAt          flexString    `json:"read_at"`
		Error           *MessageError `json:"error"`
		Category        flexString    `json:"category"`
		Billable        flexBool      `json:"billable"`
		Refunded        flexBool      `json:"refunded"`
	}
	*s = MessageStatus{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		s.MessageID, s.Status, s.UserReferenceID = string(a.MessageID), string(a.Status), string(a.UserReferenceID)
		s.To, s.From = string(a.To), string(a.From)
		s.SentAt, s.DeliveredAt, s.ReadAt = string(a.SentAt), string(a.DeliveredAt), string(a.ReadAt)
		s.Error = a.Error.orNil()
		s.Category, s.Billable, s.Refunded = string(a.Category), a.Billable.V, a.Refunded.V
	}
	return nil
}

// MessageError is a WhatsApp delivery error: Meta's code when Meta gave one, and a message.
type MessageError struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Raw     json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (e *MessageError) UnmarshalJSON(b []byte) error {
	var a struct {
		Code    flexInt    `json:"code"`
		Message flexString `json:"message"`
	}
	*e = MessageError{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		e.Code, e.Message = a.Code.V, string(a.Message)
	}
	return nil
}

// orNil returns nil for a MessageError that was not a JSON object (null included).
func (e *MessageError) orNil() *MessageError {
	if e == nil || !isJSONObject(e.Raw) {
		return nil
	}
	return e
}

// ---------------------------------------------------------------------------------------
// WhatsApp templates
// ---------------------------------------------------------------------------------------

// TemplateList is the result of WhatsApp.Templates: your approved templates.
type TemplateList struct {
	APIReference string          `json:"api_reference"`
	Message      string          `json:"message"`
	Templates    []TemplateInfo  `json:"templates"`
	Raw          json.RawMessage `json:"-"`
}

// ByName returns the first template named name, or nil. A non-empty language must match too;
// "" matches any language.
func (l *TemplateList) ByName(name, language string) *TemplateInfo {
	for i := range l.Templates {
		t := &l.Templates[i]
		if t.Name == name && (language == "" || t.Language == language) {
			return t
		}
	}
	return nil
}

// TemplateInfo describes one approved template.
type TemplateInfo struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	// Category is MARKETING, UTILITY or AUTHENTICATION (open).
	Category string `json:"category"`
	// From is the number the template was made on.
	From           string                  `json:"from"`
	Header         *TemplateHeaderInfo     `json:"header"`
	Body           TemplateBodyInfo        `json:"body"`
	Footer         string                  `json:"footer"`
	Buttons        []TemplateButtonInfo    `json:"buttons"`
	Authentication *TemplateAuthentication `json:"authentication"`
	Raw            json.RawMessage         `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *TemplateInfo) UnmarshalJSON(b []byte) error {
	var a struct {
		Name           flexString              `json:"name"`
		Language       flexString              `json:"language"`
		Category       flexString              `json:"category"`
		From           flexString              `json:"from"`
		Header         *TemplateHeaderInfo     `json:"header"`
		Body           TemplateBodyInfo        `json:"body"`
		Footer         flexString              `json:"footer"`
		Buttons        json.RawMessage         `json:"buttons"`
		Authentication *TemplateAuthentication `json:"authentication"`
	}
	*t = TemplateInfo{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		t.Name, t.Language, t.Category, t.From = string(a.Name), string(a.Language), string(a.Category), string(a.From)
		if a.Header != nil && isJSONObject(a.Header.Raw) {
			t.Header = a.Header
		}
		t.Body = a.Body
		t.Footer = string(a.Footer)
		if isJSONArray(a.Buttons) {
			_ = json.Unmarshal(a.Buttons, &t.Buttons)
		}
		if a.Authentication != nil && isJSONObject(a.Authentication.Raw) {
			t.Authentication = a.Authentication
		}
	}
	return nil
}

// TemplateHeaderInfo is a template's header: its type (text, image, video, document; open)
// and the names of its variables.
type TemplateHeaderInfo struct {
	Type      string          `json:"type"`
	Variables []string        `json:"variables"`
	Raw       json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (h *TemplateHeaderInfo) UnmarshalJSON(b []byte) error {
	var a struct {
		Type      flexString  `json:"type"`
		Variables flexStrings `json:"variables"`
	}
	*h = TemplateHeaderInfo{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		h.Type, h.Variables = string(a.Type), a.Variables
	}
	return nil
}

// TemplateBodyInfo is a template's body text and the names of its variables, in order.
type TemplateBodyInfo struct {
	Text      string          `json:"text"`
	Variables []string        `json:"variables"`
	Raw       json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (h *TemplateBodyInfo) UnmarshalJSON(b []byte) error {
	var a struct {
		Text      flexString  `json:"text"`
		Variables flexStrings `json:"variables"`
	}
	*h = TemplateBodyInfo{}
	if isJSONObject(b) {
		h.Raw = rawOrNil(b)
	}
	if decodeObject(b, &a) {
		h.Text, h.Variables = string(a.Text), a.Variables
	}
	return nil
}

// TemplateButtonInfo is one of a template's buttons. Type is url, quick_reply, phone_number
// or otp (open). A {{1}} in URL means the send must give the button's value.
type TemplateButtonInfo struct {
	Index   int             `json:"index"`
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	URL     string          `json:"url"`
	OTPType string          `json:"otp_type"`
	Raw     json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *TemplateButtonInfo) UnmarshalJSON(b []byte) error {
	var a struct {
		Index   flexInt    `json:"index"`
		Type    flexString `json:"type"`
		Text    flexString `json:"text"`
		URL     flexString `json:"url"`
		OTPType flexString `json:"otp_type"`
	}
	*t = TemplateButtonInfo{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		t.Index, t.Type, t.Text, t.URL, t.OTPType = a.Index.Value(), string(a.Type), string(a.Text), string(a.URL), string(a.OTPType)
	}
	return nil
}

// TemplateAuthentication describes an AUTHENTICATION template's one-time code.
type TemplateAuthentication struct {
	OTPType               string          `json:"otp_type"`
	CodeExpirationMinutes *int            `json:"code_expiration_minutes"`
	Raw                   json.RawMessage `json:"-"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *TemplateAuthentication) UnmarshalJSON(b []byte) error {
	var a struct {
		OTPType               flexString `json:"otp_type"`
		CodeExpirationMinutes flexInt    `json:"code_expiration_minutes"`
	}
	*t = TemplateAuthentication{Raw: rawOrNil(b)}
	if decodeObject(b, &a) {
		t.OTPType, t.CodeExpirationMinutes = string(a.OTPType), a.CodeExpirationMinutes.V
	}
	return nil
}

// ---------------------------------------------------------------------------------------
// Balance and health
// ---------------------------------------------------------------------------------------

// Balance is the result of Balance.Get: the units remaining on one bundle.
type Balance struct {
	Message string `json:"message"`
	// Balance is the units remaining, exactly as sent (see Decimal).
	Balance    Decimal         `json:"balance"`
	BundleType string          `json:"bundle_type"`
	ClientID   string          `json:"client_id"`
	ClientName string          `json:"client_name"`
	Raw        json.RawMessage `json:"-"`
}

// Balances is the result of Balance.All: every bundle's units remaining.
type Balances struct {
	Message string `json:"message"`
	// Balances maps a bundle type (SMS, EMAIL, WHATSAPP, ...) to its units remaining.
	Balances   map[string]Decimal `json:"balances"`
	ClientID   string             `json:"client_id"`
	ClientName string             `json:"client_name"`
	Raw        json.RawMessage    `json:"-"`
}

// Health is the result of Client.Health.
type Health struct {
	// Status is "ok" when the gateway is up.
	Status string `json:"status"`
	// Timestamp is the gateway's time, ISO 8601 as received.
	Timestamp   string          `json:"timestamp"`
	Version     string          `json:"version"`
	Environment string          `json:"environment"`
	Raw         json.RawMessage `json:"-"`
}

// ---------------------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------------------

type envelope struct {
	APIReference flexString      `json:"api_reference"`
	Message      json.RawMessage `json:"message"`
	Data         json.RawMessage `json:"data"`
}

func decodeEnvelope(body []byte) envelope {
	var env envelope
	_ = json.Unmarshal(body, &env)
	return env
}

func (env envelope) text() string {
	t, _ := messageText(env.Message)
	return t
}

// messageText reads an envelope's message: a string as is, an array of strings joined with
// "; ". It reports false for anything else (absent, null, a number, a mixed array).
func messageText(raw json.RawMessage) (string, bool) {
	switch firstByte(raw) {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s, true
		}
	case '[':
		var parts []json.RawMessage
		if json.Unmarshal(raw, &parts) != nil {
			return "", false
		}
		out := make([]string, len(parts))
		for i, p := range parts {
			if firstByte(p) != '"' || json.Unmarshal(p, &out[i]) != nil {
				return "", false
			}
		}
		return strings.Join(out, "; "), true
	}
	return "", false
}

func decodeSMSSendResult(body []byte) *SMSSendResult {
	env := decodeEnvelope(body)
	r := &SMSSendResult{APIReference: string(env.APIReference), Message: env.text(), Raw: body}
	if isJSONArray(env.Data) {
		_ = json.Unmarshal(env.Data, &r.Results)
	}
	return r
}

func decodeEmailSendResult(body []byte) *EmailSendResult {
	env := decodeEnvelope(body)
	r := &EmailSendResult{APIReference: string(env.APIReference), Message: env.text(), Raw: body}
	if isJSONArray(env.Data) {
		_ = json.Unmarshal(env.Data, &r.Results)
	}
	return r
}

func decodeTemplateSendResult(body []byte) *TemplateSendResult {
	env := decodeEnvelope(body)
	r := &TemplateSendResult{APIReference: string(env.APIReference), Message: env.text(), Raw: body}
	var d templateSendData
	if decodeObject(env.Data, &d) {
		r.AcceptedCount, r.RejectedCount, r.Results = d.Accepted.Value(), d.Rejected.Value(), d.Results
	}
	return r
}

func decodeSessionSendResult(body []byte) *SessionSendResult {
	env := decodeEnvelope(body)
	r := &SessionSendResult{APIReference: string(env.APIReference), Message: env.text(), Raw: body}
	var d sessionSendData
	if decodeObject(env.Data, &d) {
		r.MessageID, r.To, r.UserReferenceID = string(d.MessageID), string(d.To), string(d.UserReferenceID)
		r.Status, r.Category = string(d.Status), string(d.Category)
	}
	return r
}

func decodeStatusResult(body []byte) *StatusResult {
	env := decodeEnvelope(body)
	r := &StatusResult{APIReference: string(env.APIReference), Message: env.text(), Raw: body}
	var d struct {
		Results json.RawMessage `json:"results"`
	}
	if decodeObject(env.Data, &d) && isJSONArray(d.Results) {
		_ = json.Unmarshal(d.Results, &r.Results)
	}
	return r
}

func decodeTemplateList(body []byte) *TemplateList {
	env := decodeEnvelope(body)
	r := &TemplateList{APIReference: string(env.APIReference), Message: env.text(), Raw: body}
	var d struct {
		Templates json.RawMessage `json:"templates"`
	}
	if decodeObject(env.Data, &d) && isJSONArray(d.Templates) {
		_ = json.Unmarshal(d.Templates, &r.Templates)
	}
	return r
}

func decodeBalance(body []byte) *Balance {
	var a struct {
		Message    json.RawMessage `json:"message"`
		Balance    Decimal         `json:"balance"`
		BundleType flexString      `json:"bundle_type"`
		ClientID   flexString      `json:"client_id"`
		ClientName flexString      `json:"client_name"`
	}
	_ = json.Unmarshal(body, &a)
	msg, _ := messageText(a.Message)
	return &Balance{Message: msg, Balance: a.Balance, BundleType: string(a.BundleType),
		ClientID: string(a.ClientID), ClientName: string(a.ClientName), Raw: body}
}

func decodeBalances(body []byte) *Balances {
	var a struct {
		Message    json.RawMessage            `json:"message"`
		Balances   map[string]json.RawMessage `json:"balances"`
		ClientID   flexString                 `json:"client_id"`
		ClientName flexString                 `json:"client_name"`
	}
	_ = json.Unmarshal(body, &a)
	msg, _ := messageText(a.Message)
	r := &Balances{Message: msg, ClientID: string(a.ClientID), ClientName: string(a.ClientName), Raw: body}
	if a.Balances != nil {
		r.Balances = make(map[string]Decimal, len(a.Balances))
		for k, v := range a.Balances {
			var d Decimal
			_ = d.UnmarshalJSON(v)
			r.Balances[k] = d
		}
	}
	return r
}

func decodeHealth(body []byte) *Health {
	var a struct {
		Status      flexString `json:"status"`
		Timestamp   flexString `json:"timestamp"`
		Version     flexString `json:"version"`
		Environment flexString `json:"environment"`
	}
	_ = json.Unmarshal(body, &a)
	return &Health{Status: string(a.Status), Timestamp: string(a.Timestamp), Version: string(a.Version),
		Environment: string(a.Environment), Raw: body}
}

// ---------------------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------------------

func filter[T any](items []T, keep func(*T) bool) []*T {
	var out []*T
	for i := range items {
		if keep(&items[i]) {
			out = append(out, &items[i])
		}
	}
	return out
}

func digitsOnly(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func findNumber[T any](items []T, number string, field func(*T) string) *T {
	want := digitsOnly(number)
	for i := range items {
		got := field(&items[i])
		if (want == "" && got == number) || (want != "" && digitsOnly(got) == want) {
			return &items[i]
		}
	}
	return nil
}

// trimJSSpace trims the characters JavaScript's String.prototype.trim removes, so ByAddress
// behaves like the reference implementation.
func trimJSSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
			return true
		}
		return r >= 0x2000 && r <= 0x200a
	})
}
