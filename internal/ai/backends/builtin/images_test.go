package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// withVisionProvider is withProvider with a model the catalog reports as
// vision-capable, which is what gates the image path.
func withVisionProvider(t *testing.T, baseURL string) string {
	t.Helper()
	return visionAgent(t, baseURL, true)
}

// withVisionProviderNoRead is the same agent with the read capability removed:
// the only way it can see a picture is to be sent the bytes.
func withVisionProviderNoRead(t *testing.T, baseURL string) string {
	t.Helper()
	return visionAgent(t, baseURL, false)
}

// withConfinedVisionProvider confines file paths to the working directory, so a
// path elsewhere cannot be opened by the read tool.
func withConfinedVisionProvider(t *testing.T, baseURL string) string {
	t.Helper()
	id := visionAgent(t, baseURL, true)
	agent := model.Agents[id]
	agent.ToolPolicy.RestrictToWorkDir = true
	model.Agents[id] = agent
	return id
}

func visionAgent(t *testing.T, baseURL string, canRead bool) string {
	t.Helper()
	prevCat := model.CatalogSnapshot()
	model.ReplaceCatalog(&model.Catalog{Providers: map[string]model.CatalogProvider{
		"vision": {
			ID:       "vision",
			Protocol: model.ProviderProtocolOpenAI,
			Models:   []model.CatalogModel{{ID: "vision-model", Vision: true}},
		},
	}})
	t.Cleanup(func() { model.ReplaceCatalog(prevCat) })

	prev := model.GetProvider(72)
	model.ReplaceProviders([]*model.Provider{{
		ID: 72, Name: "vision-test", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: baseURL, APIKey: "k",
		Models: []string{"vision-model"}, DefaultModel: "vision-model",
	}})
	t.Cleanup(func() {
		if prev != nil {
			model.ReplaceProviders([]*model.Provider{prev})
			return
		}
		model.ReplaceProviders(nil)
	})

	if model.Agents == nil {
		model.Agents = map[string]*model.Agent{}
	}
	id := "builtin-vision-" + t.Name() + "-" + map[bool]string{true: "read", false: "noread"}[canRead]
	pid := int64(72)
	policy := model.ToolPolicy{}
	if canRead {
		policy.Allow = []string{model.CapFSRead}
	}
	model.Agents[id] = &model.Agent{
		ID: id, Backend: BackendID, BuiltinBackend: true,
		ProviderID: &pid, PreferredModel: "vision-model",
		ToolPolicy: policy,
	}
	t.Cleanup(func() { delete(model.Agents, id) })
	return id
}

// writeImage writes a small file the loader reads as an image. The bytes are
// never decoded here — the contract is which parts reach the model, not whether
// the pixels are valid.
func writeImage(t *testing.T, dir, name string, size int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// The core regression: an attached local picture must reach the model as an
// IMAGE, not merely as a sentence naming its path. With only an image attached
// the prompt is that sentence and nothing else, which is why the reply never
// mentioned the picture.
func TestTurnImageParts_LocalImageIsSent(t *testing.T) {
	path := writeImage(t, t.TempDir(), "shot.png", 8)

	parts, notes := turnImageParts([]model.FileEntry{{Path: path}})
	if len(notes) != 0 {
		t.Fatalf("nothing to report, got %v", notes)
	}
	if len(parts) != 1 || parts[0].Image == nil {
		t.Fatalf("expected one image part, got %+v", parts)
	}
	if parts[0].Image.MediaType != "image/png" || len(parts[0].Image.Data) != 8 {
		t.Fatalf("unexpected image %+v", parts[0].Image)
	}
	if parts[0].Image.URL != "" {
		t.Fatal("a local file has no address; it must be sent as bytes")
	}
}

// A picture that already lives at an internet address is passed as the address:
// no bytes, no download on our side.
func TestTurnImageParts_RemoteImageUsesURL(t *testing.T) {
	const url = "https://example.com/a/b/shot.webp?sig=1"
	parts, notes := turnImageParts([]model.FileEntry{{Kind: "url", URL: url}})
	if len(notes) != 0 {
		t.Fatalf("nothing to report, got %v", notes)
	}
	if len(parts) != 1 || parts[0].Image == nil {
		t.Fatalf("expected one image part, got %+v", parts)
	}
	if parts[0].Image.URL != url {
		t.Fatalf("the address must be passed through, got %q", parts[0].Image.URL)
	}
	if len(parts[0].Image.Data) != 0 {
		t.Fatal("a remote picture must not be downloaded")
	}
}

// A link that is not a picture stays a link in the prompt text; sending it as
// an image makes the endpoint fetch a web page and fail the turn.
func TestTurnImageParts_NonImageLinkIsIgnored(t *testing.T) {
	parts, _ := turnImageParts([]model.FileEntry{
		{Kind: "url", URL: "https://github.com/acme/widgets/issues/7"},
		{Kind: "url", URL: "https://example.com/page.html"},
	})
	if len(parts) != 0 {
		t.Fatalf("only image links may be sent, got %+v", parts)
	}
}

// Formats the vision APIs reject must never be sent: one of them fails the
// whole turn with an opaque endpoint error.
func TestTurnImageParts_SkipsUnsupportedFormats(t *testing.T) {
	dir := t.TempDir()
	parts, _ := turnImageParts([]model.FileEntry{
		{Path: writeImage(t, dir, "scan.pdf", 4)},
		{Path: writeImage(t, dir, "logo.svg", 4)},
		{Path: writeImage(t, dir, "notes.txt", 4)},
		{Path: writeImage(t, dir, "shot.webp", 4)},
	})
	if len(parts) != 1 || parts[0].Image == nil || parts[0].Image.MediaType != "image/webp" {
		t.Fatalf("only the webp may be sent, got %+v", parts)
	}
}

// A picture that cannot be sent must be NAMED. Silence is what made the failure
// invisible: the model read a filename and answered as if nothing was there.
func TestTurnImageParts_OversizeIsReported(t *testing.T) {
	path := writeImage(t, t.TempDir(), "huge.png", maxTurnImageBytes+1)

	parts, notes := turnImageParts([]model.FileEntry{{Path: path}})
	if len(parts) != 0 {
		t.Fatalf("an oversize image must not be sent, got %d parts", len(parts))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "huge.png") || !strings.Contains(notes[0], "too large") {
		t.Fatalf("the model must be told why, got %v", notes)
	}
}

// A missing file is reported for the same reason: the bubble still shows the
// attachment, so the model must not answer as if the user had sent none.
func TestTurnImageParts_MissingFileIsReported(t *testing.T) {
	parts, notes := turnImageParts([]model.FileEntry{{Path: filepath.Join(t.TempDir(), "gone.jpg")}})
	if len(parts) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "could not be opened") {
		t.Fatalf("a missing attachment must be reported, got parts=%v notes=%v", parts, notes)
	}
}

// Only the first few pictures of a message are sent; the rest are reported, so
// the model knows there were more.
func TestTurnImageParts_RespectsCap(t *testing.T) {
	dir := t.TempDir()
	var files []model.FileEntry
	for i := 0; i < maxTurnImages+1; i++ {
		files = append(files, model.FileEntry{Path: writeImage(t, dir, string(rune('a'+i))+".png", 4)})
	}
	parts, notes := turnImageParts(files)
	if len(parts) != maxTurnImages {
		t.Fatalf("expected %d images, got %d", maxTurnImages, len(parts))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "only the first few") {
		t.Fatalf("the dropped image must be reported, got %v", notes)
	}
}

// Quotes and directories are not pictures, and neither is an empty path.
func TestTurnImageParts_IgnoresNonFiles(t *testing.T) {
	parts, notes := turnImageParts([]model.FileEntry{
		{Kind: "quote", Path: "a.png", Text: "quoted"},
		{Path: "/tmp/somedir", IsDir: true},
		{Path: ""},
	})
	if len(parts) != 0 || len(notes) != 0 {
		t.Fatalf("expected nothing, got parts=%v notes=%v", parts, notes)
	}
}

// The preferred form: a picture inside the working directory is POINTED AT, not
// sent. The agent's read tool returns the image, so the bytes only travel when
// the model actually looks — and the path stays in the transcript as the handle.
func TestBuildSpec_ImageInsideWorkDirIsPointedAt(t *testing.T) {
	agentID := withVisionProvider(t, "http://127.0.0.1:1")
	dir := t.TempDir()
	path := writeImage(t, dir, "now.png", 6)

	spec, err := buildSpec(ai.ChatRequest{
		AgentID: agentID,
		Prompt:  "[User uploaded 1 file(s): " + path + "]\n看看这个",
		WorkDir: dir,
		Files:   []model.FileEntry{{Path: path}},
		History: []ai.HistoryMessage{
			{Role: "user", Text: "earlier", ID: 11},
			{Role: "assistant", Text: "ok", ID: 12},
		},
	})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if len(spec.PromptParts) != 0 {
		t.Fatalf("a readable picture must not be sent as bytes, got %+v", spec.PromptParts)
	}
	if !strings.Contains(spec.Prompt, path) || !strings.Contains(spec.Prompt, "read tool") {
		t.Fatalf("the prompt must point the model at the picture, got %q", spec.Prompt)
	}
	for i, m := range spec.History {
		for _, p := range m.Parts {
			if p.Image != nil {
				t.Fatalf("history message %d must not carry an image", i)
			}
		}
	}
}

// Without a read capability the pointer is useless, so the bytes are sent: the
// model has no other way to see the picture.
func TestBuildSpec_NoReadCapabilitySendsBytes(t *testing.T) {
	agentID := withVisionProviderNoRead(t, "http://127.0.0.1:1")
	dir := t.TempDir()
	path := writeImage(t, dir, "now.png", 6)

	spec, err := buildSpec(ai.ChatRequest{
		AgentID: agentID,
		Prompt:  "look",
		WorkDir: dir,
		Files:   []model.FileEntry{{Path: path}},
	})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if len(spec.PromptParts) != 1 || spec.PromptParts[0].Image == nil {
		t.Fatalf("without read the picture must be sent, got %+v", spec.PromptParts)
	}
	if strings.Contains(spec.Prompt, "read tool") {
		t.Fatalf("no hint may be given for a tool the agent does not have: %q", spec.Prompt)
	}
}

// A path the confined read tool cannot open is sent instead, or the picture is
// silently lost.
func TestBuildSpec_OutsideConfinedDirSendsBytes(t *testing.T) {
	agentID := withConfinedVisionProvider(t, "http://127.0.0.1:1")
	work := t.TempDir()
	elsewhere := writeImage(t, t.TempDir(), "outside.png", 6)

	spec, err := buildSpec(ai.ChatRequest{
		AgentID: agentID,
		Prompt:  "look",
		WorkDir: work,
		Files:   []model.FileEntry{{Path: elsewhere}},
	})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if len(spec.PromptParts) != 1 || spec.PromptParts[0].Image == nil {
		t.Fatalf("an unreadable path must be sent as bytes, got %+v", spec.PromptParts)
	}
}

// A text-only model keeps the name-only prompt: an image part would make every
// turn fail at the endpoint.
func TestBuildSpec_TextOnlyModelGetsNothing(t *testing.T) {
	agentID := withProvider(t, "http://127.0.0.1:1") // edge-model is not in the catalog
	dir := t.TempDir()
	path := writeImage(t, dir, "now.png", 6)

	spec, err := buildSpec(ai.ChatRequest{
		AgentID: agentID,
		Prompt:  "look",
		WorkDir: dir,
		Files:   []model.FileEntry{{Path: path}},
	})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	if len(spec.PromptParts) != 0 {
		t.Fatalf("a blind model must not receive image parts, got %+v", spec.PromptParts)
	}
	if strings.Contains(spec.Prompt, "read tool") {
		t.Fatalf("a blind model must not be told to look at a picture: %q", spec.Prompt)
	}
}

// The split itself: hints for what the read tool can reach, bytes for the rest.
func TestReadableImageHints_SplitsByReachability(t *testing.T) {
	dir := t.TempDir()
	inside := writeImage(t, dir, "in.png", 4)
	outside := writeImage(t, t.TempDir(), "out.png", 4)

	hints, fallback := readableImageHints([]model.FileEntry{
		{Path: inside},
		{Path: outside},
		{Kind: "url", URL: "https://example.com/x.png"},
	}, dir, true, true)

	if len(hints) != 1 || hints[0] != inside {
		t.Fatalf("only the reachable file may be hinted, got %v", hints)
	}
	if len(fallback) != 2 {
		t.Fatalf("the unreachable file and the link must be sent, got %+v", fallback)
	}
}

// With the fence off, a path anywhere is reachable, so it is hinted.
func TestReadableImageHints_OpenPolicyHintsAnyPath(t *testing.T) {
	dir := t.TempDir()
	outside := writeImage(t, t.TempDir(), "out.png", 4)

	hints, fallback := readableImageHints([]model.FileEntry{{Path: outside}}, dir, true, false)
	if len(hints) != 1 || len(fallback) != 0 {
		t.Fatalf("an open policy can reach any path, got hints=%v fallback=%v", hints, fallback)
	}
}
