//go:build !jsonv2

package jsonutil

import (
	"encoding/json"
)

// Marshal encodes v into JSON bytes using encoding/json v1
func Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes JSON bytes into v using encoding/json v1
func Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// RawMessage is an alias for json.RawMessage
type RawMessage = json.RawMessage
