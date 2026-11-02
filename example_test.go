package unicom360_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
)

func ExampleNewClient() {
	// With no options the key comes from UNICOM360_API_KEY. Pass it explicitly with
	// WithAPIKey, and tune retries, timeouts and logging with the other options.
	client, err := unicom360.NewClient(
		unicom360.WithAPIKey(os.Getenv("UNICOM360_API_KEY")),
		unicom360.WithMaxRetries(3),
		unicom360.WithTimeout(30*time.Second),
		unicom360.WithAppInfo(unicom360.AppInfo{Name: "shopsync", Version: "2.3.0"}),
		unicom360.WithLogger(slog.Default()),
	)
	if err != nil {
		log.Fatal(err) // a configuration problem, such as a missing key
	}
	_ = client // share one client: it is safe for concurrent use
}

func ExampleSMSService_Send() {
	client, err := unicom360.NewClient() // reads UNICOM360_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.SMS.Send(context.Background(), unicom360.SMSSendParams{
		From:    "Unicom360",
		To:      []string{"260971234567", "260961234567"},
		Message: "Your order ZM4821 is ready for collection.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("accepted:", len(res.Accepted()), "invalid:", len(res.Invalid()))
	// Results are not in request order past the tenth recipient: look them up by number.
	if r := res.ByNumber("+260 96 123 4567"); r != nil {
		fmt.Println(r.MobileNumber, r.Status, r.MessageID)
	}
	// Output:
	// accepted: 2 invalid: 0
	// 260961234567 ACKNOWLEDGED dff31690-b450-4001-a6b7-141d8523ef10
}

func ExampleEmailService_Send() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.Email.Send(context.Background(), unicom360.EmailSendParams{
		From:      "noreply@yourdomain.com",
		ToOne:     "jane@example.com",
		Subject:   "Your order is confirmed",
		EmailBody: "<p>Thanks for your order. Order ZM4821 is confirmed.</p>",
		ReplyTo:   "support@yourdomain.com",
	})
	if err != nil {
		log.Fatal(err)
	}
	r := res.ByAddress("Jane@Example.com")
	fmt.Println(r.EmailAddress, r.Status, r.MessageID)
	// Output: jane@example.com ACKNOWLEDGED 48213
}

func ExampleWhatsAppService_SendTemplate() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.WhatsApp.SendTemplate(context.Background(), unicom360.TemplateSendParams{
		Template: "order_ready",
		Language: "en_US",
		Recipients: []unicom360.TemplateRecipient{
			{
				To:              "260971234567",
				UserReferenceID: "inv-42", // with a reference on every recipient, retries are safe
				Header:          unicom360.TemplateHeaderImage("https://example.com/receipts/ZM-4821.png"),
				Body:            unicom360.TemplateBodyOf("Ada", "ZM-4821"),
				Buttons:         []unicom360.TemplateButton{unicom360.TemplateButtonURL(0, "ZM-4821")},
			},
			{To: "12", UserReferenceID: "inv-43", Body: unicom360.TemplateBodyOf("Joseph", "ZM-4822")},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("accepted:", res.AcceptedCount, "rejected:", res.RejectedCount)
	for _, r := range res.Rejected() {
		fmt.Println("rejected", r.To, r.Reason)
	}
	// Output:
	// accepted: 1 rejected: 1
	// rejected 12 INVALID_NUMBER
}

func ExampleWhatsAppService_SendSession() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	msg := unicom360.ButtonsMessage("260971234567", "Collect today or deliver?",
		[]unicom360.ReplyButton{{ID: "collect", Title: "Collect"}, {ID: "deliver", Title: "Deliver"}},
		"Order ZM-4821", "")
	msg.UserReferenceID = "btn-1"
	res, err := client.WhatsApp.SendSession(context.Background(), msg)
	if errors.Is(err, unicom360.ErrSessionWindowClosed) {
		fmt.Println("the customer has not written in 24 hours: send a template instead")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Status, res.UserReferenceID)
	// Output: ACKNOWLEDGED btn-1
}

func ExampleTextMessage() {
	// Every session factory returns a SessionMessage; set the common fields on it.
	msg := unicom360.TextMessage("260971234567", "Hi Ada, your order is ready.")
	msg.From = "260960123456"
	msg.ReplyTo = "wamid.HBgMMjYwOTcxMjM0NTY3FQIAEhggQTVCMEM3Qjc5OUZCOEU5NEM3REMxMjI5NzJBODJEQTVEAA=="

	// The other factories:
	_ = unicom360.ImageMessage("260971234567", "https://example.com/receipt.png", "Your receipt")
	_ = unicom360.DocumentMessage("260971234567", "https://example.com/invoice.pdf", "Invoice", "invoice-4821.pdf")
	_ = unicom360.LocationMessage("260971234567", -15.4167, 28.2833, "Cairo Road branch", "Cairo Road, Lusaka")
	_ = unicom360.ReactionMessage("260971234567", "wamid.HBgM...", "👍")
	_ = unicom360.ListMessage("260971234567", "Choose a delivery slot", "Pick a slot",
		[]unicom360.ListSection{{Title: "Today", Rows: []unicom360.ListRow{{ID: "am", Title: "Morning"}}}}, "", "")
	_ = unicom360.ContactsMessage("260971234567", []unicom360.ContactCard{{
		Name:   unicom360.ContactName{FormattedName: "Unicom360 Support"},
		Phones: []unicom360.ContactPhone{{Phone: "+260960123456", Type: "WORK"}},
	}})

	fmt.Println(msg.Type, msg.To)
	// Output: text 260971234567
}

func ExampleWhatsAppService_Status() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.WhatsApp.Status(context.Background(), []string{
		"7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f",
		"00000000-0000-4000-8000-000000000000",
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range res.Results {
		fmt.Println(s.MessageID, s.Status)
	}
	// Output:
	// 7c1f3a2e-5b64-4d2f-9a8e-0f1b2c3d4e5f READ
	// 00000000-0000-4000-8000-000000000000 NOT_FOUND
}

func ExampleWhatsAppService_Templates() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	list, err := client.WhatsApp.Templates(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	t := list.ByName("order_ready", "en_US")
	fmt.Println(t.Name, t.Category, t.Body.Variables)
	// Output: order_ready UTILITY [1 2]
}

func ExampleBalanceService_Get() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	b, err := client.Balance.Get(context.Background(), unicom360.BundleWhatsApp)
	if err != nil {
		log.Fatal(err)
	}
	units, _ := b.Balance.Float64() // b.Balance.String() keeps the exact text
	fmt.Println(b.BundleType, b.Balance, units)
	// Output: WHATSAPP 37.5 37.5
}

func ExampleBalanceService_All() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	all, err := client.Balance.All(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("SMS:", all.Balances[unicom360.BundleSMS], "email:", all.Balances[unicom360.BundleEmail])
	// Output: SMS: 1250 email: 4000
}

func ExampleClient_Health() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	h, err := client.Health(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(h.Status, h.Environment)
	// Output: ok production
}

func ExampleError() {
	client, err := unicom360.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.SMS.Send(context.Background(), unicom360.SMSSendParams{
		From:    "NoSuchSender",
		ToOne:   "260971234567",
		Message: "Hello",
	})
	var e *unicom360.Error
	switch {
	case errors.Is(err, unicom360.ErrInsufficientBundle):
		fmt.Println("top up the SMS bundle")
	case errors.As(err, &e) && e.Code == unicom360.CodeInvalidSenderID:
		fmt.Println(e.Class(), e.Code, e.HTTPStatus, e.APIReference)
	case err != nil:
		log.Fatal(err)
	}
	// Output: APIError INVALID_SENDER_ID 200 1764184470578998353
}

func ExampleWithAutoReferenceIDs() {
	// Every send without a user_reference_id gets a generated one, so SMS and WhatsApp
	// sends become safe to retry after a timeout or a 5xx.
	client, err := unicom360.NewClient(unicom360.WithAutoReferenceIDs(true))
	if err != nil {
		log.Fatal(err)
	}
	res, err := client.WhatsApp.SendSession(context.Background(),
		unicom360.TextMessage("260971234567", "Your order is ready."))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.UserReferenceID[:5], len(res.UserReferenceID))
	// Output: u360- 37
}

func ExampleNormalizeCode() {
	fmt.Println(unicom360.NormalizeCode("Invalid From Sender ID"))
	fmt.Println(unicom360.NormalizeCode("INSUFFICIENT_BUNDLE: no international SMS bundle"))
	fmt.Println(unicom360.NormalizeCode("SESSION_WINDOW_CLOSED"))
	fmt.Println(unicom360.NormalizeCode("Missing required keys: [\"message\"]"))
	// Output:
	// INVALID_SENDER_ID
	// INSUFFICIENT_BUNDLE
	// SESSION_WINDOW_CLOSED
	// UNKNOWN
}
