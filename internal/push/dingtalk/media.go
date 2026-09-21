package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/push/common"
)

// dingtalkMessageFileURL is the DingTalk API that exchanges a message's
// downloadCode for a short-lived download URL.
// POST /v1.0/robot/messageFiles/download
var dingtalkMessageFileURL = "https://api.dingtalk.com/v1.0/robot/messageFiles/download"

// Inbound message types that carry downloadable media. DingTalk only delivers
// file/picture/audio/video to a bot in a direct (human↔bot) conversation; this
// bot already ignores non-single chats upstream.
const (
	msgTypeFile    = "file"
	msgTypePicture = "picture"
	// msgTypeRichText is what DingTalk sends when a message mixes text and
	// images (e.g. "@abc look at this" + a screenshot). It is the only way a
	// bot receives text and media TOGETHER, so it is the one type that needs
	// both a text and a media parse.
	msgTypeRichText = "richText"
)

// dingtalkMediaContent is the "content" object of a file or picture callback.
// A file message carries fileName; a picture message carries only downloadCode.
type dingtalkMediaContent struct {
	DownloadCode string `json:"downloadCode"`
	FileName     string `json:"fileName"`
}

// dingtalkRichTextContent is the "content" object of a richText callback:
// a flat list of fragments, each either a text run or a picture.
//
//	{"richText":[{"text":"@abc look"},{"downloadCode":"mIof...","type":"picture"}]}
//
// type is not modeled: the docs only define "picture", and a present
// downloadCode is the reliable signal that a fragment is downloadable.
type dingtalkRichTextContent struct {
	RichText []struct {
		Text         string `json:"text"`
		DownloadCode string `json:"downloadCode"`
	} `json:"richText"`
}

// parseRichText extracts the text and the downloadable image codes from a
// richText callback.
//
// ok is false when content is not a richText object at all (a malformed or
// unexpected payload), which the caller treats the same as "nothing to send".
//
// Text fragments are concatenated with NO separator: they are runs of a single
// line (DingTalk splits on formatting and on each image), so inserting a space
// would corrupt the message and could break an "@{shortID}" prefix match.
func parseRichText(content any) (text string, codes []string, ok bool) {
	// The SDK types Content as interface{}, which json.Unmarshal fills with a
	// map[string]any. A string payload is also possible in principle, so
	// normalize to bytes before decoding rather than type-asserting.
	raw, err := json.Marshal(content)
	if err != nil {
		return "", nil, false
	}
	if len(raw) > 0 && raw[0] == '"' {
		// content was a JSON-encoded string; unwrap it to the object it holds.
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return "", nil, false
		}
		raw = []byte(inner)
	}

	var parsed dingtalkRichTextContent
	if err := json.Unmarshal(raw, &parsed); err != nil {
		slog.Warn("dingtalk: richText content parse failed", "error", err)
		return "", nil, false
	}

	var sb strings.Builder
	seen := make(map[string]struct{}, len(parsed.RichText))
	for _, frag := range parsed.RichText {
		sb.WriteString(frag.Text)
		if frag.DownloadCode == "" {
			continue
		}
		if _, dup := seen[frag.DownloadCode]; dup {
			continue
		}
		seen[frag.DownloadCode] = struct{}{}
		codes = append(codes, frag.DownloadCode)
	}
	return sb.String(), codes, true
}

// mediaDownloadResult is the response of the messageFiles/download API.
type mediaDownloadResult struct {
	DownloadURL string `json:"downloadUrl"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

// extractMedia parses a callback's content into a download request. It returns
// ok=false for message types this bot does not download (text, richText, etc.)
// or when the payload is missing the downloadCode.
func extractMedia(msgType string, content any) (code, filename string, ok bool) {
	if msgType != msgTypeFile && msgType != msgTypePicture {
		return "", "", false
	}

	// The SDK types Content as interface{} and json.Unmarshal gives a
	// map[string]any. Re-marshal into the typed struct rather than type-asserting
	// field by field, so a missing/renamed field surfaces as an empty value
	// instead of a panic.
	raw, err := json.Marshal(content)
	if err != nil {
		return "", "", false
	}
	var media dingtalkMediaContent
	if err := json.Unmarshal(raw, &media); err != nil {
		return "", "", false
	}
	if media.DownloadCode == "" {
		slog.Warn("dingtalk: media message without downloadCode", "msgtype", msgType)
		return "", "", false
	}

	filename = media.FileName
	if filename == "" {
		// A picture message carries no name. Derive one from the download code
		// (DingTalk serves the bytes as ".file") so the extension is at least
		// stable and collision-free.
		if msgType == msgTypePicture {
			filename = "image.png"
		} else {
			filename = "attachment"
		}
	}
	return media.DownloadCode, filename, true
}

// resolveDownloadURL exchanges a downloadCode for a temporary download URL.
// On 401 it invalidates the cached token and retries once, mirroring
// SendMarkdownMessage.
func (m *Manager) resolveDownloadURL(ctx context.Context, downloadCode string) (string, error) {
	url, err := m.resolveDownloadURLOnce(ctx, downloadCode)
	if err == nil {
		return url, nil
	}
	if isDingTalkTokenError(err) {
		slog.Info("dingtalk: token expired resolving download url, retrying")
		m.invalidateToken()
		return m.resolveDownloadURLOnce(ctx, downloadCode)
	}
	return "", err
}

func (m *Manager) resolveDownloadURLOnce(ctx context.Context, downloadCode string) (string, error) {
	token, err := m.getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("dingtalk: get token: %w", err)
	}

	body, err := json.Marshal(map[string]string{
		"downloadCode": downloadCode,
		"robotCode":    m.cfg.AppKey,
	})
	if err != nil {
		return "", fmt.Errorf("dingtalk: marshal download request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dingtalkMessageFileURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("dingtalk: download request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("dingtalk: download fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("dingtalk: token expired (401)")
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("dingtalk: download read: %w", err)
	}

	var result mediaDownloadResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("dingtalk: download parse: %w", err)
	}
	if result.Code != "" && result.Code != "0" {
		if result.Code == "InvalidAuthentication" {
			return "", fmt.Errorf("dingtalk: invalid authentication")
		}
		return "", fmt.Errorf("dingtalk: download error: %s (code %s)", result.Message, result.Code)
	}
	if result.DownloadURL == "" {
		return "", fmt.Errorf("dingtalk: download error: empty downloadUrl")
	}
	return result.DownloadURL, nil
}

// downloadMedia fetches a callback's file/picture into the session's uploads
// directory and returns the attachment entry.
//
// It performs the two-step DingTalk flow: exchange downloadCode for a
// temporary URL, then GET the bytes. The size limit is enforced during the
// download so an oversized file never fully lands on disk.
func (m *Manager) downloadMedia(ctx context.Context, msgType string, content any, projectPath string) (model.FileEntry, error) {
	code, filename, ok := extractMedia(msgType, content)
	if !ok {
		return model.FileEntry{}, fmt.Errorf("dingtalk: not a downloadable media message")
	}

	url, err := m.resolveDownloadURL(ctx, code)
	if err != nil {
		return model.FileEntry{}, err
	}

	entry, err := common.DownloadAttachment(m.httpClient, url, projectPath, filename)
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("dingtalk: %w", err)
	}
	return entry, nil
}
