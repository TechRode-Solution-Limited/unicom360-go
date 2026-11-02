package gsm_test

import (
	"fmt"
	"strings"

	"github.com/TechRode-Solution-Limited/unicom360-go/gsm"
)

func ExampleValidateSMS() {
	fmt.Println(gsm.ValidateSMS("Your order ZM4821 is ready."))
	fmt.Println(gsm.ValidateSMS("It’s ready 😀"))
	fmt.Println(gsm.ValidateSMS("   "))
	fmt.Println(gsm.ValidateSMS(strings.Repeat("a", 1201)))
	// Output:
	// []
	// [non_gsm]
	// [empty]
	// [too_long]
}

func ExampleUnits() {
	text := strings.Repeat("a", 161)
	fmt.Println(gsm.Length(text), gsm.Units(text))
	fmt.Println(gsm.Length("Hi\r\nthere"), gsm.Units("Hi\r\nthere")) // CR LF counts as one
	// Output:
	// 161 2
	// 8 1
}

func ExampleNonGSMCharacters() {
	for _, c := range gsm.NonGSMCharacters("It’s “done”") {
		fmt.Printf("%q at %d\n", c.Char, c.Index)
	}
	// Output:
	// '’' at 2
	// '“' at 5
	// '”' at 10
}

func ExampleIsGSM7() {
	fmt.Println(gsm.IsGSM7("Price: 5€ {incl. VAT}"))
	fmt.Println(gsm.IsGSM7("Ça coûte 5€"))
	// Output:
	// true
	// false
}
