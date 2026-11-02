package unicom360

import "context"

// EmailService sends email. Use it through Client.Email.
type EmailService struct {
	client *Client
}

// EmailSendParams are the parameters of Email.Send. Set exactly one of To and ToOne; optional
// fields left "" are not sent.
type EmailSendParams struct {
	// From is an email sender registered and active on your account.
	From string
	// To lists the addresses (1 to 2,000), sent as a JSON array.
	To []string
	// ToOne is a single address, sent as a JSON string.
	ToOne string
	// Subject is 1 to 200 characters.
	Subject string
	// EmailBody is the body. HTML is detected and sent as HTML, anything else as plain text.
	EmailBody string
	// ReplyTo is the Reply-To address.
	ReplyTo string
	// UserReferenceID is your own reference. A reference already used for an address and
	// this sender is refused, but email has no unique index behind the check, so it does not
	// make a retry safe.
	UserReferenceID string
	// CallbackURL receives the unsigned email delivery reports (see
	// webhook.ParseEmailCallback).
	CallbackURL string
	// Source labels the sending system in reports.
	Source string
}

type emailBody struct {
	From            string `json:"from"`
	To              any    `json:"to,omitempty"`
	Subject         string `json:"subject"`
	EmailBody       string `json:"email_body"`
	ReplyTo         string `json:"reply_to,omitempty"`
	UserReferenceID string `json:"user_reference_id,omitempty"`
	CallbackURL     string `json:"callback_url,omitempty"`
	Source          string `json:"source,omitempty"`
}

// Send sends one email to one or more addresses.
//
// The result has one entry per address, not in request order past the tenth: find an
// address's entry with ByAddress. An address the gateway cannot use comes back with status
// "Invalid email address"; that is a result, not an error.
//
// Retries: email is retried only after a 429 or a failure to connect, never after a timeout or
// a 5xx, even with a UserReferenceID: the gateway's email de-duplication has no unique index,
// so an overlapping retry could send twice.
func (s *EmailService) Send(ctx context.Context, params EmailSendParams, opts ...RequestOption) (*EmailSendResult, error) {
	to, err := recipients(params.To, params.ToOne, "EmailSendParams")
	if err != nil {
		return nil, err
	}
	op := &operation{method: "POST", path: "/api/v1/json/bulk/email", auth: true}
	if s.client.core.cfg.autoRefs && params.UserReferenceID == "" {
		params.UserReferenceID = newReferenceID()
		op.generatedRefs = []string{params.UserReferenceID}
	}
	// Never retry-safe: see the method documentation.
	op.retrySafe = false
	op.body = emailBody{From: params.From, To: to, Subject: params.Subject, EmailBody: params.EmailBody,
		ReplyTo: params.ReplyTo, UserReferenceID: params.UserReferenceID, CallbackURL: params.CallbackURL,
		Source: params.Source}
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeEmailSendResult(body), nil
}
