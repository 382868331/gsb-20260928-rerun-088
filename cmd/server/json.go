package main

import (
	"encoding/json"
	"io"
)

func newJSONDecoder(r io.Reader) *json.Decoder { return json.NewDecoder(r) }
func newJSONEncoder(w io.Writer) *json.Encoder { return json.NewEncoder(w) }
