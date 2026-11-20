package ws

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOriginPatterns(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com:20000/ws", nil)
	got := OriginPatterns(r)

	assert.Contains(t, got, "http://example.com:20000")
	assert.Contains(t, got, "https://example.com:20000")
	assert.Contains(t, got, "http://localhost:*")
	assert.Contains(t, got, "https://127.0.0.1:*")
}
