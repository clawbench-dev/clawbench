package askquestion

import (
	"strings"
	"testing"
)

// The legacy tag is degraded to readable Markdown, never to a card. These tests
// pin the user-visible outcomes: no parser-only field value, no lost separator,
// no content loss, and no false positives on prose or code.

func TestLegacy_ParsedIsAlwaysFalse(t *testing.T) {
	// A legacy span must never become a card: AllItems only reports parsed
	// spans, so a Parsed=true legacy span would render both a card and text.
	text := "<ask-question>\n<item>\n<header>H</header>\n<question>Q?</question>\n" +
		"<option><label>A</label><description>da</description></option>\n</item>\n</ask-question>"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	if ms[0].Parsed {
		t.Error("legacy span must not be Parsed")
	}
	if ms[0].Reason != ReasonLegacyFormat {
		t.Errorf("reason = %q, want %q", ms[0].Reason, ReasonLegacyFormat)
	}
	if got := AllItems(ms); len(got) != 0 {
		t.Errorf("AllItems must be empty for a legacy span, got %+v", got)
	}
	if HasParsed(ms) {
		t.Error("HasParsed must be false for a legacy span")
	}
}

func TestLegacy_MultiSelectValueDoesNotLeak(t *testing.T) {
	// The reported defect: `<multi-select>false</multi-select>` was stripped of
	// its tag but its text node survived, so the literal `false` appeared in the
	// message body.
	text := "<ask-question>\n<item>\n<header>下一步</header>\n<multi-select>false</multi-select>\n" +
		"<question>你想怎么做？</question>\n" +
		"<option><label>只修本地能用</label><description>先保证自己 iOS 上传恢复</description></option>\n" +
		"</item>\n</ask-question>"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	fb := ms[0].Fallback
	if strings.Contains(fb, "false") {
		t.Errorf("multi-select value leaked into the fallback: %q", fb)
	}
	want := "**下一步**\n你想怎么做？\n- 只修本地能用 — 先保证自己 iOS 上传恢复"
	if fb != want {
		t.Errorf("fallback mismatch\n got: %q\nwant: %q", fb, want)
	}
}

func TestLegacy_LabelAndDescriptionKeepTheirSeparator(t *testing.T) {
	// The second reported symptom: stripping the tags glued a label to its
	// description with no separator.
	text := "<ask-question>\n<item>\n<question>Q?</question>\n" +
		"<option>\n<label>Option A</label>\n<description>Fast but less safe</description>\n</option>\n" +
		"</item>\n</ask-question>"
	fb := Extract(text)[0].Fallback
	if !strings.Contains(fb, "Option A — Fast but less safe") {
		t.Errorf("label and description are not separated by an em dash: %q", fb)
	}
}

func TestLegacy_OptionBodyWinsOverAttribute(t *testing.T) {
	// Production has 320 options shaped `<option value="A">A. …</option>`: the
	// attribute is only a key and the body carries the real label. Preferring
	// the attribute would render a bare "A" and discard the sentence.
	text := "<ask-question>\n<item>\n<question>Q?</question>\n" +
		"<option value=\"A\">A. 只读可见性</option>\n" +
		"<option value=\"B\">B. 双向同步</option>\n</item>\n</ask-question>"
	fb := Extract(text)[0].Fallback
	if !strings.Contains(fb, "- A. 只读可见性") || !strings.Contains(fb, "- B. 双向同步") {
		t.Errorf("option body text was not preferred over the attribute: %q", fb)
	}
}

func TestLegacy_AttributeIsUsedWhenBodyIsEmpty(t *testing.T) {
	text := "<ask-question>\n<item>\n<question>Q?</question>\n" +
		"<option value=\"only\"></option>\n</item>\n</ask-question>"
	fb := Extract(text)[0].Fallback
	if !strings.Contains(fb, "- only") {
		t.Errorf("attribute label was dropped when the body was empty: %q", fb)
	}
}

func TestLegacy_UnclosedWrapperEndsAtLastChildClose(t *testing.T) {
	// 13% of real payloads lose the wrapper close. The span must stop at its
	// last child close rather than swallowing the following prose.
	text := "前言\n<ask-question>\n<item>\n<header>H</header>\n<question>Q?</question>\n" +
		"<option><label>A</label></option>\n</item>\n\n后续正文必须保留。"
	ms := Extract(text)
	if len(ms) != 1 {
		t.Fatalf("expected 1 match, got %d", len(ms))
	}
	if strings.Contains(ms[0].Fallback, "后续正文") {
		t.Errorf("span swallowed following prose: %q", ms[0].Fallback)
	}
	if !strings.Contains(Strip(text, ms), "后续正文必须保留。") {
		t.Errorf("following prose was lost: %q", Strip(text, ms))
	}
}

func TestLegacy_SiblingPayloadsEachDegrade(t *testing.T) {
	// A span must never swallow the next payload.
	text := "<ask-question>\n<item><header>Q1</header><question>A?</question>" +
		"<option><label>X</label></option></item>\n</ask-question>\n中间\n" +
		"<ask-question>\n<item><header>Q2</header><question>B?</question>" +
		"<option><label>Y</label></option></item>\n</ask-question>"
	ms := Extract(text)
	if len(ms) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(ms))
	}
	for i, m := range ms {
		if m.Parsed {
			t.Errorf("match %d must not be parsed", i)
		}
	}
	joined := ms[0].Fallback + "|" + ms[1].Fallback
	if !strings.Contains(joined, "Q1") || !strings.Contains(joined, "Q2") {
		t.Errorf("both payloads must degrade, got %q", joined)
	}
	if strings.Contains(ms[0].Fallback, "Q2") {
		t.Errorf("first span swallowed the second payload: %q", ms[0].Fallback)
	}
}

func TestLegacy_UnclosedPayloadDoesNotSwallowASibling(t *testing.T) {
	// Production has unclosed payloads directly followed by another payload
	// (block #24582 holds three). Without clamping the search region at the next
	// line-start open tag, the first span would run to the SECOND payload's
	// close and swallow it — one question would render twice and the other not
	// at all.
	text := "<ask-question>\n<item><header>Q1</header><question>A?</question>" +
		"<option><label>X</label></option></item>\n" +
		"<ask-question>\n<item><header>Q2</header><question>B?</question>" +
		"<option><label>Y</label></option></item>\n</ask-question>"
	ms := Extract(text)
	if len(ms) != 2 {
		t.Fatalf("expected 2 matches, got %d (%+v)", len(ms), ms)
	}
	if strings.Contains(ms[0].Fallback, "Q2") || strings.Contains(ms[0].Fallback, "Y") {
		t.Errorf("unclosed first span swallowed the sibling payload: %q", ms[0].Fallback)
	}
	if !strings.Contains(ms[1].Fallback, "Q2") {
		t.Errorf("second payload was not degraded: %q", ms[1].Fallback)
	}
	stripped := Strip(text, ms)
	for _, want := range []string{"Q1", "Q2"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped output lost %q: %q", want, stripped)
		}
	}
}

func TestLegacy_ProseMentionIsLeftAlone(t *testing.T) {
	// The assistant discusses the tag in ordinary sentences. Turning those into
	// spans would delete the sentence from the visible text.
	for _, text := range []string{
		"要发起提问，就用 <ask-question> 标签包起来。",
		"the fix stripped <ask-question> tags from e.blocks, then Finalize ran",
	} {
		if ms := Extract(text); len(ms) != 0 {
			t.Errorf("prose mention became a span: %q -> %+v", text, ms)
		}
	}
}

func TestLegacy_CodeContextsAreIgnored(t *testing.T) {
	for _, text := range []string{
		"用 `<ask-question>` 标签来提问。",
		"示例：\n\n```\n<ask-question>\n<item><question>Q?</question></item>\n</ask-question>\n```\n\n完毕。",
	} {
		if ms := Extract(text); len(ms) != 0 {
			t.Errorf("tag inside code became a span: %q -> %+v", text, ms)
		}
	}
}

func TestLegacy_BrokenJSONIsSalvaged(t *testing.T) {
	// Two production payloads are JSON with a stray brace. Their field values
	// are intact, so they must be recovered rather than shown as raw JSON.
	text := "<ask-question>\n{\"questions\":[{\"header\":\"文件命名\",\"multiSelect\":false," +
		"\"options\":[{\"label\":\"claude_tool.go\",\"description\":\"Claude 协议族\"}]," +
		"\"question\":\"工具解析文件命名？\"}]}}\n</ask-question>"
	fb := Extract(text)[0].Fallback
	if strings.Contains(fb, "{") || strings.Contains(fb, "\"questions\"") {
		t.Errorf("raw JSON leaked into the fallback: %q", fb)
	}
	for _, want := range []string{"文件命名", "工具解析文件命名？", "claude_tool.go — Claude 协议族"} {
		if !strings.Contains(fb, want) {
			t.Errorf("fallback missing %q: %q", want, fb)
		}
	}
}

func TestLegacy_DSMLArtifactsAreDropped(t *testing.T) {
	// 12297 of these sentinel-mangled closers exist in production, always as
	// garbage inside a payload. They must not reach the message body.
	text := "<ask-question>\n  <item>\n    <header>图标颜色</\uFF5C\uFF5CDSML\uFF5C\uFF5Cparameter>\n" +
		"</\uFF5C\uFF5CDSML\uFF5C\uFF5Cinvoke>\n</\uFF5C\uFF5CDSML\uFF5C\uFF5Ctool_calls>"
	fb := Extract(text)[0].Fallback
	if strings.Contains(fb, "DSML") || strings.Contains(fb, "\uFF5C") {
		t.Errorf("harness artifact leaked into the fallback: %q", fb)
	}
	if !strings.Contains(fb, "图标颜色") {
		t.Errorf("real header text was lost: %q", fb)
	}
}

func TestLegacy_TruncatedPayloadKeepsItsText(t *testing.T) {
	// A payload truncated mid-stream has no close and no child close. Its text
	// is the only copy of the question, so it must be kept.
	fb := Extract("<ask-question>\n  <item>\n    <header>下一步操作")[0].Fallback
	if fb != "下一步操作" {
		t.Errorf("fallback = %q, want %q", fb, "下一步操作")
	}
}

func TestLegacy_CurrentTagIsUnaffected(t *testing.T) {
	text := "<clawbench-ask-question>\n**H**\nQ?\n- A\n</clawbench-ask-question>"
	ms := Extract(text)
	if len(ms) != 1 || !ms[0].Parsed {
		t.Fatalf("current tag must still parse into a card, got %+v", ms)
	}
}

func TestLegacy_CoexistsWithCurrentTagInSourceOrder(t *testing.T) {
	// Strip walks the matches linearly, so an out-of-order slice would drop
	// spans. Both tag names may occur in one block.
	text := "旧：\n<ask-question>\n<item><header>Old</header><question>Q?</question>" +
		"<option><label>A</label></option></item>\n</ask-question>\n新：\n" +
		"<clawbench-ask-question>\n**New**\nQ?\n- B\n</clawbench-ask-question>"
	ms := Extract(text)
	if len(ms) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(ms))
	}
	if ms[0].Start >= ms[1].Start {
		t.Errorf("matches are not in source order: %d then %d", ms[0].Start, ms[1].Start)
	}
	if ms[0].Parsed || !ms[1].Parsed {
		t.Errorf("expected legacy unparsed then current parsed, got %v %v", ms[0].Parsed, ms[1].Parsed)
	}
	stripped := Strip(text, ms)
	if !strings.Contains(stripped, "旧：") || !strings.Contains(stripped, "新：") {
		t.Errorf("surrounding prose was lost: %q", stripped)
	}
	if !strings.Contains(stripped, "**Old**") {
		t.Errorf("legacy payload text was not degraded into the stream: %q", stripped)
	}
	if strings.Contains(stripped, "Old</header>") {
		t.Errorf("raw markup leaked: %q", stripped)
	}
}

func TestLegacy_NoContentIsLost(t *testing.T) {
	// Every user-visible field must survive the degradation.
	text := "<ask-question>\n<item>\n<header>H</header>\n<multi-select>true</multi-select>\n" +
		"<question>Q?</question>\n<option><label>L</label><description>D</description></option>\n" +
		"</item>\n</ask-question>"
	fb := Extract(text)[0].Fallback
	for _, want := range []string{"H", "Q?", "L", "D"} {
		if !strings.Contains(fb, want) {
			t.Errorf("fallback lost %q: %q", want, fb)
		}
	}
}
