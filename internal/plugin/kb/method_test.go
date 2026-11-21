package kb

import (
	"net/http"
	"testing"
)

// 集合与文档两个接口都按方法分派，未列出的方法走 RequireMethod 报 405。
// 这条分支没有业务含义，但少了它，客户端用错方法时会拿到 200/500 而不是
// 明确的 405。
func TestServeKBCollectionsAndDocuments_RejectUnknownMethods(t *testing.T) {
	_, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	for _, h := range []struct {
		name string
		fn   http.HandlerFunc
		path string
	}{
		{"collections", ServeKBCollections, "/api/kb/collections"},
		{"documents", ServeKBDocuments, "/api/kb/documents"},
	} {
		t.Run(h.name, func(t *testing.T) {
			w := callHandler(h.fn, newRequest(t, http.MethodTrace, h.path, nil))
			assertStatus(t, w, http.StatusMethodNotAllowed)
		})
	}
}
