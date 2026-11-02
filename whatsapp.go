package unicom360

import (
	"context"
	"encoding/json"
)

// WhatsAppService sends WhatsApp templates and session messages and reads message status and
// the template list. Use it through Client.WhatsApp.
type WhatsAppService struct {
	client *Client
}

// ---------------------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------------------

// TemplateSendParams are the parameters of WhatsApp.SendTemplate. Optional fields left ""
// are not sent.
type TemplateSendParams struct {
	// Template is the name of an approved, active template.
	Template string
	// From is one of your approved WhatsApp senders. Optional with exactly one.
	From string
	// Language is the template's language. Optional when the name exists in one language.
	Language string
	// CallbackURL receives this send's signed status webhooks (an https URL).
	CallbackURL string
	// Recipients are the messages to send, 1 to 1,000.
	Recipients []TemplateRecipient
}

// TemplateRecipient is one template message: a number and that recipient's variables.
type TemplateRecipient struct {
	// To is the number in international format.
	To string
	// UserReferenceID is your own reference. When every recipient has one, the send is safe
	// to retry: the gateway refuses a reference it has already sent.
	UserReferenceID string
	// Header fills the template's header: TemplateHeaderText, TemplateHeaderImage,
	// TemplateHeaderVideo or TemplateHeaderDocument.
	Header *TemplateHeader
	// Body fills the body variables: TemplateBodyOf (in order) or TemplateBodyNamed.
	Body *TemplateBody
	// Buttons fill the dynamic buttons: TemplateButtonURL or TemplateButtonQuickReply.
	Buttons []TemplateButton
	// OTP is the code for an AUTHENTICATION template.
	OTP string
}

type templateRecipientBody struct {
	To              string          `json:"to"`
	UserReferenceID string          `json:"user_reference_id,omitempty"`
	Header          *TemplateHeader `json:"header,omitempty"`
	Body            *TemplateBody   `json:"body,omitempty"`
	Buttons         any             `json:"buttons,omitempty"`
	OTP             string          `json:"otp,omitempty"`
}

// MarshalJSON writes the recipient in the API's shape; unset optional fields are left out.
func (r TemplateRecipient) MarshalJSON() ([]byte, error) {
	b := templateRecipientBody{To: r.To, UserReferenceID: r.UserReferenceID, Header: r.Header, Body: r.Body, OTP: r.OTP}
	if r.Buttons != nil {
		b.Buttons = r.Buttons
	}
	return marshalJSON(b)
}

// TemplateHeader is the value of a template's header: text (sent as a JSON string) or media
// (sent as {"type", "url", "filename"?}). Create one with TemplateHeaderText,
// TemplateHeaderImage, TemplateHeaderVideo or TemplateHeaderDocument.
type TemplateHeader struct {
	text     string
	isText   bool
	kind     string
	url      string
	filename string
}

// TemplateHeaderText fills a text header's variable.
func TemplateHeaderText(value string) *TemplateHeader {
	return &TemplateHeader{text: value, isText: true}
}

// TemplateHeaderImage is an image header from an https URL Meta can fetch.
func TemplateHeaderImage(url string) *TemplateHeader {
	return &TemplateHeader{kind: "image", url: url}
}

// TemplateHeaderVideo is a video header from an https URL Meta can fetch.
func TemplateHeaderVideo(url string) *TemplateHeader {
	return &TemplateHeader{kind: "video", url: url}
}

// TemplateHeaderDocument is a document header; filename ("" for none) is the name it is
// shown with.
func TemplateHeaderDocument(url, filename string) *TemplateHeader {
	return &TemplateHeader{kind: "document", url: url, filename: filename}
}

// MarshalJSON writes the header in the API's shape.
func (h TemplateHeader) MarshalJSON() ([]byte, error) {
	if h.isText {
		return marshalJSON(h.text)
	}
	return marshalJSON(struct {
		Type     string `json:"type"`
		URL      string `json:"url"`
		Filename string `json:"filename,omitempty"`
	}{h.kind, h.url, h.filename})
}

// TemplateBody is the value of a template's body variables: a list in the template's order
// (TemplateBodyOf) or a map keyed by variable name (TemplateBodyNamed).
type TemplateBody struct {
	list  []string
	named map[string]string
}

// TemplateBodyOf gives the body variables in the template's order ({{1}}, {{2}}, ...).
func TemplateBodyOf(values ...string) *TemplateBody {
	if values == nil {
		values = []string{}
	}
	return &TemplateBody{list: values}
}

// TemplateBodyNamed gives the body variables by name, as the template list's
// body.variables names them.
func TemplateBodyNamed(values map[string]string) *TemplateBody {
	if values == nil {
		values = map[string]string{}
	}
	return &TemplateBody{named: values}
}

// MarshalJSON writes the variables as a JSON array or object.
func (b TemplateBody) MarshalJSON() ([]byte, error) {
	if b.named != nil {
		return marshalJSON(b.named)
	}
	if b.list == nil {
		return []byte("[]"), nil
	}
	return marshalJSON(b.list)
}

// TemplateButton fills one of a template's dynamic buttons. Create one with
// TemplateButtonURL or TemplateButtonQuickReply.
type TemplateButton struct {
	// Index is the button's position in the template, from 0.
	Index int `json:"index"`
	// Type is "url" or "quick_reply".
	Type string `json:"type"`
	// Value is the URL suffix ({{1}}) or the quick reply's payload.
	Value string `json:"value"`
}

// TemplateButtonURL fills the {{1}} of the url button at index.
func TemplateButtonURL(index int, value string) TemplateButton {
	return TemplateButton{Index: index, Type: "url", Value: value}
}

// TemplateButtonQuickReply sets the payload of the quick reply button at index.
func TemplateButtonQuickReply(index int, value string) TemplateButton {
	return TemplateButton{Index: index, Type: "quick_reply", Value: value}
}

type templateBody struct {
	Template    string `json:"template"`
	From        string `json:"from,omitempty"`
	Language    string `json:"language,omitempty"`
	CallbackURL string `json:"callback_url,omitempty"`
	Recipients  any    `json:"recipients,omitempty"`
}

// SendTemplate sends an approved template to up to 1,000 recipients.
//
// A recipient the gateway rejects (an invalid number, a reused reference, bad variables) is
// reported in the result with status REJECTED and a reason; the send still succeeds. When no
// recipient is valid, the call fails with a NoValidRecipientsError whose Results give each
// reason.
//
// Retries: a 429 and a failure to connect are always retried; a timeout, a 5xx or SEND_FAILED
// only when every recipient has a UserReferenceID (or the client uses WithAutoReferenceIDs,
// which gives each recipient without one its own).
func (s *WhatsAppService) SendTemplate(ctx context.Context, params TemplateSendParams, opts ...RequestOption) (*TemplateSendResult, error) {
	op := &operation{method: "POST", path: "/api/v1/json/whatsapp/template", auth: true}
	recipients := params.Recipients
	if s.client.core.cfg.autoRefs {
		// Copy before filling in references: the caller's slice is never modified.
		recipients = append([]TemplateRecipient(nil), params.Recipients...)
		for i := range recipients {
			if recipients[i].UserReferenceID == "" {
				recipients[i].UserReferenceID = newReferenceID()
				op.generatedRefs = append(op.generatedRefs, recipients[i].UserReferenceID)
			}
		}
	}
	op.retrySafe = len(recipients) > 0
	for _, r := range recipients {
		if !hasReference(r.UserReferenceID) {
			op.retrySafe = false
		}
	}
	b := templateBody{Template: params.Template, From: params.From, Language: params.Language, CallbackURL: params.CallbackURL}
	if recipients != nil {
		b.Recipients = recipients
	}
	op.body = b
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeTemplateSendResult(body), nil
}

// ---------------------------------------------------------------------------------------
// Session messages
// ---------------------------------------------------------------------------------------

// Session message types (SessionMessage.Type).
const (
	SessionText        = "text"
	SessionImage       = "image"
	SessionVideo       = "video"
	SessionDocument    = "document"
	SessionAudio       = "audio"
	SessionSticker     = "sticker"
	SessionInteractive = "interactive"
	SessionLocation    = "location"
	SessionContacts    = "contacts"
	SessionReaction    = "reaction"
)

// SessionMessage is a WhatsApp session message: a free-form message inside the 24-hour window
// a customer opens by writing to you. Create one with a factory (TextMessage, ImageMessage,
// ButtonsMessage, ...) and set the optional common fields on the value it returns:
//
//	msg := unicom360.TextMessage("260971234567", "Your order is ready.")
//	msg.UserReferenceID = "order-4821"
//
// The content field matching Type is sent; optional fields left "" are not.
type SessionMessage struct {
	// To is the customer's number.
	To string
	// Type is one of the Session* constants.
	Type string

	// Text is the text of a text message.
	Text string
	// Media is the content of an image, video, document, audio or sticker message.
	Media *SessionMedia
	// Interactive is the content of a buttons or list message.
	Interactive *Interactive
	// Location is the content of a location message.
	Location *Location
	// Contacts is the content of a contacts message.
	Contacts []ContactCard
	// Reaction is the content of a reaction message.
	Reaction *Reaction

	// From is one of your approved WhatsApp senders. Optional with exactly one.
	From string
	// ReplyTo quotes a message: a message_id a send returned, or the wamid. id of an inbound
	// message (as the message.received webhook gives it). Not allowed on a reaction.
	ReplyTo string
	// UserReferenceID is your own reference; with one, the send is safe to retry.
	UserReferenceID string
	// CallbackURL receives this message's signed status webhooks (an https URL).
	CallbackURL string
}

// SessionMedia is a media message's content. Caption applies to image, video and document;
// Filename to document.
type SessionMedia struct {
	URL      string `json:"url"`
	Caption  string `json:"caption,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// Interactive is a buttons message (Type "button") or a list message (Type "list").
type Interactive struct {
	// Type is "button" or "list".
	Type   string `json:"type"`
	Header string `json:"header,omitempty"`
	Body   string `json:"body"`
	Footer string `json:"footer,omitempty"`
	// Button is the text of the button that opens a list.
	Button string `json:"button,omitempty"`
	// Buttons are a buttons message's reply buttons (1 to 3).
	Buttons []ReplyButton `json:"buttons,omitempty"`
	// Sections are a list message's sections.
	Sections []ListSection `json:"sections,omitempty"`
}

// ReplyButton is a reply button: an id you choose and the title the customer sees.
type ReplyButton struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ListSection is a section of a list message. Title is required when there are several.
type ListSection struct {
	Title string    `json:"title,omitempty"`
	Rows  []ListRow `json:"rows"`
}

// ListRow is a row of a list message.
type ListRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// Location is a location message's content. Latitude and longitude are always sent, zero
// included.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Name      string  `json:"name,omitempty"`
	Address   string  `json:"address,omitempty"`
}

// Reaction is a reaction message's content. An empty Emoji removes your reaction.
type Reaction struct {
	// MessageID is the message reacted to: a message_id a send returned, or the wamid. id of
	// an inbound message.
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

// ContactCard is Meta's contact object, sent in a contacts message. Name.FormattedName and at
// least one phone are required.
type ContactCard struct {
	Name      ContactName      `json:"name"`
	Phones    []ContactPhone   `json:"phones,omitempty"`
	Emails    []ContactEmail   `json:"emails,omitempty"`
	URLs      []ContactURL     `json:"urls,omitempty"`
	Addresses []ContactAddress `json:"addresses,omitempty"`
	Org       *ContactOrg      `json:"org,omitempty"`
	Birthday  string           `json:"birthday,omitempty"`
}

// ContactName is a contact's name.
type ContactName struct {
	FormattedName string `json:"formatted_name"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
	MiddleName    string `json:"middle_name,omitempty"`
	Suffix        string `json:"suffix,omitempty"`
	Prefix        string `json:"prefix,omitempty"`
}

// ContactPhone is one of a contact's phone numbers.
type ContactPhone struct {
	Phone string `json:"phone"`
	Type  string `json:"type,omitempty"`
	WaID  string `json:"wa_id,omitempty"`
}

// ContactEmail is one of a contact's email addresses.
type ContactEmail struct {
	Email string `json:"email,omitempty"`
	Type  string `json:"type,omitempty"`
}

// ContactURL is one of a contact's web addresses.
type ContactURL struct {
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

// ContactAddress is one of a contact's postal addresses.
type ContactAddress struct {
	Street      string `json:"street,omitempty"`
	City        string `json:"city,omitempty"`
	State       string `json:"state,omitempty"`
	Zip         string `json:"zip,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	Type        string `json:"type,omitempty"`
}

// ContactOrg is a contact's organisation.
type ContactOrg struct {
	Company    string `json:"company,omitempty"`
	Department string `json:"department,omitempty"`
	Title      string `json:"title,omitempty"`
}

// TextMessage is a text message (1 to 4,096 characters).
func TextMessage(to, text string) SessionMessage {
	return SessionMessage{To: to, Type: SessionText, Text: text}
}

// ImageMessage is an image from an https URL; caption ("" for none) is shown under it.
func ImageMessage(to, url, caption string) SessionMessage {
	return SessionMessage{To: to, Type: SessionImage, Media: &SessionMedia{URL: url, Caption: caption}}
}

// VideoMessage is a video from an https URL; caption ("" for none) is shown under it.
func VideoMessage(to, url, caption string) SessionMessage {
	return SessionMessage{To: to, Type: SessionVideo, Media: &SessionMedia{URL: url, Caption: caption}}
}

// DocumentMessage is a document from an https URL, with an optional caption and the filename
// it is shown with ("" for either leaves it out).
func DocumentMessage(to, url, caption, filename string) SessionMessage {
	return SessionMessage{To: to, Type: SessionDocument, Media: &SessionMedia{URL: url, Caption: caption, Filename: filename}}
}

// AudioMessage is an audio file from an https URL.
func AudioMessage(to, url string) SessionMessage {
	return SessionMessage{To: to, Type: SessionAudio, Media: &SessionMedia{URL: url}}
}

// StickerMessage is a sticker (a WebP image) from an https URL.
func StickerMessage(to, url string) SessionMessage {
	return SessionMessage{To: to, Type: SessionSticker, Media: &SessionMedia{URL: url}}
}

// ButtonsMessage is a message with up to three reply buttons. header and footer ("" for none)
// are optional. The customer's tap arrives as a message.received webhook with the button's
// title in its text.
func ButtonsMessage(to, body string, buttons []ReplyButton, header, footer string) SessionMessage {
	return SessionMessage{To: to, Type: SessionInteractive, Interactive: &Interactive{
		Type: "button", Body: body, Buttons: buttons, Header: header, Footer: footer,
	}}
}

// ListMessage is a message whose button opens a list of up to ten rows in sections. header and
// footer ("" for none) are optional.
func ListMessage(to, body, button string, sections []ListSection, header, footer string) SessionMessage {
	return SessionMessage{To: to, Type: SessionInteractive, Interactive: &Interactive{
		Type: "list", Body: body, Button: button, Sections: sections, Header: header, Footer: footer,
	}}
}

// LocationMessage is a map pin. name and address ("" for none) are optional.
func LocationMessage(to string, latitude, longitude float64, name, address string) SessionMessage {
	return SessionMessage{To: to, Type: SessionLocation, Location: &Location{
		Latitude: latitude, Longitude: longitude, Name: name, Address: address,
	}}
}

// ContactsMessage shares one or more contact cards.
func ContactsMessage(to string, contacts []ContactCard) SessionMessage {
	return SessionMessage{To: to, Type: SessionContacts, Contacts: contacts}
}

// ReactionMessage reacts to a message with an emoji; an empty emoji removes the reaction.
// messageID is a message_id a send returned or the wamid. id of an inbound message. A reaction
// cannot carry ReplyTo.
func ReactionMessage(to, messageID, emoji string) SessionMessage {
	return SessionMessage{To: to, Type: SessionReaction, Reaction: &Reaction{MessageID: messageID, Emoji: emoji}}
}

type sessionBody struct {
	To              string        `json:"to"`
	Type            string        `json:"type"`
	Text            *string       `json:"text,omitempty"`
	Media           *SessionMedia `json:"media,omitempty"`
	Interactive     *Interactive  `json:"interactive,omitempty"`
	Location        *Location     `json:"location,omitempty"`
	Contacts        any           `json:"contacts,omitempty"`
	Reaction        *Reaction     `json:"reaction,omitempty"`
	From            string        `json:"from,omitempty"`
	ReplyTo         string        `json:"reply_to,omitempty"`
	UserReferenceID string        `json:"user_reference_id,omitempty"`
	CallbackURL     string        `json:"callback_url,omitempty"`
}

// MarshalJSON writes the message in the API's shape: the common fields and the content field
// named after Type.
func (m SessionMessage) MarshalJSON() ([]byte, error) {
	b := sessionBody{
		To: m.To, Type: m.Type, Media: m.Media, Interactive: m.Interactive, Location: m.Location,
		Reaction: m.Reaction, From: m.From, ReplyTo: m.ReplyTo, UserReferenceID: m.UserReferenceID,
		CallbackURL: m.CallbackURL,
	}
	if m.Type == SessionText || m.Text != "" {
		text := m.Text
		b.Text = &text
	}
	if m.Contacts != nil {
		b.Contacts = m.Contacts
	}
	return marshalJSON(b)
}

// SendSession sends one session message. The customer must have written to the sender in the
// last 24 hours, else the call fails with a SessionWindowClosedError.
//
// Retries: a 429 and a failure to connect are always retried; a timeout, a 5xx or SEND_FAILED
// only when the message has a UserReferenceID (or the client uses WithAutoReferenceIDs). A
// DuplicateReferenceError carries the message the reference first went out as in
// OriginalMessageID.
func (s *WhatsAppService) SendSession(ctx context.Context, msg SessionMessage, opts ...RequestOption) (*SessionSendResult, error) {
	op := &operation{method: "POST", path: "/api/v1/json/whatsapp/session", auth: true}
	if s.client.core.cfg.autoRefs && msg.UserReferenceID == "" {
		msg.UserReferenceID = newReferenceID()
		op.generatedRefs = []string{msg.UserReferenceID}
	}
	op.retrySafe = hasReference(msg.UserReferenceID)
	op.body = msg
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeSessionSendResult(body), nil
}

// ---------------------------------------------------------------------------------------
// Status and template list
// ---------------------------------------------------------------------------------------

// Status returns the status of 1 to 100 WhatsApp messages by the message_id a send returned.
// The ids are sent as given; the gateway answers each distinct id once, with NOT_FOUND for an
// id it does not know.
func (s *WhatsAppService) Status(ctx context.Context, messageIDs []string, opts ...RequestOption) (*StatusResult, error) {
	if messageIDs == nil {
		messageIDs = []string{}
	}
	op := &operation{method: "POST", path: "/api/v1/json/whatsapp/status", auth: true, retrySafe: true,
		body: struct {
			MessageIDs []string `json:"message_ids"`
		}{messageIDs}}
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeStatusResult(body), nil
}

// Templates returns your approved WhatsApp templates, with their variables and buttons.
func (s *WhatsAppService) Templates(ctx context.Context, opts ...RequestOption) (*TemplateList, error) {
	op := &operation{method: "GET", path: "/api/v1/json/whatsapp/templates", auth: true, retrySafe: true}
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeTemplateList(body), nil
}

// ---------------------------------------------------------------------------------------
// Balance and health
// ---------------------------------------------------------------------------------------

// BalanceService reads bundle balances. Use it through Client.Balance.
type BalanceService struct {
	client *Client
}

// Get returns the units remaining on one bundle: bundleType is BundleSMS (the gateway's
// default), BundleSMSInternational, BundleEmail or BundleWhatsApp; "" sends none.
func (s *BalanceService) Get(ctx context.Context, bundleType string, opts ...RequestOption) (*Balance, error) {
	req := map[string]string{}
	if bundleType != "" {
		req["bundle_type"] = bundleType
	}
	op := &operation{method: "POST", path: "/api/v1/json/balance", auth: true, balance: true, retrySafe: true, body: req}
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeBalance(body), nil
}

// All returns the units remaining on every bundle.
func (s *BalanceService) All(ctx context.Context, opts ...RequestOption) (*Balances, error) {
	op := &operation{method: "POST", path: "/api/v1/json/balance/all", auth: true, balance: true, retrySafe: true,
		body: json.RawMessage("{}")}
	body, err := s.client.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeBalances(body), nil
}

// Health reports whether the gateway is up. It sends no API key.
func (c *Client) Health(ctx context.Context, opts ...RequestOption) (*Health, error) {
	op := &operation{method: "GET", path: "/health", health: true, retrySafe: true}
	body, err := c.core.do(ctx, op, opts)
	if err != nil {
		return nil, err
	}
	return decodeHealth(body), nil
}
