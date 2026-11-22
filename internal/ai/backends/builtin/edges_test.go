package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
)

// Small helpers whose edges decide what the picker shows and what the model
// actually receives. The happy paths are covered elsewhere; these pin the
// "nothing trustworthy to report" answers, which are the ones that used to leak
// a wrong model name or a path the agent cannot open.

func TestHasModelID(t *testing.T) {
	models := []model.AgentModel{{ID: "a"}, {ID: "b"}}
	if !hasModelID(models, "b") {
		t.Fatal("an id in the list must be found")
	}
	if hasModelID(models, "c") {
		t.Fatal("an id outside the list must not be found")
	}
	if hasModelID(nil, "a") {
		t.Fatal("an empty list has no ids")
	}
}

func TestWithinDir(t *testing.T) {
	// t.TempDir is absolute on every platform; a bare "\work\proj" is
	// drive-relative on Windows and fails the IsAbs check for the wrong reason.
	dir := t.TempDir()
	cases := []struct {
		name string
		dir  string
		path string
		want bool
	}{
		{"inside", dir, filepath.Join(dir, "a", "b.png"), true},
		{"the dir itself", dir, dir, true},
		{"sibling with a shared prefix", dir, dir + "-other/b.png", false},
		{"parent", dir, filepath.Join(dir, "..", "b.png"), false},
		{"unknown dir", "", filepath.Join(dir, "a.png"), false},
		{"relative path", dir, "a.png", false},
	}
	for _, c := range cases {
		if got := withinDir(c.dir, c.path); got != c.want {
			t.Fatalf("%s: withinDir(%q, %q) = %v, want %v", c.name, c.dir, c.path, got, c.want)
		}
	}
}

func TestImageMediaType(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"a.png", "image/png", true},
		{"A.PNG", "image/png", true},
		{"a.jpg", "image/jpeg", true},
		{"a.jpeg", "image/jpeg", true},
		{"a.gif", "image/gif", true},
		{"a.webp", "image/webp", true},
		{"https://x/y.png?token=1#frag", "image/png", true},
		{"a.pdf", "", false},
		{"a.svg", "", false},
		{"noext", "", false},
	}
	for _, c := range cases {
		got, ok := imageMediaType(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("imageMediaType(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// The notice is rendered per call so it follows the UI language. With no
// language set the default must still produce text, never an empty error.
func TestNewNoProviderError_UsesDefaultLanguage(t *testing.T) {
	orig := model.Language
	model.Language = ""
	t.Cleanup(func() { model.Language = orig })

	err := newNoProviderError()
	if err == nil || err.Error() == "" {
		t.Fatalf("expected a localized message, got %v", err)
	}
	if !errors.Is(err, errNoProvider) {
		t.Fatal("the error must still match errNoProvider")
	}
}

func TestReadableImageHints_SkipsQuotesDirsAndNonImages(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")
	hints, fallback := readableImageHints([]model.FileEntry{
		{Kind: "quote", Text: "x"},
		{Path: dir, IsDir: true},
		{Path: filepath.Join(dir, "notes.txt")},
		{Path: png},
		{Path: png}, // duplicate
	}, dir, true, true)

	if len(hints) != 1 || hints[0] != png {
		t.Fatalf("expected exactly the one readable image, got %v", hints)
	}
	if len(fallback) != 0 {
		t.Fatalf("nothing should need inline bytes, got %v", fallback)
	}
}

func TestReadableImageHints_FallsBackWhenTheAgentCannotOpenThePath(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")

	t.Run("no read capability", func(t *testing.T) {
		hints, fallback := readableImageHints([]model.FileEntry{{Path: png}}, dir, false, false)
		if len(hints) != 0 || len(fallback) != 1 {
			t.Fatalf("expected the picture to go inline, got hints=%v fallback=%v", hints, fallback)
		}
	})

	t.Run("outside the confined working directory", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "b.png")
		hints, fallback := readableImageHints([]model.FileEntry{{Path: outside}}, dir, true, true)
		if len(hints) != 0 || len(fallback) != 1 {
			t.Fatalf("expected the picture to go inline, got hints=%v fallback=%v", hints, fallback)
		}
	})
}

func TestReadableImageHints_URLsStayAddresses(t *testing.T) {
	hints, fallback := readableImageHints([]model.FileEntry{
		{Kind: "url", URL: "https://x/a.png"},
		{Kind: "url", URL: "https://x/a.png"}, // duplicate
		{Kind: "url", URL: "https://x/page.html"},
	}, "", true, false)

	if len(hints) != 0 {
		t.Fatalf("a remote picture has no local file to read, got %v", hints)
	}
	if len(fallback) != 1 || fallback[0].URL != "https://x/a.png" {
		t.Fatalf("expected the image URL once, got %v", fallback)
	}
}

func TestTurnImageParts_URLAndOverflow(t *testing.T) {
	files := make([]model.FileEntry, 0, maxTurnImages+2)
	files = append(files, model.FileEntry{Kind: "url", URL: "https://x/page.html"})
	for range maxTurnImages + 1 {
		files = append(files, model.FileEntry{Kind: "url", URL: "https://x/" + uniqueURLSuffix(len(files)) + ".png"})
	}

	parts, notes := turnImageParts(files)
	if len(parts) != maxTurnImages {
		t.Fatalf("expected the cap to hold, got %d parts", len(parts))
	}
	if len(notes) != 1 {
		t.Fatalf("the overflow must be one note, got %v", notes)
	}
}

func TestTurnImageParts_MissingAndOversizedFilesAreReported(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone.png")
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, maxTurnImageBytes+1), 0o600); err != nil {
		t.Fatalf("write oversized image: %v", err)
	}

	parts, notes := turnImageParts([]model.FileEntry{{Path: missing}, {Path: big}})
	if len(parts) != 0 {
		t.Fatalf("neither picture can be sent, got %d parts", len(parts))
	}
	if len(notes) != 2 {
		t.Fatalf("expected one note per skipped picture, got %v", notes)
	}
}

func TestTurnImageParts_DuplicateLocalPathIsSentOnce(t *testing.T) {
	dir := t.TempDir()
	png := writeImage(t, dir, "a.png", 4)

	parts, _ := turnImageParts([]model.FileEntry{{Path: png}, {Path: png}})
	if len(parts) != 1 {
		t.Fatalf("a repeated path must not be sent twice, got %d parts", len(parts))
	}
}

func TestTurnImageParts_LocalOverflowIsReported(t *testing.T) {
	dir := t.TempDir()
	files := make([]model.FileEntry, 0, maxTurnImages+1)
	for i := range maxTurnImages + 1 {
		files = append(files, model.FileEntry{Path: writeImage(t, dir, uniqueURLSuffix(i)+".png", 4)})
	}

	parts, notes := turnImageParts(files)
	if len(parts) != maxTurnImages {
		t.Fatalf("expected the cap to hold, got %d parts", len(parts))
	}
	if len(notes) != 1 {
		t.Fatalf("the overflow must be one note, got %v", notes)
	}
}

func TestModelListStateForAgent_NilAgent(t *testing.T) {
	if got := modelListStateForAgent(nil); got != nil {
		t.Fatalf("a nil agent has no list, got %#v", got)
	}
}

func uniqueURLSuffix(i int) string {
	return string(rune('a' + i))
}
