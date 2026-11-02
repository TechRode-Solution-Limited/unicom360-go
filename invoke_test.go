package unicom360_test

// invoke maps a fixture's canonical operation and API-shaped params to the SDK's public
// methods. Params are decoded strictly: a parameter the harness does not know fails the test.

import (
	"context"
	"encoding/json"
	"fmt"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

func invoke(ctx context.Context, c *unicom360.Client, op string, params json.RawMessage) (any, error) {
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	switch op {
	case "sms.send":
		var p struct {
			From            string          `json:"from"`
			To              json.RawMessage `json:"to"`
			Message         string          `json:"message"`
			UserReferenceID string          `json:"user_reference_id"`
			CallbackURL     string          `json:"callback_url"`
			Source          string          `json:"source"`
		}
		if err := strictDecode(params, &p); err != nil {
			return nil, harnessError("sms.send params: %v", err)
		}
		sp := unicom360.SMSSendParams{From: p.From, Message: p.Message, UserReferenceID: p.UserReferenceID,
			CallbackURL: p.CallbackURL, Source: p.Source}
		if err := setRecipients(p.To, &sp.To, &sp.ToOne); err != nil {
			return nil, err
		}
		return c.SMS.Send(ctx, sp)

	case "email.send":
		var p struct {
			From            string          `json:"from"`
			To              json.RawMessage `json:"to"`
			Subject         string          `json:"subject"`
			EmailBody       string          `json:"email_body"`
			ReplyTo         string          `json:"reply_to"`
			UserReferenceID string          `json:"user_reference_id"`
			CallbackURL     string          `json:"callback_url"`
			Source          string          `json:"source"`
		}
		if err := strictDecode(params, &p); err != nil {
			return nil, harnessError("email.send params: %v", err)
		}
		ep := unicom360.EmailSendParams{From: p.From, Subject: p.Subject, EmailBody: p.EmailBody, ReplyTo: p.ReplyTo,
			UserReferenceID: p.UserReferenceID, CallbackURL: p.CallbackURL, Source: p.Source}
		if err := setRecipients(p.To, &ep.To, &ep.ToOne); err != nil {
			return nil, err
		}
		return c.Email.Send(ctx, ep)

	case "whatsapp.send_template":
		tp, err := templateParams(params)
		if err != nil {
			return nil, err
		}
		return c.WhatsApp.SendTemplate(ctx, tp)

	case "whatsapp.send_session":
		msg, err := sessionMessage(params)
		if err != nil {
			return nil, err
		}
		return c.WhatsApp.SendSession(ctx, msg)

	case "whatsapp.status":
		var p struct {
			MessageIDs []string `json:"message_ids"`
		}
		if err := strictDecode(params, &p); err != nil {
			return nil, harnessError("whatsapp.status params: %v", err)
		}
		return c.WhatsApp.Status(ctx, p.MessageIDs)

	case "whatsapp.templates":
		if err := strictDecode(params, &struct{}{}); err != nil {
			return nil, harnessError("whatsapp.templates params: %v", err)
		}
		return c.WhatsApp.Templates(ctx)

	case "balance.get":
		var p struct {
			BundleType string `json:"bundle_type"`
		}
		if err := strictDecode(params, &p); err != nil {
			return nil, harnessError("balance.get params: %v", err)
		}
		return c.Balance.Get(ctx, p.BundleType)

	case "balance.all":
		if err := strictDecode(params, &struct{}{}); err != nil {
			return nil, harnessError("balance.all params: %v", err)
		}
		return c.Balance.All(ctx)

	case "health":
		if err := strictDecode(params, &struct{}{}); err != nil {
			return nil, harnessError("health params: %v", err)
		}
		return c.Health(ctx)
	}
	return nil, harnessError("unknown operation %q", op)
}

// harnessErr marks a failure of the harness itself (a vector it does not understand), so a
// test never mistakes it for the SDK's error.
type harnessErr struct{ msg string }

func (e *harnessErr) Error() string { return "harness: " + e.msg }

func harnessError(format string, args ...any) error {
	return &harnessErr{msg: fmt.Sprintf(format, args...)}
}

// setRecipients maps "to" (a string or a list) to the To / ToOne pair.
func setRecipients(raw json.RawMessage, list *[]string, one *string) error {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		return json.Unmarshal(raw, one)
	}
	if err := strictDecode(raw, list); err != nil {
		return harnessError("to: %v", err)
	}
	return nil
}

func templateParams(params json.RawMessage) (unicom360.TemplateSendParams, error) {
	var p struct {
		Template    string `json:"template"`
		From        string `json:"from"`
		Language    string `json:"language"`
		CallbackURL string `json:"callback_url"`
		Recipients  []struct {
			To              string          `json:"to"`
			UserReferenceID string          `json:"user_reference_id"`
			Header          json.RawMessage `json:"header"`
			Body            json.RawMessage `json:"body"`
			Buttons         []struct {
				Index int    `json:"index"`
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"buttons"`
			OTP string `json:"otp"`
		} `json:"recipients"`
	}
	if err := strictDecode(params, &p); err != nil {
		return unicom360.TemplateSendParams{}, harnessError("whatsapp.send_template params: %v", err)
	}
	tp := unicom360.TemplateSendParams{Template: p.Template, From: p.From, Language: p.Language, CallbackURL: p.CallbackURL}
	for i, r := range p.Recipients {
		rec := unicom360.TemplateRecipient{To: r.To, UserReferenceID: r.UserReferenceID, OTP: r.OTP}
		if len(r.Header) > 0 {
			if r.Header[0] == '"' {
				var text string
				if err := json.Unmarshal(r.Header, &text); err != nil {
					return tp, err
				}
				rec.Header = unicom360.TemplateHeaderText(text)
			} else {
				var h struct {
					Type     string `json:"type"`
					URL      string `json:"url"`
					Filename string `json:"filename"`
				}
				if err := strictDecode(r.Header, &h); err != nil {
					return tp, harnessError("recipients[%d].header: %v", i, err)
				}
				switch h.Type {
				case "image":
					rec.Header = unicom360.TemplateHeaderImage(h.URL)
				case "video":
					rec.Header = unicom360.TemplateHeaderVideo(h.URL)
				case "document":
					rec.Header = unicom360.TemplateHeaderDocument(h.URL, h.Filename)
				default:
					return tp, harnessError("recipients[%d].header: unknown type %q", i, h.Type)
				}
				if h.Filename != "" && h.Type != "document" {
					return tp, harnessError("recipients[%d].header: filename on %s", i, h.Type)
				}
			}
		}
		if len(r.Body) > 0 {
			if r.Body[0] == '[' {
				var list []string
				if err := strictDecode(r.Body, &list); err != nil {
					return tp, harnessError("recipients[%d].body: %v", i, err)
				}
				rec.Body = unicom360.TemplateBodyOf(list...)
			} else {
				var named map[string]string
				if err := strictDecode(r.Body, &named); err != nil {
					return tp, harnessError("recipients[%d].body: %v", i, err)
				}
				rec.Body = unicom360.TemplateBodyNamed(named)
			}
		}
		if r.Buttons != nil {
			rec.Buttons = []unicom360.TemplateButton{}
			for _, b := range r.Buttons {
				switch b.Type {
				case "url":
					rec.Buttons = append(rec.Buttons, unicom360.TemplateButtonURL(b.Index, b.Value))
				case "quick_reply":
					rec.Buttons = append(rec.Buttons, unicom360.TemplateButtonQuickReply(b.Index, b.Value))
				default:
					return tp, harnessError("recipients[%d].buttons: unknown type %q", i, b.Type)
				}
			}
		}
		tp.Recipients = append(tp.Recipients, rec)
	}
	return tp, nil
}

// sessionMessage builds a session message with the factory for params.type, then sets the
// common fields.
func sessionMessage(params json.RawMessage) (unicom360.SessionMessage, error) {
	var p struct {
		To    string  `json:"to"`
		Type  string  `json:"type"`
		Text  *string `json:"text"`
		Media *struct {
			URL      string `json:"url"`
			Caption  string `json:"caption"`
			Filename string `json:"filename"`
		} `json:"media"`
		Interactive *struct {
			Type     string                  `json:"type"`
			Header   string                  `json:"header"`
			Body     string                  `json:"body"`
			Footer   string                  `json:"footer"`
			Button   string                  `json:"button"`
			Buttons  []unicom360.ReplyButton `json:"buttons"`
			Sections []unicom360.ListSection `json:"sections"`
		} `json:"interactive"`
		Location *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Address   string  `json:"address"`
		} `json:"location"`
		Contacts []unicom360.ContactCard `json:"contacts"`
		Reaction *struct {
			MessageID string `json:"message_id"`
			Emoji     string `json:"emoji"`
		} `json:"reaction"`
		From            string `json:"from"`
		ReplyTo         string `json:"reply_to"`
		UserReferenceID string `json:"user_reference_id"`
		CallbackURL     string `json:"callback_url"`
	}
	if err := strictDecode(params, &p); err != nil {
		return unicom360.SessionMessage{}, harnessError("whatsapp.send_session params: %v", err)
	}
	contentField := map[string]string{"text": "text", "image": "media", "video": "media", "document": "media",
		"audio": "media", "sticker": "media", "interactive": "interactive", "location": "location",
		"contacts": "contacts", "reaction": "reaction"}[p.Type]
	present := map[string]bool{"text": p.Text != nil, "media": p.Media != nil, "interactive": p.Interactive != nil,
		"location": p.Location != nil, "contacts": p.Contacts != nil, "reaction": p.Reaction != nil}
	for field, ok := range present {
		if ok && field != contentField {
			return unicom360.SessionMessage{}, harnessError("session %s message with a %s field", p.Type, field)
		}
	}
	var m unicom360.SessionMessage
	need := func(ok bool, field string) error {
		if !ok {
			return harnessError("session %s message without %s", p.Type, field)
		}
		return nil
	}
	var err error
	switch p.Type {
	case "text":
		if err = need(p.Text != nil, "text"); err == nil {
			m = unicom360.TextMessage(p.To, *p.Text)
		}
	case "image":
		if err = need(p.Media != nil && p.Media.Filename == "", "media without filename"); err == nil {
			m = unicom360.ImageMessage(p.To, p.Media.URL, p.Media.Caption)
		}
	case "video":
		if err = need(p.Media != nil && p.Media.Filename == "", "media without filename"); err == nil {
			m = unicom360.VideoMessage(p.To, p.Media.URL, p.Media.Caption)
		}
	case "document":
		if err = need(p.Media != nil, "media"); err == nil {
			m = unicom360.DocumentMessage(p.To, p.Media.URL, p.Media.Caption, p.Media.Filename)
		}
	case "audio":
		if err = need(p.Media != nil && p.Media.Caption == "" && p.Media.Filename == "", "plain media"); err == nil {
			m = unicom360.AudioMessage(p.To, p.Media.URL)
		}
	case "sticker":
		if err = need(p.Media != nil && p.Media.Caption == "" && p.Media.Filename == "", "plain media"); err == nil {
			m = unicom360.StickerMessage(p.To, p.Media.URL)
		}
	case "interactive":
		if err = need(p.Interactive != nil, "interactive"); err != nil {
			break
		}
		in := p.Interactive
		switch in.Type {
		case "button":
			m = unicom360.ButtonsMessage(p.To, in.Body, in.Buttons, in.Header, in.Footer)
		case "list":
			m = unicom360.ListMessage(p.To, in.Body, in.Button, in.Sections, in.Header, in.Footer)
		default:
			err = harnessError("unknown interactive type %q", in.Type)
		}
	case "location":
		if err = need(p.Location != nil, "location"); err == nil {
			l := p.Location
			m = unicom360.LocationMessage(p.To, l.Latitude, l.Longitude, l.Name, l.Address)
		}
	case "contacts":
		if err = need(p.Contacts != nil, "contacts"); err == nil {
			m = unicom360.ContactsMessage(p.To, p.Contacts)
		}
	case "reaction":
		if err = need(p.Reaction != nil && p.ReplyTo == "", "reaction (and no reply_to)"); err == nil {
			m = unicom360.ReactionMessage(p.To, p.Reaction.MessageID, p.Reaction.Emoji)
		}
	default:
		err = harnessError("unknown session type %q", p.Type)
	}
	if err != nil {
		return m, err
	}
	m.From, m.ReplyTo, m.UserReferenceID, m.CallbackURL = p.From, p.ReplyTo, p.UserReferenceID, p.CallbackURL
	return m, nil
}
