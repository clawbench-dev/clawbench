package sharecore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidToken(t *testing.T) {
	assert.True(t, ValidToken("abc123"))
	assert.True(t, ValidToken("A-Z_a-z-0-9"))
	assert.True(t, ValidToken(strings.Repeat("a", 64)), "64 characters is the upper bound")

	assert.False(t, ValidToken(""), "empty is not a token")
	assert.False(t, ValidToken(strings.Repeat("a", 65)), "65 characters is too long")
	assert.False(t, ValidToken("has space"))
	assert.False(t, ValidToken("has/slash"), "a path separator must not be accepted")
	assert.False(t, ValidToken("has.dot"), "only [0-9A-Za-z_-] is allowed")
	assert.False(t, ValidToken("中文"), "non-ASCII is not a token this server issues")
}
