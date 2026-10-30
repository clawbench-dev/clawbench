package wechat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	ilink "github.com/openilink/openilink-sdk-go"
)

// apiErrorDoer turns an in-body API error into a Go error.
//
// The iLink SDK's send path only inspects the HTTP status: a 200 response whose
// body is {"ret":-1,"errcode":...} is reported as success. That matters here
// because a "successful" push makes the caller delete the pending event, so the
// notification would be lost with no trace — the exact failure mode the push
// layer is supposed to prevent.
//
// It is scoped to the bot API (`/ilink/bot/…`), for two reasons:
//
//   - CDN downloads are ciphertext. Reading them here just to find no JSON
//     envelope would buffer every attachment twice (this pass + the SDK's own
//     read), which with a 100 MB cap and concurrent messages is an OOM risk.
//   - GetUpdates parses `ret` itself and needs to SEE -14 to trigger the
//     re-scan path; erroring out here would turn an expired session into an
//     ordinary poll failure with backoff.
type apiErrorDoer struct{ inner ilink.HTTPDoer }

func (d apiErrorDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.inner.Do(req)
	if err != nil {
		return resp, err
	}
	if resp == nil {
		// Defensive: returning (nil, nil) would make the SDK nil-deref on
		// resp.Body.
		return nil, errors.New("wechat: nil http response")
	}
	// Non-2xx is already turned into an *ilink.HTTPError by the SDK.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, nil
	}
	// Only the bot API carries the ret/errcode envelope; see the type comment.
	if !strings.Contains(req.URL.Path, "/ilink/bot/") {
		return resp, nil
	}
	// GetUpdates owns its own ret handling (and needs -14 intact).
	if strings.Contains(req.URL.Path, "getupdates") {
		return resp, nil
	}

	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	// Restore the body for the SDK's own read.
	resp.Body = io.NopCloser(bytes.NewReader(body))

	var envelope struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	// A body that is not a JSON envelope is left to the caller: only a
	// well-formed `ret`/`errcode` pair is this wrapper's business.
	if json.Unmarshal(body, &envelope) == nil && (envelope.Ret != 0 || envelope.ErrCode != 0) {
		return nil, &ilink.APIError{
			Ret:     envelope.Ret,
			ErrCode: envelope.ErrCode,
			ErrMsg:  envelope.ErrMsg,
		}
	}
	return resp, nil
}
