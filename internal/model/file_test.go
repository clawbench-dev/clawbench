package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestIsSupportedFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"text file", "main.go", true},
		{"image file", "photo.png", true},
		{"audio file", "song.mp3", true},
		{"video file", "movie.mp4", true},
		{"office docx", "report.docx", true},
		{"office xlsx", "data.xlsx", true},
		{"office pptx", "slides.pptx", true},
		{"unsupported file", "data.bin", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.IsSupportedFile(tt.input)
			assert.Equal(t, tt.want, got, "IsSupportedFile(%q)", tt.input)
		})
	}
}

func TestDetectSubtype(t *testing.T) {
	t.Run("YAML with openapi key", func(t *testing.T) {
		got := model.DetectSubtype("openapi.yaml", "openapi: '3.0.0'\ninfo:\n  title: Test\npaths: {}")
		assert.Equal(t, model.SubtypeOpenAPI, got)
	})

	t.Run("YAML with swagger key", func(t *testing.T) {
		got := model.DetectSubtype("swagger.yaml", "swagger: '2.0'\ninfo:\n  title: Test\npaths: {}")
		assert.Equal(t, model.SubtypeOpenAPI, got)
	})

	t.Run("YAML without openapi/swagger", func(t *testing.T) {
		got := model.DetectSubtype("config.yaml", "foo: bar\nbaz: 123")
		assert.Equal(t, "", got)
	})

	t.Run("YML extension with openapi", func(t *testing.T) {
		got := model.DetectSubtype("api.yml", "openapi: '3.1.0'\ninfo:\n  title: Test\npaths: {}")
		assert.Equal(t, model.SubtypeOpenAPI, got)
	})

	t.Run("JSON with openapi key", func(t *testing.T) {
		got := model.DetectSubtype("openapi.json", `{"openapi":"3.0.0","info":{"title":"Test"},"paths":{}}`)
		assert.Equal(t, model.SubtypeOpenAPI, got)
	})

	t.Run("JSON with swagger key", func(t *testing.T) {
		got := model.DetectSubtype("swagger.json", `{"swagger":"2.0","info":{"title":"Test"},"paths":{}}`)
		assert.Equal(t, model.SubtypeOpenAPI, got)
	})

	t.Run("JSON without openapi/swagger", func(t *testing.T) {
		got := model.DetectSubtype("package.json", `{"name":"test","version":"1.0.0"}`)
		assert.Equal(t, "", got)
	})

	t.Run("JSONC extension with openapi", func(t *testing.T) {
		got := model.DetectSubtype("api.jsonc", `{"openapi":"3.0.0","info":{"title":"Test"}}`)
		assert.Equal(t, model.SubtypeOpenAPI, got)
	})

	t.Run("Non YAML/JSON file", func(t *testing.T) {
		got := model.DetectSubtype("main.go", "package main")
		assert.Equal(t, "", got)
	})

	t.Run("Malformed YAML", func(t *testing.T) {
		got := model.DetectSubtype("bad.yaml", "openapi: '3.0.0'\n  bad indent: here\n    broken")
		assert.Equal(t, "", got)
	})

	t.Run("Malformed JSON", func(t *testing.T) {
		got := model.DetectSubtype("bad.json", `{openapi: invalid}`)
		assert.Equal(t, "", got)
	})

	t.Run("Empty content", func(t *testing.T) {
		got := model.DetectSubtype("empty.yaml", "")
		assert.Equal(t, "", got)
	})

	t.Run("Content exceeds size limit", func(t *testing.T) {
		largeContent := strings.Repeat("x", 1<<20+1)
		got := model.DetectSubtype("openapi.yaml", largeContent)
		assert.Equal(t, "", got)
	})
}

func TestConvertSpecToJSON(t *testing.T) {
	t.Run("Valid YAML to JSON", func(t *testing.T) {
		yaml := "openapi: '3.0.0'\ninfo:\n  title: Test\n  version: '1.0'\npaths: {}"
		got := model.ConvertSpecToJSON(yaml)
		assert.NotEmpty(t, got)

		// Verify the result is valid JSON
		var parsed map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(got), &parsed))
		assert.Equal(t, "3.0.0", parsed["openapi"])
	})

	t.Run("YAML with non-string map keys", func(t *testing.T) {
		yaml := "openapi: '3.0.0'\ninfo:\n  title: Test\nx-custom:\n  1: one\n  2: two"
		got := model.ConvertSpecToJSON(yaml)
		assert.NotEmpty(t, got)

		var parsed map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(got), &parsed))
	})

	t.Run("Invalid YAML", func(t *testing.T) {
		got := model.ConvertSpecToJSON("not: valid\n  yaml: []\n broken")
		assert.Equal(t, "", got)
	})

	t.Run("Empty content", func(t *testing.T) {
		got := model.ConvertSpecToJSON("")
		assert.Equal(t, "", got)
	})
}

func TestIsTextFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Markdown
		{"markdown .md", "README.md", true},
		{"markdown .markdown", "CHANGELOG.markdown", true},

		// JSON
		{"json .json", "package.json", true},
		{"jsonc .jsonc", "settings.jsonc", true},
		{"json5 .json5", "data.json5", true},

		// YAML
		{"yaml .yaml", "config.yaml", true},
		{"yml .yml", "docker-compose.yml", true},

		// TOML
		{"toml", "Cargo.toml", true},

		// XML
		{"xml .xml", "pom.xml", true},
		{"plist .plist", "Info.plist", true},

		// Config
		{"ini", "app.ini", true},
		{"properties", "app.properties", true},
		{"conf", "nginx.conf", true},
		{"cfg", "app.cfg", true},

		// Go
		{"go", "main.go", true},
		{"mod", "go.mod", true},
		{"sum", "go.sum", true},

		// Python
		{"py", "script.py", true},
		{"pyi", "stub.pyi", true},

		// Rust
		{"rs", "main.rs", true},

		// JavaScript
		{"js", "index.js", true},
		{"mjs", "module.mjs", true},
		{"cjs", "common.cjs", true},

		// TypeScript
		{"ts", "app.ts", true},
		{"tsx", "component.tsx", true},
		{"mts", "module.mts", true},
		{"cts", "config.cts", true},

		// Java
		{"java", "Main.java", true},

		// C#
		{"cs", "Program.cs", true},

		// Ruby
		{"rb", "app.rb", true},

		// PHP
		{"php", "index.php", true},

		// Swift
		{"swift", "App.swift", true},

		// Kotlin
		{"kt", "Main.kt", true},
		{"kts", "build.kts", true},

		// Scala
		{"scala", "App.scala", true},

		// C/C++
		{"c", "main.c", true},
		{"h", "header.h", true},
		{"cpp", "app.cpp", true},
		{"hpp", "header.hpp", true},
		{"cc", "app.cc", true},
		{"cxx", "app.cxx", true},

		// Lua
		{"lua", "init.lua", true},

		// R
		{"r lowercase", "script.r", true},
		{"R uppercase", "script.R", true},

		// Perl
		{"pl", "script.pl", true},
		{"pm", "module.pm", true},

		// Shell
		{"sh", "run.sh", true},
		{"bash", "script.bash", true},
		{"zsh", "config.zsh", true},
		{"fish", "config.fish", true},
		{"ksh", "script.ksh", true},
		{"ash", "script.ash", true},

		// PowerShell
		{"ps1", "script.ps1", true},
		{"psm1", "module.psm1", true},

		// SQL
		{"sql", "query.sql", true},

		// GraphQL
		{"graphql", "schema.graphql", true},
		{"gql", "query.gql", true},

		// HTML
		{"html", "index.html", true},
		{"htm", "page.htm", true},
		{"xhtml", "page.xhtml", true},

		// CSS
		{"css", "style.css", true},
		{"scss", "style.scss", true},
		{"sass", "style.sass", true},
		{"less", "style.less", true},
		{"styl", "style.styl", true},

		// Vue / Svelte
		{"vue", "App.vue", true},
		{"svelte", "Component.svelte", true},

		// Docker
		{"dockerfile", "Docker.dockerfile", true},
		{"dockerignore", ".dockerignore", true},

		// Make
		{"makefile", "GNU.makefile", true},
		{"mak", "rules.mak", true},

		// Nginx
		{"nginx", "site.nginx", true},

		// Git
		{"gitignore", ".gitignore", true},
		{"gitattributes", ".gitattributes", true},
		{"gitconfig", ".gitconfig", true},

		// Editor
		{"editorconfig", ".editorconfig", true},

		// EditorConfig -> Ignore (next category)
		// Ignore
		{"ignore", ".ignore", true},

		// Text
		{"txt", "notes.txt", true},
		{"text", "notes.text", true},

		// Log
		{"log", "app.log", true},

		// Diff/Patch
		{"diff", "changes.diff", true},
		{"patch", "fix.patch", true},

		// CSV/TSV
		{"csv", "data.csv", true},
		{"tsv", "data.tsv", true},

		// TeX
		{"tex", "paper.tex", true},

		// Certificates
		{"pem", "cert.pem", true},
		{"crt", "cert.crt", true},
		{"key", "server.key", true},
		{"pub", "id_rsa.pub", true},

		// Regex
		{"regex", "pattern.regex", true},
		{"regexp", "pattern.regexp", true},

		// Case insensitivity
		{"case insensitive .GO", "main.GO", true},
		{"case insensitive .PY", "script.PY", true},
		{"case insensitive .MD", "README.MD", true},
		{"case insensitive .Js", "index.Js", true},
		{"case insensitive .HTML", "page.HTML", true},

		// Multiple dots
		{"multiple dots test.min.js", "test.min.js", true},
		{"multiple dots app.config.yaml", "app.config.yaml", true},
		{"multiple dots module.bundled.mjs", "module.bundled.mjs", true},

		// Just extension
		{"just extension .go", ".go", true},
		{"just extension .py", ".py", true},
		{"just extension .md", ".md", true},

		// Negative cases
		{"binary .bin", "data.bin", false},
		{"data .dat", "file.dat", false},
		{"exe", "program.exe", false},
		{"iso", "disk.iso", false},
		{"zip", "archive.zip", false},
		{"tar", "backup.tar", false},
		{"gz", "file.gz", false},
		{"7z", "archive.7z", false},
		{"rar", "archive.rar", false},
		{"dll", "library.dll", false},
		{"so", "library.so", false},
		{"dylib", "library.dylib", false},
		{"class", "Main.class", false},
		{"o", "main.o", false},
		{"woff", "font.woff", false},
		{"woff2", "font.woff2", false},
		{"ttf", "font.ttf", false},
		{"eot", "font.eot", false},
		{"mp3 not text", "song.mp3", false},
		{"png not text", "image.png", false},
		{"mp4 not text", "video.mp4", false},

		// Empty string
		{"empty string", "", false},

		// No extension
		{"no extension", "Makefile", false},
		{"no extension README", "README", false},

		// Dotfile without recognized extension
		{"dotfile .bashrc", ".bashrc", false},
		{"dotfile .profile", ".profile", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.IsTextFile(tt.input)
			assert.Equal(t, tt.want, got, "IsTextFile(%q)", tt.input)
		})
	}
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// All supported extensions
		{"png", "photo.png", true},
		{"jpg", "photo.jpg", true},
		{"jpeg", "photo.jpeg", true},
		{"gif", "animation.gif", true},
		{"webp", "photo.webp", true},
		{"svg", "logo.svg", true},
		{"bmp", "image.bmp", true},
		{"ico", "favicon.ico", true},
		{"tiff", "photo.tiff", true},
		{"tif", "photo.tif", true},
		{"avif", "photo.avif", true},
		{"pdf", "document.pdf", true},

		// Case insensitivity
		{"case insensitive .PNG", "photo.PNG", true},
		{"case insensitive .Jpg", "photo.Jpg", true},
		{"case insensitive .JPEG", "photo.JPEG", true},
		{"case insensitive .GIF", "photo.GIF", true},
		{"case insensitive .SVG", "logo.SVG", true},
		{"case insensitive .PDF", "doc.PDF", true},
		{"case insensitive .WebP", "photo.WebP", true},

		// Multiple dots
		{"multiple dots photo.large.png", "photo.large.png", true},
		{"multiple dots icon.dark.svg", "icon.dark.svg", true},

		// Just extension
		{"just extension .png", ".png", true},
		{"just extension .jpg", ".jpg", true},

		// Negative cases
		{"text file", "readme.txt", false},
		{"go file", "main.go", false},
		{"mp3 file", "song.mp3", false},
		{"mp4 file", "video.mp4", false},
		{"binary .bin", "data.bin", false},
		{"exe", "program.exe", false},
		{"zip", "archive.zip", false},

		// Empty string
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.IsImageFile(tt.input)
			assert.Equal(t, tt.want, got, "IsImageFile(%q)", tt.input)
		})
	}
}

func TestIsAudioFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// All supported extensions
		{"mp3", "song.mp3", true},
		{"wav", "audio.wav", true},
		{"ogg", "audio.ogg", true},
		{"m4a", "audio.m4a", true},
		{"aac", "audio.aac", true},
		{"flac", "audio.flac", true},
		{"wma", "audio.wma", true},
		{"opus", "audio.opus", true},

		// Case insensitivity
		{"case insensitive .MP3", "song.MP3", true},
		{"case insensitive .Wav", "song.Wav", true},
		{"case insensitive .FLAC", "song.FLAC", true},
		{"case insensitive .OGG", "song.OGG", true},
		{"case insensitive .Opus", "song.Opus", true},

		// Multiple dots
		{"multiple dots track.remastered.flac", "track.remastered.flac", true},
		{"multiple dots audio.hq.mp3", "audio.hq.mp3", true},

		// Just extension
		{"just extension .mp3", ".mp3", true},
		{"just extension .wav", ".wav", true},

		// Negative cases
		{"mp4 not audio", "video.mp4", false},
		{"png not audio", "image.png", false},
		{"go file", "main.go", false},
		{"txt file", "notes.txt", false},
		{"bin file", "data.bin", false},
		{"exe file", "program.exe", false},

		// Empty string
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.IsAudioFile(tt.input)
			assert.Equal(t, tt.want, got, "IsAudioFile(%q)", tt.input)
		})
	}
}

func TestIsVideoFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// All supported extensions
		{"mp4", "movie.mp4", true},
		{"mkv", "movie.mkv", true},
		{"avi", "movie.avi", true},
		{"mov", "movie.mov", true},
		{"webm", "movie.webm", true},
		{"flv", "movie.flv", true},
		{"wmv", "movie.wmv", true},
		{"m4v", "movie.m4v", true},
		{"3gp", "movie.3gp", true},
		{"m3u8", "playlist.m3u8", true},

		// Case insensitivity
		{"case insensitive .MP4", "movie.MP4", true},
		{"case insensitive .Mkv", "movie.Mkv", true},
		{"case insensitive .AVI", "movie.AVI", true},
		{"case insensitive .MOV", "movie.MOV", true},
		{"case insensitive .WEBM", "movie.WEBM", true},
		{"case insensitive .M3U8", "playlist.M3U8", true},

		// Multiple dots
		{"multiple dots movie.1080p.mp4", "movie.1080p.mp4", true},
		{"multiple dots clip.raw.webm", "clip.raw.webm", true},

		// Just extension
		{"just extension .mp4", ".mp4", true},
		{"just extension .mkv", ".mkv", true},

		// Negative cases - .ts is TypeScript, not video
		{"ts is text not video", "app.ts", false},
		{"mp3 not video", "song.mp3", false},
		{"png not video", "image.png", false},
		{"go file", "main.go", false},
		{"txt file", "notes.txt", false},
		{"bin file", "data.bin", false},
		{"exe file", "program.exe", false},

		// Empty string
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.IsVideoFile(tt.input)
			assert.Equal(t, tt.want, got, "IsVideoFile(%q)", tt.input)
		})
	}
}

func TestIsOfficeFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Supported extensions
		{"docx", "report.docx", true},
		{"xlsx", "data.xlsx", true},
		{"pptx", "slides.pptx", true},
		{"xls", "legacy.xls", true},

		// Case insensitivity
		{"case insensitive .DOCX", "REPORT.DOCX", true},
		{"case insensitive .XLSX", "DATA.XLSX", true},
		{"case insensitive .PPTX", "SLIDES.PPTX", true},

		// Multiple dots
		{"multiple dots report.final.docx", "report.final.docx", true},

		// Legacy formats NOT supported for preview
		{"doc not supported", "legacy.doc", false},
		{"ppt not supported", "legacy.ppt", false},

		// Negative cases
		{"pdf not office", "doc.pdf", false},
		{"txt not office", "notes.txt", false},
		{"bin not office", "data.bin", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.IsOfficeFile(tt.input)
			assert.Equal(t, tt.want, got, "IsOfficeFile(%q)", tt.input)
		})
	}
}
