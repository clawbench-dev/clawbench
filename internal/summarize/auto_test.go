package summarize

import (
	"context"
	"errors"
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
	// At exactly the threshold the LLM is used (>= comparison).
	simple := &recordingSummarizer{marker: "SIMPLE"}
	api := &recordingSummarizer{marker: "API"}
	a := NewAuto(simple, api)

	old := AutoJunkRatio
	AutoJunkRatio = 0.5
	defer func() { AutoJunkRatio = old }()

	// "ab" + a code block whose removal leaves ratio well above 0.5.
	text := "ab\n```\n" + "xxxxxxxxxx\n" + "```"
	_, err := a.Summarize(context.Background(), text, "en")
	require.NoError(t, err)
	assert.Equal(t, 1, api.calls)
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
