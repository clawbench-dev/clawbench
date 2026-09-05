package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultFontsDir(t *testing.T) {
	origDataDir := DataDir
	defer func() { DataDir = origDataDir }()

	DataDir = ""
	if got := DefaultFontsDir(); got != "" {
		t.Errorf("DefaultFontsDir() with empty DataDir = %q, want empty", got)
	}

	DataDir = "/data/.clawbench"
	if got := DefaultFontsDir(); got != "/data/.clawbench/fonts" {
		t.Errorf("DefaultFontsDir() = %q, want %q", got, "/data/.clawbench/fonts")
	}
}

func TestConfigResolveFontsDir(t *testing.T) {
	origDataDir := DataDir
	defer func() { DataDir = origDataDir }()
	DataDir = "/data/.clawbench"

	cfg := Config{}
	if got := cfg.ResolveFontsDir(); got != "/data/.clawbench/fonts" {
		t.Errorf("ResolveFontsDir() empty = %q, want default", got)
	}

	cfg.Fonts.Dir = "/custom/fonts"
	if got := cfg.ResolveFontsDir(); got != "/custom/fonts" {
		t.Errorf("ResolveFontsDir() configured = %q, want /custom/fonts", got)
	}
}

func TestIsFontFile(t *testing.T) {
	valid := []string{"a.ttf", "b.otf", "c.woff", "d.woff2", "e.eot", "A.TTF", "font.TTF", "My Font.woff2"}
	for _, name := range valid {
		if !IsFontFile(name) {
			t.Errorf("IsFontFile(%q) = false, want true", name)
		}
	}

	invalid := []string{"a.txt", "b.png", "noext", "dir/", "font.bak", "font.woff2.tmp"}
	for _, name := range invalid {
		if IsFontFile(name) {
			t.Errorf("IsFontFile(%q) = true, want false", name)
		}
	}
}

func TestListFontFiles_SortsAndFilters(t *testing.T) {
	dir := t.TempDir()
	writeFont := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fontdata"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeFont("Zeta.woff2")
	writeFont("Alpha.ttf")
	writeFont("beta.otf")
	writeFont("notes.txt")    // ignored — not a font
	writeFont(".hidden.woff") // ignored — dot-file
	// Nested directory must not be scanned recursively.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFont(filepath.Join("sub", "Nested.woff2"))

	files := ListFontFiles(dir)

	if len(files) != 3 {
		t.Fatalf("ListFontFiles returned %d files, want 3: %+v", len(files), files)
	}
	// Sorted by family (case-sensitive byte order: Alpha < beta < Zeta).
	got := []string{files[0].Family, files[1].Family, files[2].Family}
	want := []string{"Alpha", "Zeta", "beta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if files[0].Ext != ".ttf" || files[0].File != "Alpha.ttf" {
		t.Errorf("Alpha entry = %+v, want ttf", files[0])
	}
}

func TestListFontFiles_DedupesByStem(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Sarasa.woff2", "Sarasa.ttf", "Sarasa.otf", "Other.woff"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files := ListFontFiles(dir)

	if len(files) != 2 {
		t.Fatalf("ListFontFiles returned %d files, want 2 (deduped by stem): %+v", len(files), files)
	}
	// Best format wins: Sarasa → woff2, Other → woff.
	for _, f := range files {
		switch f.Family {
		case "Sarasa":
			if f.Ext != ".woff2" || f.File != "Sarasa.woff2" {
				t.Errorf("Sarasa dedupe = %+v, want woff2", f)
			}
		case "Other":
			if f.Ext != ".woff" {
				t.Errorf("Other = %+v, want woff", f)
			}
		default:
			t.Errorf("unexpected family %q", f.Family)
		}
	}
}

func TestListFontFiles_MissingDir(t *testing.T) {
	files := ListFontFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if files == nil || len(files) != 0 {
		t.Errorf("ListFontFiles missing dir = %v, want empty non-nil slice", files)
	}
}
