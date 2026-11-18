package summarize

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrOutputNotPlain is returned when the model replied with an API envelope
// instead of the text that was asked for (a title, a next-step suggestion).
// Such a reply is not a shorter answer: nothing in it is usable.
var ErrOutputNotPlain = errors.New("summarizer returned an API payload instead of text")

// looksLikeEnvelope reports whether s is a JSON object carrying a chat
// completion / messages response. Providers do return their own response body
// as the content when a reasoning model finished without any output — and a
// session name or a "fill in" button then shows the whole payload. Only
// envelope semantics count: a suggestion that legitimately quotes user code
// such as {"name": "x"} is not rejected, and a plain JSON document is not
// text we asked for anyway.
func looksLikeEnvelope(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		return false
	}
	if _, ok := obj["choices"]; ok {
		return true
	}
	for _, key := range []string{"error", "errors"} {
		if _, ok := obj[key]; ok {
			return true
		}
	}
	if typ, ok := obj["type"]; ok {
		var t string
		if json.Unmarshal(typ, &t) == nil && (t == "error" || t == "message") {
			return true
		}
	}
	if _, ok := obj["usage"]; ok {
		if _, ok := obj["model"]; ok {
			return true
		}
	}
	return false
}

// RejectEnvelope turns an envelope payload into ErrOutputNotPlain. Callers that
// persist the output (session titles, recommendations) use it so a gateway
// quirk never becomes a stored title.
func RejectEnvelope(what, text string) error {
	if looksLikeEnvelope(text) {
		return fmt.Errorf("%w (%s)", ErrOutputNotPlain, what)
	}
	return nil
}
