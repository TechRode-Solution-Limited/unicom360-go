package webhook

import "encoding/json"

// Known delivery report statuses (open sets).
const (
	StatusSent         = "SENT"
	StatusDelivered    = "DELIVERED"
	StatusFailed       = "FAILED"
	StatusUnknownError = "UNKNOWN_ERROR"
)

// SMSDeliveryReport is an SMS delivery report posted to a send's callback_url.
type SMSDeliveryReport struct {
	MessageID string `json:"message_id"`
	// Status is SENT, DELIVERED, FAILED or UNKNOWN_ERROR (open).
	Status       string          `json:"status"`
	MobileNumber string          `json:"mobile_number"`
	Raw          json.RawMessage `json:"-"`
}

// EmailDeliveryReport is an email delivery report posted to a send's callback_url.
type EmailDeliveryReport struct {
	// MessageID is the email's id as a string (the report sends a JSON number; it matches
	// the send result's EmailResult.MessageID).
	MessageID string `json:"message_id"`
	// Status is SENT, DELIVERED or FAILED (open).
	Status       string `json:"status"`
	EmailAddress string `json:"email_address"`
	// Message is "success", or the provider's reason on FAILED.
	Message string          `json:"message"`
	Raw     json.RawMessage `json:"-"`
}

// ParseSMSCallback parses an SMS delivery report from its raw body.
//
// These reports are NOT signed and are never retried: anyone who knows the callback URL can
// post one, and a lost report is not sent again. Treat a report as a hint, for example by
// checking that the message id is one you sent.
//
// A body that is not a JSON object is an *unicom360.Error of type unicom360_error.
func ParseSMSCallback(body []byte) (*SMSDeliveryReport, error) {
	var a struct {
		MessageID    flexString `json:"message_id"`
		Status       flexString `json:"status"`
		MobileNumber flexString `json:"mobile_number"`
	}
	if !isObject(body) || json.Unmarshal(body, &a) != nil {
		return nil, invalidPayload("the SMS callback body is not a JSON object")
	}
	return &SMSDeliveryReport{MessageID: string(a.MessageID), Status: string(a.Status),
		MobileNumber: string(a.MobileNumber), Raw: clone(body)}, nil
}

// ParseEmailCallback parses an email delivery report from its raw body. Like SMS reports,
// these are NOT signed and never retried; treat them as hints.
//
// A body that is not a JSON object is an *unicom360.Error of type unicom360_error.
func ParseEmailCallback(body []byte) (*EmailDeliveryReport, error) {
	var a struct {
		MessageID    flexString `json:"message_id"`
		Status       flexString `json:"status"`
		EmailAddress flexString `json:"email_address"`
		Message      flexString `json:"message"`
	}
	if !isObject(body) || json.Unmarshal(body, &a) != nil {
		return nil, invalidPayload("the email callback body is not a JSON object")
	}
	return &EmailDeliveryReport{MessageID: string(a.MessageID), Status: string(a.Status),
		EmailAddress: string(a.EmailAddress), Message: string(a.Message), Raw: clone(body)}, nil
}
