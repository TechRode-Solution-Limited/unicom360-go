package unicom360

import "regexp"

// Normalised error codes (Error.Code). Codes are an open set: any other UPPER_SNAKE code the
// gateway sends passes through NormalizeCode unchanged.
const (
	// CodeUnknown is the code of a message that is not a code: a request-shape sentence, a
	// framework error, an empty or missing message.
	CodeUnknown = "UNKNOWN"

	CodeInvalidSenderID       = "INVALID_SENDER_ID"
	CodeSenderIDNotAllowed    = "SENDER_ID_NOT_ALLOWED"
	CodeInvalidRecipientLimit = "INVALID_RECIPIENT_LIMIT"
	CodeInsufficientBundle    = "INSUFFICIENT_BUNDLE"
	CodeDuplicateReference    = "DUPLICATE_REFERENCE"
	CodeInvalidParameters     = "INVALID_PARAMETERS"
	CodeMissingAPIKey         = "MISSING_API_KEY"
	CodeInvalidAuthorization  = "INVALID_AUTHORIZATION"

	CodeSenderRequired          = "SENDER_REQUIRED"
	CodeTemplateNotFound        = "TEMPLATE_NOT_FOUND"
	CodeTemplateNotApproved     = "TEMPLATE_NOT_APPROVED"
	CodeLanguageRequired        = "LANGUAGE_REQUIRED"
	CodeTemplateSenderMismatch  = "TEMPLATE_SENDER_MISMATCH"
	CodeNoValidRecipients       = "NO_VALID_RECIPIENTS"
	CodeSendFailed              = "SEND_FAILED"
	CodeSessionWindowClosed     = "SESSION_WINDOW_CLOSED"
	CodeReplyToNotFound         = "REPLY_TO_NOT_FOUND"
	CodeReactionTargetNotFound  = "REACTION_TARGET_NOT_FOUND"
	CodeInvalidNumber           = "INVALID_NUMBER"
	CodeInvalidUserReferenceID  = "INVALID_USER_REFERENCE_ID"
	CodeMediaURLInvalid         = "MEDIA_URL_INVALID"
	CodeClientNotFound          = "CLIENT_NOT_FOUND"
	CodeAuthenticationFailed    = "AUTHENTICATION_FAILED"
	CodeCredentialDisabled      = "CREDENTIAL_DISABLED"
	CodeClientDisabled          = "CLIENT_DISABLED"
	CodeCredentialExpired       = "CREDENTIAL_EXPIRED"
	CodeIPNotAllowed            = "IP_NOT_ALLOWED"
	CodeRateLimitExceeded       = "RATE_LIMIT_EXCEEDED"
	CodeBodyParametersInvalid   = "BODY_PARAMETERS_INVALID"
	CodeHeaderParameterInvalid  = "HEADER_PARAMETER_INVALID"
	CodeButtonParametersInvalid = "BUTTON_PARAMETERS_INVALID"
	CodeOTPRequired             = "OTP_REQUIRED"
	CodeOTPInvalid              = "OTP_INVALID"
)

// codeAliases maps the gateway's exact messages (and legacy codes) to normalised codes.
var codeAliases = map[string]string{
	"Invalid From Sender ID":                           CodeInvalidSenderID,
	"Invalid Sender":                                   CodeInvalidSenderID,
	"From Sender ID not allowed":                       CodeSenderIDNotAllowed,
	"Sender not allowed":                               CodeSenderIDNotAllowed,
	"Recipient list size is invalid":                   CodeInvalidRecipientLimit,
	"Invalid Recipient Limit":                          CodeInvalidRecipientLimit,
	"INSUFFICIENT_BUNDLE: no international SMS bundle": CodeInsufficientBundle,
	"Insufficient bundle":                              CodeInsufficientBundle,
	"INSUFFICIENT_BUNDLE_LIMIT":                        CodeInsufficientBundle,
	"duplicate user reference id":                      CodeDuplicateReference,
	"DUPLICATE_MSG_REF":                                CodeDuplicateReference,
	"Invalid parameters provided":                      CodeInvalidParameters,
	"Missing API key. Provide X-API-Key header or Authorization: Bearer <api_key>": CodeMissingAPIKey,
	"Invalid authorization format. Use 'Bearer <api_key>' or 'Basic <base64>'":     CodeInvalidAuthorization,
	"Invalid Basic auth format. Use api_key as username":                           CodeInvalidAuthorization,
	"Invalid Base64 encoding in Basic auth":                                        CodeInvalidAuthorization,
}

var (
	codeRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	// The whitespace class is JavaScript's \s, so the rule matches the reference
	// implementation exactly.
	codePrefixRE = regexp.MustCompile(`^([A-Z][A-Z0-9_]*):[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`)
)

// NormalizeCode turns a gateway message into a normalised code. The first rule that matches
// wins:
//
//  1. an exact entry in the alias table ("Invalid From Sender ID" is INVALID_SENDER_ID);
//  2. a message that is a whole UPPER_SNAKE code is that code (or its alias);
//  3. a message starting "CODE: " is CODE (or its alias);
//  4. anything else is CodeUnknown.
//
// Matching is exact and case-sensitive; nothing is trimmed.
func NormalizeCode(message string) string {
	if c, ok := codeAliases[message]; ok {
		return c
	}
	if codeRE.MatchString(message) {
		return message
	}
	if m := codePrefixRE.FindStringSubmatch(message); m != nil {
		if c, ok := codeAliases[m[1]]; ok {
			return c
		}
		return m[1]
	}
	return CodeUnknown
}
