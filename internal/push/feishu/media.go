package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"clawbench/internal/model"
	"clawbench/internal/push/common"
)

// feishuOpenBaseURL is the Feishu OpenAPI base URL. It is a package variable so
// tests can point the SDK at an httptest server.
var feishuOpenBaseURL = "https://open.feishu.cn"

// larkCliMu guards lazy construction of Manager.larkCli.
var larkCliMu sync.Mutex

// Inbound message types that carry downloadable media.
const (
	msgTypeFile  = "file"
	msgTypeImage = "image"
)

// feishuMediaContent is the "content" object of a file or image callback.
// A file message carries file_name; an image message carries only image_key.
type feishuMediaContent struct {
	FileKey  string `json:"file_key"`
	FileName string `json:"file_name"`
	ImageKey string `json:"image_key"`
}

// extractMedia parses a callback's content into a download request.
//
// It returns the resource key and the resource type expected by Feishu's
// message-resource API ("file" for files, "image" for images), plus a display
// filename. ok=false for message types this bot does not download (text, post,
// audio, media, ...) or when the payload is missing its key.
func extractMedia(msgType, content string) (key, resType, filename string, ok bool) {
	if content == "" {
		return "", "", "", false
	}

	var media feishuMediaContent
	if err := json.Unmarshal([]byte(content), &media); err != nil {
		slog.Warn("feishu: media content parse failed", "error", err, "msgtype", msgType)
		return "", "", "", false
	}

	switch msgType {
	case msgTypeFile:
		if media.FileKey == "" {
			slog.Warn("feishu: file message without file_key")
			return "", "", "", false
		}
		name := media.FileName
		if name == "" {
			name = "attachment"
		}
		return media.FileKey, msgTypeFile, name, true
	case msgTypeImage:
		if media.ImageKey == "" {
			slog.Warn("feishu: image message without image_key")
			return "", "", "", false
		}
		// An image message carries no name; the resource API returns a
		// Content-Type but the saved extension still has to come from
		// somewhere, so default to .png (the common case for a screenshot).
		return media.ImageKey, msgTypeImage, "image.png", true
	default:
		return "", "", "", false
	}
}

// larkClient returns the SDK client used for OpenAPI calls, building it on
// first use. Lazy construction keeps Manager creation (which happens during
// startup and hot-reload) free of SDK side effects.
//
// The client carries no token of its own: downloadMedia passes the token from
// this Manager's cache explicitly, so a hot-reload credential change is picked
// up without rebuilding the client.
func (m *Manager) larkClient() *lark.Client {
	larkCliMu.Lock()
	defer larkCliMu.Unlock()
	if m.larkCli == nil {
		m.larkCli = lark.NewClient(
			m.cfg.AppID, m.cfg.AppSecret,
			lark.WithOpenBaseUrl(feishuOpenBaseURL),
			lark.WithEnableTokenCache(false),
			lark.WithReqTimeout(0),
		)
	}
	return m.larkCli
}

// downloadMedia fetches a callback's file/image into the session's uploads
// directory and returns the attachment entry.
//
// It calls GET /open-apis/im/v1/messages/{message_id}/resources/{file_key}
// with the tenant token. The size limit is enforced during the download so an
// oversized resource never fully lands on disk.
func (m *Manager) downloadMedia(ctx context.Context, msgType, content, messageID, projectPath string) (model.FileEntry, error) {
	key, resType, filename, ok := extractMedia(msgType, content)
	if !ok {
		return model.FileEntry{}, fmt.Errorf("feishu: not a downloadable media message")
	}
	if messageID == "" {
		return model.FileEntry{}, fmt.Errorf("feishu: missing message_id for resource download")
	}

	token, err := m.getAccessToken(ctx)
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("feishu: get token: %w", err)
	}

	// The SDK's MessageResource.Get streams the body via GetMessageResourceResp.File.
	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(key).
		Type(resType).
		Build()

	resp, err := m.larkClient().Im.MessageResource.Get(ctx, req,
		larkcore.WithTenantAccessToken(token))
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("feishu: resource fetch: %w", err)
	}
	if !resp.Success() {
		return model.FileEntry{}, fmt.Errorf("feishu: resource error: %s (code %d)", resp.Msg, resp.Code)
	}

	entry, err := common.SaveAttachment(projectPath, filename, resp.File, common.AttachmentMaxBytes())
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("feishu: %w", err)
	}
	slog.Debug("feishu: attachment saved", "path", entry.Path, "msgtype", msgType)
	return entry, nil
}
