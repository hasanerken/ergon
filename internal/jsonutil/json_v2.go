//go:build jsonv2

package jsonutil

import (
	jsonv2 "encoding/json/v2"
)

// Marshal encodes v into JSON bytes using encoding/json v2
func Marshal(v interface{}) ([]byte, error) {
	return jsonv2.Marshal(v)
}

// Unmarshal decodes JSON bytes into v using encoding/json v2
func Unmarshal(data []byte, v interface{}) error {
	return jsonv2.Unmarshal(data, v)
}

// RawMessage is an alias for jsonv2.RawMessage
type RawMessage = jsonv2.RawMessage
