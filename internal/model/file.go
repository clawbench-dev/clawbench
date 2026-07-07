package model

import "strings"

// IsSupportedFile returns true if the filename has a supported file extension
// (text, image, audio, or video).
func IsSupportedFile(name string) bool {
	return IsTextFile(name) || IsImageFile(name) || IsAudioFile(name) || IsVideoFile(name)
}

// IsTextFile returns true if the filename has a supported text file extension.
func IsTextFile(name string) bool {
	exts := []string{
		".md", ".markdown",
		".json", ".jsonc", ".json5",
		".yaml", ".yml",
		".toml",
		".xml", ".plist",
		".ini", ".properties", ".conf", ".cfg",
		".go", ".mod", ".sum",
		".py", ".pyi",
		".rs",
		".js", ".mjs", ".cjs",
		".ts", ".tsx", ".mts", ".cts",
		".java",
		".cs",
		".rb",
		".php",
		".swift",
		".kt", ".kts",
		".scala",
		".c", ".h", ".cpp", ".hpp", ".cc", ".cxx",
		".lua",
		".r", ".R",
		".pl", ".pm",
		".sh", ".bash", ".zsh", ".fish", ".ksh", ".ash",
		".ps1", ".psm1",
		".sql",
		".graphql", ".gql",
		".html", ".htm", ".xhtml",
		".css", ".scss", ".sass", ".less", ".styl",
		".vue", ".svelte",
		".dockerfile", ".dockerignore",
		".makefile", ".mak",
		".nginx",
		".gitignore", ".gitattributes", ".gitconfig",
		".editorconfig",
		".ignore",
		".txt", ".text",
		".log",
		".diff", ".patch",
		".csv", ".tsv",
		".tex",
		".pem", ".crt", ".key", ".pub",
		".regex", ".regexp",
	}
	lower := strings.ToLower(name)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsImageFile returns true if the filename has a supported image file extension.
func IsImageFile(name string) bool {
	lower := strings.ToLower(name)
	imageExts := []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".tiff", ".tif", ".avif", ".pdf",
	}
	for _, ext := range imageExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsAudioFile returns true if the filename has a supported audio file extension.
func IsAudioFile(name string) bool {
	lower := strings.ToLower(name)
	audioExts := []string{
		".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac", ".wma", ".opus",
	}
	for _, ext := range audioExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsVideoFile returns true if the filename has a supported video file extension.
func IsVideoFile(name string) bool {
	lower := strings.ToLower(name)
	videoExts := []string{
		".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv", ".wmv", ".m4v", ".3gp", ".m3u8",
	}
	for _, ext := range videoExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
