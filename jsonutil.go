package unicom360

import "github.com/TechRode-Solution-Limited/unicom360-go/internal/jsonx"

// Short local names for the shared lenient decoding helpers.
type (
	flexString  = jsonx.String
	flexInt     = jsonx.Int
	flexBool    = jsonx.Bool
	flexStrings = jsonx.Strings
)

var (
	firstByte    = jsonx.FirstByte
	isJSONObject = jsonx.IsObject
	isJSONArray  = jsonx.IsArray
	rawOrNil     = jsonx.RawOrNil
	decodeObject = jsonx.DecodeObject
	jsonString   = jsonx.StringValue
	marshalJSON  = jsonx.Marshal
)
