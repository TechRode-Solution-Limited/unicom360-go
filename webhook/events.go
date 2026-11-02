package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

// Event types (the body's "event").
const (
	EventMessageStatus   = "message.status"
	EventMessageReceived = "message.received"
	EventTest            = "webhook.test"
)

// Event is a verified webhook event: *MessageStatusEvent, *MessageReceivedEvent, *TestEvent
// or, for an event type this SDK does not know, *UnknownEvent. Use a type switch:
//
//	switch e := ev.(type) {
//	case *webhook.MessageStatusEvent:
//		log.Printf("%s is %s", e.Data.MessageID, e.Data.Status)
//	case *webhook.MessageReceivedEvent:
//		log.Printf("%s wrote %q", e.Data.From, e.Data.Text)
//	}
type Event interface {
	// Meta returns the fields every event has.
	Meta() *EventMeta
}

// EventMeta holds the fields every event has.
type EventMeta struct {
	// Event is the body's event type, for example "message.status".
	Event string
	// DeliveryID is the X-Unicom360-Delivery header ("" when absent). A retried delivery
	// keeps its id: use it to drop repeats.
	DeliveryID string
	// CreatedAt is when the event happened, ISO 8601 as received.
	CreatedAt string
	// Raw is the whole body.
	Raw json.RawMessage
}

// Meta implements Event.
func (m *EventMeta) Meta() *EventMeta { return m }

// MessageStatusEvent reports a WhatsApp message's progress (message.status).
type MessageStatusEvent struct {
	EventMeta
	Data MessageStatusData
}

// MessageReceivedEvent is a customer's inbound WhatsApp message (message.received). It opens
// (or extends) the 24-hour session window until Data.WindowExpiresAt.
type MessageReceivedEvent struct {
	EventMeta
	Data InboundMessage
}

// TestEvent is the portal's "Send test" delivery (webhook.test).
type TestEvent struct {
	EventMeta
	Data TestEventData
}

// UnknownEvent is an event type this SDK version does not know. Data is the raw "data" value
// (nil when absent or null).
type UnknownEvent struct {
	EventMeta
	Data json.RawMessage
}

// MessageStatusData is a message.status event's data.
type MessageStatusData struct {
	// MessageID is the message_id the send returned.
	MessageID       string `json:"message_id"`
	UserReferenceID string `json:"user_reference_id"`
	// Channel is "whatsapp".
	Channel string `json:"channel"`
	To      string `json:"to"`
	From    string `json:"from"`
	// Status is sent, delivered, read or failed (open).
	Status string `json:"status"`
	// Timestamp is when the status changed, ISO 8601 as received.
	Timestamp string                  `json:"timestamp"`
	Error     *unicom360.MessageError `json:"error"`
	Category  string                  `json:"category"`
	Billable  *bool                   `json:"billable"`
	Raw       json.RawMessage         `json:"-"`
}

// InboundMessage is a message.received event's data.
//
// A media caption arrives in Text, not in Media. Payload is set only for location, contacts
// ({"contacts": [...]}), reaction, sticker and unsupported messages. A tap on a reply button or
// list row has the chosen title in Text, Payload nil, and ReplyTo set to the wamid. of your
// interactive message; the button or row id is not passed on.
type InboundMessage struct {
	// MessageID is Meta's id of the inbound message ("wamid." and Meta's id). Pass it as
	// ReplyTo or as a reaction's message id.
	MessageID string `json:"message_id"`
	Channel   string `json:"channel"`
	// From is the customer's number.
	From string `json:"from"`
	// To is your business number.
	To string `json:"to"`
	// Type is text, image, document, audio, video, voice, sticker, interactive, button,
	// location, contacts, reaction or unsupported (open).
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Media   *InboundMedia   `json:"media"`
	Payload json.RawMessage `json:"payload"`
	// ReplyTo is the wamid. of the message the customer replied to, or "".
	ReplyTo         string          `json:"reply_to"`
	Timestamp       string          `json:"timestamp"`
	WindowExpiresAt string          `json:"window_expires_at"`
	Raw             json.RawMessage `json:"-"`
}

// InboundMedia identifies an inbound message's media on Meta.
type InboundMedia struct {
	ID       string          `json:"id"`
	MIMEType string          `json:"mime_type"`
	Filename string          `json:"filename"`
	Raw      json.RawMessage `json:"-"`
}

// TestEventData is a webhook.test event's data.
type TestEventData struct {
	Message string          `json:"message"`
	Raw     json.RawMessage `json:"-"`
}

// ConstructEvent verifies a delivery (see Verify) and parses it into an Event. An empty or
// whitespace-only secret is a configuration_error; a verification failure is a
// signature_verification_error; a body that is not a JSON object with a string "event" is an
// *unicom360.Error of type unicom360_error. An unknown event type is not an error: it parses as
// *UnknownEvent.
func ConstructEvent(body []byte, headers http.Header, secret string, opts ...Option) (Event, error) {
	if err := Verify(body, headers, secret, opts...); err != nil {
		return nil, err
	}
	return parseEvent(body, header(headers, HeaderDelivery))
}

func parseEvent(body []byte, deliveryID string) (Event, error) {
	var env struct {
		Event     json.RawMessage `json:"event"`
		CreatedAt json.RawMessage `json:"created_at"`
		Data      json.RawMessage `json:"data"`
	}
	if !isObject(body) || json.Unmarshal(body, &env) != nil {
		return nil, invalidPayload("the webhook body is not a JSON object")
	}
	var name string
	if firstByte(env.Event) != '"' || json.Unmarshal(env.Event, &name) != nil {
		return nil, invalidPayload("the webhook body has no string \"event\"")
	}
	meta := EventMeta{Event: name, DeliveryID: deliveryID, CreatedAt: jsonString(env.CreatedAt), Raw: clone(body)}
	data := env.Data
	if !isObject(data) {
		data = nil
	}
	switch name {
	case EventMessageStatus:
		ev := &MessageStatusEvent{EventMeta: meta}
		if data != nil {
			_ = json.Unmarshal(data, &ev.Data)
		}
		return ev, nil
	case EventMessageReceived:
		ev := &MessageReceivedEvent{EventMeta: meta}
		if data != nil {
			_ = json.Unmarshal(data, &ev.Data)
		}
		return ev, nil
	case EventTest:
		ev := &TestEvent{EventMeta: meta}
		if data != nil {
			_ = json.Unmarshal(data, &ev.Data)
		}
		return ev, nil
	}
	return &UnknownEvent{EventMeta: meta, Data: rawOrNil(env.Data)}, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *MessageStatusData) UnmarshalJSON(b []byte) error {
	var a struct {
		MessageID       flexString              `json:"message_id"`
		UserReferenceID flexString              `json:"user_reference_id"`
		Channel         flexString              `json:"channel"`
		To              flexString              `json:"to"`
		From            flexString              `json:"from"`
		Status          flexString              `json:"status"`
		Timestamp       flexString              `json:"timestamp"`
		Error           *unicom360.MessageError `json:"error"`
		Category        flexString              `json:"category"`
		Billable        json.RawMessage         `json:"billable"`
	}
	*d = MessageStatusData{Raw: clone(b)}
	if !isObject(b) || json.Unmarshal(b, &a) != nil {
		return nil
	}
	d.MessageID, d.UserReferenceID, d.Channel = string(a.MessageID), string(a.UserReferenceID), string(a.Channel)
	d.To, d.From, d.Status, d.Timestamp = string(a.To), string(a.From), string(a.Status), string(a.Timestamp)
	if a.Error != nil && isObject(a.Error.Raw) {
		d.Error = a.Error
	}
	d.Category = string(a.Category)
	d.Billable = jsonBool(a.Billable)
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *InboundMessage) UnmarshalJSON(b []byte) error {
	var a struct {
		MessageID       flexString      `json:"message_id"`
		Channel         flexString      `json:"channel"`
		From            flexString      `json:"from"`
		To              flexString      `json:"to"`
		Type            flexString      `json:"type"`
		Text            flexString      `json:"text"`
		Media           *InboundMedia   `json:"media"`
		Payload         json.RawMessage `json:"payload"`
		ReplyTo         flexString      `json:"reply_to"`
		Timestamp       flexString      `json:"timestamp"`
		WindowExpiresAt flexString      `json:"window_expires_at"`
	}
	*m = InboundMessage{Raw: clone(b)}
	if !isObject(b) || json.Unmarshal(b, &a) != nil {
		return nil
	}
	m.MessageID, m.Channel, m.From, m.To = string(a.MessageID), string(a.Channel), string(a.From), string(a.To)
	m.Type, m.Text, m.ReplyTo = string(a.Type), string(a.Text), string(a.ReplyTo)
	m.Timestamp, m.WindowExpiresAt = string(a.Timestamp), string(a.WindowExpiresAt)
	if a.Media != nil && isObject(a.Media.Raw) {
		m.Media = a.Media
	}
	if isObject(a.Payload) {
		m.Payload = clone(bytes.TrimSpace(a.Payload))
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *InboundMedia) UnmarshalJSON(b []byte) error {
	var a struct {
		ID       flexString `json:"id"`
		MIMEType flexString `json:"mime_type"`
		Filename flexString `json:"filename"`
	}
	*m = InboundMedia{Raw: rawOrNil(b)}
	if isObject(b) && json.Unmarshal(b, &a) == nil {
		m.ID, m.MIMEType, m.Filename = string(a.ID), string(a.MIMEType), string(a.Filename)
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *TestEventData) UnmarshalJSON(b []byte) error {
	var a struct {
		Message flexString `json:"message"`
	}
	*d = TestEventData{Raw: clone(b)}
	if isObject(b) && json.Unmarshal(b, &a) == nil {
		d.Message = string(a.Message)
	}
	return nil
}

func invalidPayload(msg string) error {
	return &unicom360.Error{Type: unicom360.TypeUnicom360Error, Message: msg}
}
