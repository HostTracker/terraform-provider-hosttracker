package client

import (
	"bytes"
	"io"
)

// typeAliases maps the spellings the API accepts on a write to the token it
// stores and reads back. Keeping a practitioner's spelling out of state
// without this map would make every plan show a change that is not one.
var typeAliases = map[string]string{
	"pagespeed": "waterfall",
}

// NormalizeType resolves a monitor-type alias to the token the API reads
// back. An unknown token is returned unchanged: the type vocabulary is
// open, and a value this release has never heard of is still valid.
func NormalizeType(v string) string {
	if canonical, ok := typeAliases[lower(v)]; ok {
		return canonical
	}
	return v
}

// SameType reports whether two spellings name the same monitor type.
func SameType(a, b string) bool {
	return NormalizeType(a) == NormalizeType(b)
}

// PreferredType keeps the practitioner's spelling while it still names the
// type the API reports, and adopts the API's token once it does not. That
// is what lets `type = "pageSpeed"` stay in the configuration without a
// perpetual diff against the stored `waterfall`.
func PreferredType(prior, server string) string {
	if prior != "" && SameType(prior, server) {
		return prior
	}
	return server
}

func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
