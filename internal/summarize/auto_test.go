package summarize

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSummarizer records how many times it was called and returns a fixed
// marker so a test can tell which backend handled the request.
type recordingSummarizer struct {
	marker string
	calls  int
	err    error
}

func (r *recordingSummarizer) Summarize(_ context.Context, text string, _ string) (string, error) {
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	return r.marker, nil
}

func TestAuto_RoutesCleanShortToSimple(t *testing.T) {
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{marker: "API"}
	a := NewAuto(simple, api)

	// Pure prose, well under the length cap: the cleaned text is returned
	// directly and the LLM is never called.
	got, err := a.Summarize(context.Background(), "这是一段干净的纯文本，可以直接朗读。", "zh")
	require.NoError(t, err)
	assert.Contains(t, got, "干净的纯文本")
	assert.Equal(t, 0, api.calls, "clean short text must not hit the LLM")
}

func TestAuto_RoutesJunkHeavyToAPI(t *testing.T) {
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{marker: "API"}
	a := NewAuto(simple, api)

	// A large code block dominates the message → high junk ratio → LLM.
	text := "看这段：\n```go\n" +
		"func main() { for i := range x { fmt.Println(i) } }\n" +
		"```\n以上。"
	got, err := a.Summarize(context.Background(), text, "zh")
	require.NoError(t, err)
	assert.Equal(t, "API", got)
	assert.Equal(t, 1, api.calls)
	assert.Equal(t, 0, simple.calls)
}

func TestAuto_RoutesLongCleanToAPI(t *testing.T) {
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{marker: "API"}
	a := NewAuto(simple, api)

	// Clean prose but longer than the simple summarizer would keep → LLM.
	long := ""
	for len([]rune(long)) <= autoMaxKeptRunes+10 {
		long += "这是一句很长但是完全干净的中文散文。"
	}
	got, err := a.Summarize(context.Background(), long, "zh")
	require.NoError(t, err)
	assert.Equal(t, "API", got)
	assert.Equal(t, 1, api.calls)
}

func TestAuto_NilAPIFallsBackToSimple(t *testing.T) {
	a := NewAuto(nil, nil)
	got, err := a.Summarize(context.Background(), "任意文本", "zh")
	require.NoError(t, err)
	assert.Equal(t, "任意文本", got)
}

func TestAuto_ThresholdBoundary(t *testing.T) {
	// Pin the >= semantics of the junk-ratio axis by deriving the fixture's
	// EXACT ratio and setting the threshold to it. A "well above threshold"
	// fixture (as an earlier version used) passes for both >= and >, so it
	// cannot catch a reversal.
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{marker: "API"}
	a := NewAuto(simple, api)

	text := "看这段：\n```go\nfunc main() {}\n```\n以上。"
	_, original, kept := StripMarkdownStats(text)
	require.Greater(t, original, kept, "fixture must actually strip something")
	exact := float64(original-kept) / float64(original)

	old := AutoJunkRatio
	defer func() { AutoJunkRatio = old }()

	// Exactly at the threshold → LLM (>=).
	AutoJunkRatio = exact
	_, err := a.Summarize(context.Background(), text, "zh")
	require.NoError(t, err)
	assert.Equal(t, 1, api.calls, "ratio == threshold must route to the LLM (>=)")

	// Just above the threshold → simple (no LLM).
	api.calls = 0
	AutoJunkRatio = exact + 0.001
	_, err = a.Summarize(context.Background(), text, "zh")
	require.NoError(t, err)
	assert.Equal(t, 0, api.calls, "ratio < threshold must stay on the simple path")
}

func TestAuto_LengthBoundary(t *testing.T) {
	// Pin the length axis at exactly autoMaxKeptRunes: the cap itself stays on
	// the simple path (kept > cap is the LLM condition), one rune over goes to
	// the LLM. Clean prose so the junk ratio is 0 and only length can decide.
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{marker: "API"}
	a := NewAuto(simple, api)

	old := AutoJunkRatio
	AutoJunkRatio = 0.5
	defer func() { AutoJunkRatio = old }()

	atCap := strings.Repeat("a", autoMaxKeptRunes)
	_, err := a.Summarize(context.Background(), atCap, "en")
	require.NoError(t, err)
	assert.Equal(t, 0, api.calls, "kept == cap must stay on the simple path")

	overCap := strings.Repeat("a", autoMaxKeptRunes+1)
	_, err = a.Summarize(context.Background(), overCap, "en")
	require.NoError(t, err)
	assert.Equal(t, 1, api.calls, "kept > cap must route to the LLM")
}

func TestAuto_PropagatesAPIError(t *testing.T) {
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{err: errors.New("boom")}
	a := NewAuto(simple, api)

	text := "看这段：\n```go\nfunc main() {}\n```\n以上。"
	_, err := a.Summarize(context.Background(), text, "zh")
	require.Error(t, err)
}

func TestAuto_ImplementsSummarizer(t *testing.T) {
	var _ Summarizer = NewAuto(NewSimple(), nil)
}

func TestAuto_EmptyText(t *testing.T) {
	a := NewAuto(NewSimple(), &recordingSummarizer{marker: "API"})
	got, err := a.Summarize(context.Background(), "", "zh")
	require.NoError(t, err)
	assert.Equal(t, "", got)
}
