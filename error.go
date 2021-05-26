package bencode

import (
	"fmt"
)

type Error error

func newError(format string, a ...interface{}) Error {
	return Error(fmt.Errorf("bencode: "+format, a...))
}

func newParseError(key string, err error) Error {
	return newError("parsing value for key %q: %s", key, err)
}
func newSyntaxError(offset int64, err error) Error {
	return newError("syntax error (Offset: %d): %s", offset, err)
}
func newUnknownValueType(offset int64, b byte) Error {
	return newError("unknown value type %+q", offset, b)
}
func newUnknownType() Error {
	return newError("unknown value type")
}
