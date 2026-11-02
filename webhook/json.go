package webhook

import "github.com/TechRode-Solution-Limited/unicom360-go/internal/jsonx"

type flexString = jsonx.String

var (
	firstByte  = jsonx.FirstByte
	isObject   = jsonx.IsObject
	clone      = jsonx.Clone
	rawOrNil   = jsonx.RawOrNil
	jsonString = jsonx.StringValue
	jsonBool   = jsonx.BoolValue
)
