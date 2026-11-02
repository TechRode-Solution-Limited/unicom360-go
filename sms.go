package unicom360

import "context"

// SMSService sends SMS messages. Use it through Client.SMS.
type SMSService struct {
	client *Client
}

// SMSSendParams are the parameters of SMS.Send. Set exactly one of To (a list) and ToOne (a
// single number); what you set is what is sent. Optional fields left "" are not sent.
//
// The SDK does not check the message: use the gsm package to count characters and billing
// units, or to find characters the gateway refuses.
type SMSSendParams struct {
	// From is a sender ID registered and active for SMS on your account.
	From string
	// To lists the recipients in international format (1 to 2,000), sent as a JSON array.
	To []string
	// ToOne is a single recipient, sent as a JSON string.
	ToOne string
	// Message is the text: GSM-7 only, at most 1,200 characters.
	Message string
	// UserReferenceID is your own reference. The gateway refuses a reference already used
	// for a recipient and this sender, so a send that carries one is safe to retry.
	UserReferenceID string
	// CallbackURL receives the unsigned SMS delivery reports (see webhook.ParseSMSCallback).
	CallbackURL string
	// Source labels the sending system in reports.
	Source string
}

type smsBody struct {
	From            string `json:"from"`
	To              any    `json:"to,omitempty"`
	Message         string `json:"message"`
	UserReferenceID string `json:"user_reference_id,omitempty"`
	CallbackURL     string `json:"callback_url,omitempty"`
	Source          string `json:"source,omitempty"`
}

// Send sends one SMS text to one or more recipients.
//
// The result has one entry per recipient, not in request order past the tenth: find a
// recipient's entry with ByNumber. A number the gateway cannot use comes back with status
// "Invalid mobile number"; that is a result, not an error.
//
// Errors: an *Error, for example an InsufficientBundleError (errors.Is(err,
// ErrInsufficientBundle)), INVALID_SENDER_ID, a DuplicateReferenceError for a reused
// UserReferenceID, or an InvalidRequestError (HTTP 400) for a malformed request.
//
// Retries: a 429 and a failure to connect are always retried; a timeout, a 5xx or SEND_FAILED
// only when UserReferenceID is set (or the client uses WithAutoReferenceIDs).
func (s *SMSService) Send(ctx context.Context, params SMSSendParams, opts ...RequestOption) (*SMSSendResult, error) {
	to, err := recipients(params.To, params.ToOne, "SMSSendParams")
	if err != nil {
		return nil, err
	}
	op := &operation{method: "POST", path: "/api/v1/json/bulk/sms", auth: true}
	if s.client.core.cfg.autoRefs && params.UserReferenceID == "" {
		params.UserReferenceID = newReferenceID()
		op.generatedRefs = []string{params.UserReferenceID}
	}
	op.retrySafe = hasReference(params.UserReferenceID)
	op.body = smsBody{From: params.From, To: to, Message: params.Message,
		UserReferenceID: params.UserReferenceID, CallbackURL: params.CallbackURL, Source: params.Source}
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeSMSSendResult(body), nil
}

// recipients returns the value to send as "to": the list, the single string, or nil (not
// sent) when neither is set.
func recipients(list []string, one, params string) (any, error) {
	switch {
	case list != nil && one != "":
		return nil, configError(params + ": set To or ToOne, not both")
	case one != "":
		return one, nil
	case list != nil:
		return list, nil
	}
	return nil, nil
}
