package model

import (
	"path/filepath"
	"testing"
)

func TestIsThemeAllowedExt(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"photo.png", true},
		{"photo.PNG", true}, // case-insensitive
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"photo.gif", true},
		{"photo.webp", true},
		{"photo.svg", true},
		{"photo.bmp", false},
		{"photo.ico", false},
		{"photo.tiff", false},
		{"photo.avif", false},
		{"photo.pdf", false},
		{"noext", false},
	}
	for _, c := range cases {
		if got := IsThemeAllowedExt(c.name); got != c.want {
			t.Errorf("IsThemeAllowedExt(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDefaultThemeDir(t *testing.T) {
	orig := DataDir
	defer func() { DataDir = orig }()

	DataDir = ""
	if got := DefaultThemeDir(); got != "" {
		t.Errorf("DefaultThemeDir() with empty DataDir = %q, want empty", got)
	}

	DataDir = "/data/.clawbench"
	want := filepath.Join("/data/.clawbench", "theme")
	if got := DefaultThemeDir(); got != want {
		t.Errorf("DefaultThemeDir() = %q, want %q", got, want)
	}
}
