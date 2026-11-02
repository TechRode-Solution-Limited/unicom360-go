// Command send-sms sends one SMS with the Unicom360 Go SDK.
//
//	UNICOM360_API_KEY=... go run ./examples/send-sms -from Unicom360 -to 260971234567 -text "Hello"
//
// Every accepted send is real and billed: there is no sandbox.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	unicom360 "github.com/TechRode-Solution-Limited/unicom360-go"
	"github.com/TechRode-Solution-Limited/unicom360-go/gsm"
)

func main() {
	from := flag.String("from", "", "a sender ID registered on your account")
	to := flag.String("to", "", "comma-separated recipients in international format")
	text := flag.String("text", "", "the message (GSM-7, at most 1,200 characters)")
	ref := flag.String("ref", "", "your reference; makes the send safe to retry")
	flag.Parse()
	if *from == "" || *to == "" {
		flag.Usage()
		os.Exit(2)
	}

	// Optional: check the text the way the gateway will, before paying for a request.
	if issues := gsm.ValidateSMS(*text); len(issues) > 0 {
		for _, c := range gsm.NonGSMCharacters(*text) {
			fmt.Fprintf(os.Stderr, "not GSM-7: %q at %d\n", c.Char, c.Index)
		}
		log.Fatalf("the gateway would refuse this text: %v", issues)
	}
	fmt.Printf("%d characters, %d unit(s) per recipient\n", gsm.Length(*text), gsm.Units(*text))

	client, err := unicom360.NewClient(unicom360.WithAppInfo(unicom360.AppInfo{Name: "send-sms-example"}))
	if err != nil {
		log.Fatal(err) // UNICOM360_API_KEY is missing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := client.SMS.Send(ctx, unicom360.SMSSendParams{
		From:            *from,
		To:              strings.Split(*to, ","),
		Message:         *text,
		UserReferenceID: *ref,
	})
	var e *unicom360.Error
	switch {
	case errors.Is(err, unicom360.ErrInsufficientBundle):
		log.Fatal("the SMS bundle is empty: top up in the portal")
	case errors.Is(err, unicom360.ErrDuplicateReference):
		if errors.As(err, &e) && e.Attempts > 1 {
			log.Print("an earlier attempt went through; the retry was refused as a duplicate")
			return
		}
		log.Fatal("this reference was already used")
	case errors.As(err, &e):
		log.Fatalf("%s %s: %s (api_reference %s)", e.Class(), e.Code, e.Message, e.APIReference)
	case err != nil:
		log.Fatal(err)
	}

	fmt.Println("api_reference:", res.APIReference)
	for _, r := range res.Results {
		fmt.Printf("%s  %-22s %s\n", r.MobileNumber, r.Status, r.MessageID)
	}
	if n := len(res.Invalid()); n > 0 {
		fmt.Printf("%d number(s) were not usable\n", n)
	}
}
