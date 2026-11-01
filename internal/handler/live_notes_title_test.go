package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

/**
 * Renaming a meeting after its minutes exist.
 *
 * The decision is deliberately separated from the model call: what matters is
 * WHO owns the title (the user typed it, or it is still the date placeholder),
 * not what the model happens to answer. These cases are the whole contract —
 * the summary model is stubbed out.
 */

func TestNameMeetingFromMinutes(t *testing.T) {
	ctx := context.Background()
	const placeholder = "11-01 17:35"

	t.Run("replaces a placeholder with the generated name", func(t *testing.T) {
		var gotMinutes string
		generate := func(_ context.Context, minutes string) (string, error) {
			gotMinutes = minutes
			return "  发版评审  ", nil
		}

		title := nameMeetingFromMinutes(ctx, placeholder, "## 概要\n讨论了发版", generate)

		assert.Equal(t, "发版评审", title)
		assert.Equal(t, "## 概要\n讨论了发版", gotMinutes, "the minutes are what gets titled")
	})

	t.Run("never overwrites a title the user typed", func(t *testing.T) {
		called := false
		generate := func(context.Context, string) (string, error) {
			called = true
			return "模型起的名字", nil
		}

		title := nameMeetingFromMinutes(ctx, "发版评审", "## 概要", generate)

		assert.Equal(t, "发版评审", title)
		assert.False(t, called, "the model must not even be asked")
	})

	t.Run("keeps the placeholder when the model fails", func(t *testing.T) {
		generate := func(context.Context, string) (string, error) {
			return "", errors.New("provider down")
		}

		assert.Equal(t, placeholder, nameMeetingFromMinutes(ctx, placeholder, "## 概要", generate))
	})

	t.Run("keeps the placeholder when the model answers nothing", func(t *testing.T) {
		generate := func(context.Context, string) (string, error) { return "   ", nil }

		assert.Equal(t, placeholder, nameMeetingFromMinutes(ctx, placeholder, "## 概要", generate))
	})
}
