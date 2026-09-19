package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// refusalDetailFromMeta — the real cause behind CodeBuddy's -32603
// ---------------------------------------------------------------------------
//
// CodeBuddy sets stopReason=refusal and reports the generic JSON-RPC code
// -32603 for every internal failure, stashing the actionable cause in
// _meta["codebuddy.ai/errorMessage"] as a JSON-encoded {code,message,data}
// blob. Without extracting data.details the UI shows a code that explains
// nothing (the observed 2026-09-19 refusals were all shell-quote
// "Bad substitution: <expr>" errors).

func TestRefusalDetailFromMeta_StructuredPayloadUsesDetails(t *testing.T) {
	// Shape observed in the CodeBuddy bundle: classifyErrorAsRequestError(...)
	// .toErrorResponse() serialized into _meta.
	meta := map[string]any{
		metaKeyCodeBuddyErrorMessage: `{"code":-32603,"message":"Internal error","data":{"category":"internal","details":"Bad substitution: o.gaps.join"}}`,
	}
	assert.Equal(t, "Bad substitution: o.gaps.join", refusalDetailFromMeta(meta))
}

func TestRefusalDetailFromMeta_FallsBackToMessageWhenNoDetails(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyErrorMessage: `{"code":-32603,"message":"Failed to run function tools: boom"}`,
	}
	assert.Equal(t, "Failed to run function tools: boom", refusalDetailFromMeta(meta))
}

// "Internal error" is the placeholder CodeBuddy itself generates for an
// unclassified failure — echoing it back would be as useless as the code.
func TestRefusalDetailFromMeta_GenericMessageIsNotADetail(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyErrorMessage: `{"code":-32603,"message":"Internal error"}`,
	}
	assert.Equal(t, "", refusalDetailFromMeta(meta))
}

// The sensitive-input block path and the out-of-band control-command failure
// path use an unprefixed key with a plain message string.
func TestRefusalDetailFromMeta_BareStringIsTheDetail(t *testing.T) {
	meta := map[string]any{metaKeyCodeBuddyErrorMessage: "Prompt blocked by policy"}
	assert.Equal(t, "Prompt blocked by policy", refusalDetailFromMeta(meta))
}

func TestRefusalDetailFromMeta_UnprefixedKeyIsAccepted(t *testing.T) {
	meta := map[string]any{metaKeyCodeBuddyErrorMessageBare: "control command failed"}
	assert.Equal(t, "control command failed", refusalDetailFromMeta(meta))
}

// When both keys are present the namespaced one wins — it carries the
// structured payload, while the bare key is the generic path.
func TestRefusalDetailFromMeta_NamespacedKeyWinsOverBare(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyErrorMessage:     `{"code":-32603,"data":{"details":"precise cause"}}`,
		metaKeyCodeBuddyErrorMessageBare: "generic cause",
	}
	assert.Equal(t, "precise cause", refusalDetailFromMeta(meta))
}

// A JSON object that parses but carries neither field must yield nothing — the
// raw envelope is noise, not a reason.
func TestRefusalDetailFromMeta_JSONWithoutUsableFieldsIsNotEchoed(t *testing.T) {
	meta := map[string]any{metaKeyCodeBuddyErrorMessage: `{"code":-32603}`}
	assert.Equal(t, "", refusalDetailFromMeta(meta))
}

func TestRefusalDetailFromMeta_EmptyAndMissing(t *testing.T) {
	assert.Equal(t, "", refusalDetailFromMeta(nil))
	assert.Equal(t, "", refusalDetailFromMeta(map[string]any{}))
	assert.Equal(t, "", refusalDetailFromMeta(map[string]any{"other": "x"}))
	assert.Equal(t, "", refusalDetailFromMeta(map[string]any{metaKeyCodeBuddyErrorMessage: ""}))
	// Non-string values (e.g. an object) carry no readable detail.
	assert.Equal(t, "", refusalDetailFromMeta(map[string]any{metaKeyCodeBuddyErrorMessage: map[string]any{"a": 1}}))
}

// A malformed JSON blob is still a usable detail: the truncated text tells the
// user more than the bare -32603 code.
func TestRefusalDetailFromMeta_MalformedJSONFallsBackToRaw(t *testing.T) {
	meta := map[string]any{metaKeyCodeBuddyErrorMessage: `{"code":-32603,"message":"trunc`}
	assert.Equal(t, `{"code":-32603,"message":"trunc`, refusalDetailFromMeta(meta))
}

func TestRefusalDetailFromMeta_TrimsWhitespace(t *testing.T) {
	meta := map[string]any{
		metaKeyCodeBuddyErrorMessage: `{"code":-32603,"data":{"details":"  spaced cause  "}}`,
	}
	assert.Equal(t, "spaced cause", refusalDetailFromMeta(meta))
}

// An unbounded upstream payload must not be persisted verbatim into every
// warning block; the truncation is rune-based so it never splits a character.
func TestRefusalDetailFromMeta_TruncatesLongDetail(t *testing.T) {
	long := strings.Repeat("字", maxErrorDetailRunes+50)
	meta := map[string]any{
		metaKeyCodeBuddyErrorMessage: `{"code":-32603,"data":{"details":"` + long + `"}}`,
	}
	got := refusalDetailFromMeta(meta)
	assert.True(t, strings.HasSuffix(got, "…"), "truncated detail should be marked")
	assert.Equal(t, maxErrorDetailRunes+1, len([]rune(got)), "should be capped at the limit plus the marker")
	assert.True(t, strings.HasPrefix(got, "字"))
}
