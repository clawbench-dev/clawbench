package wechat

import (
	"strings"
	"testing"

	"clawbench/internal/push/common"

	ilink "github.com/openilink/openilink-sdk-go"
)

// A video is a first-class attachment; the filename is fixed because the
// platform does not send one.
func TestMediaOfItem_VideoCarriesDeclaredSize(t *testing.T) {
	item := &ilink.MessageItem{
		Type:      ilink.ItemVideo,
		VideoItem: &ilink.VideoItem{Media: &ilink.CDNMedia{FullURL: "u"}, VideoSize: 4096},
	}

	ref, ok := mediaOfItem(item)
	if !ok {
		t.Fatal("a video item must be accepted")
	}
	if ref.kind != "video" || ref.filename != "video.mp4" || ref.declaredSize != 4096 {
		t.Fatalf("unexpected ref %+v", ref)
	}

	// A video without its payload is not downloadable and must be skipped.
	if _, ok := mediaOfItem(&ilink.MessageItem{Type: ilink.ItemVideo}); ok {
		t.Fatal("a video without media must be rejected")
	}
}

func TestImageSize_UsesLargestDeclaredDimension(t *testing.T) {
	if got := imageSize(&ilink.ImageItem{HDSize: 10, MidSize: 20, ThumbSize: 30}); got != 30 {
		t.Fatalf("got %d, want the largest", got)
	}
	if got := imageSize(&ilink.ImageItem{HDSize: 50, MidSize: 20, ThumbSize: 30}); got != 50 {
		t.Fatalf("got %d, want the largest", got)
	}
}

func TestParseLen(t *testing.T) {
	if got := parseLen(" 2048 "); got != 2048 {
		t.Fatalf("got %d, want 2048", got)
	}
	if got := parseLen(""); got != 0 {
		t.Fatalf("an empty length must be 0, got %d", got)
	}
	if got := parseLen("abc"); got != 0 {
		t.Fatalf("garbage must be 0, got %d", got)
	}
}

// The quoted block is shown verbatim, so every shape (text, media, title-only,
// empty) needs its own rendering.
func TestQuoteText_Shapes(t *testing.T) {
	if got := quoteText(nil); got != "" {
		t.Fatalf("no quote yields nothing, got %q", got)
	}

	label := common.T("PushQuoteLabel")
	text := quoteText(&ilink.RefMessage{
		Title:       "标题",
		MessageItem: &ilink.MessageItem{TextItem: &ilink.TextItem{Text: "原文"}},
	})
	if !strings.Contains(text, label) || !strings.Contains(text, "标题") || !strings.Contains(text, "原文") {
		t.Fatalf("title and body must both appear, got %q", text)
	}

	bodyOnly := quoteText(&ilink.RefMessage{
		MessageItem: &ilink.MessageItem{TextItem: &ilink.TextItem{Text: "原文"}},
	})
	if strings.Contains(bodyOnly, "|") || !strings.Contains(bodyOnly, "原文") {
		t.Fatalf("a body without a title has no separator, got %q", bodyOnly)
	}

	titleOnly := quoteText(&ilink.RefMessage{Title: "标题"})
	if !strings.Contains(titleOnly, "标题") {
		t.Fatalf("a title without a body must still render, got %q", titleOnly)
	}

	// A quoted attachment has no text: the placeholder keeps the quote visible.
	media := quoteText(&ilink.RefMessage{
		MessageItem: &ilink.MessageItem{Type: ilink.ItemImage, ImageItem: &ilink.ImageItem{}},
	})
	if !strings.Contains(media, common.T("PushQuoteMedia")) {
		t.Fatalf("a quoted attachment must use the media placeholder, got %q", media)
	}

	if got := quoteText(&ilink.RefMessage{}); got != "" {
		t.Fatalf("an empty quote renders nothing, got %q", got)
	}
}

func TestDescribeUnsupported_NamesTheType(t *testing.T) {
	cases := []struct {
		item ilink.MessageItem
		want string
	}{
		{ilink.MessageItem{Type: ilink.ItemVoice}, "voice"},
		{ilink.MessageItem{Type: ilink.ItemImage}, "image"},
		{ilink.MessageItem{Type: ilink.ItemFile}, "file"},
		{ilink.MessageItem{Type: ilink.ItemVideo}, "video"},
		{ilink.MessageItem{Type: ilink.ItemText}, "unknown"},
	}
	for _, tc := range cases {
		got := describeUnsupported(ilink.WeixinMessage{ItemList: []ilink.MessageItem{tc.item}})
		if got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}
