package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Subtype constants for FileContent.Subtype
const (
	SubtypeOpenAPI = "openapi"
)

// maxSpecSniffSize is the maximum file content size (1MB) for subtype detection.
// Files larger than this are skipped to avoid expensive parsing.
const maxSpecSniffSize = 1 << 20

// IsSupportedFile returns true if the filename has a supported file extension
// (text, image, audio, video, or office document).
func IsSupportedFile(name string) bool {
	return IsTextFile(name) || IsImageFile(name) || IsAudioFile(name) || IsVideoFile(name) || IsOfficeFile(name)
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

// IsOfficeFile returns true if the filename has a supported office document extension.
func IsOfficeFile(name string) bool {
	lower := strings.ToLower(name)
	officeExts := []string{
		".docx", ".xlsx", ".pptx", ".xls",
	}
	for _, ext := range officeExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// DetectSubtype examines file content to determine its subtype (e.g., "openapi").
// Returns ("", "") for unrecognized or malformed content.
// Files larger than maxSpecSniffSize are skipped for performance.
func DetectSubtype(filename, content string) (subtype string) {
	if len(content) > maxSpecSniffSize {
		return ""
	}
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml"):
		return detectSubtypeYAML(content)
	case strings.HasSuffix(lower, ".json") || strings.HasSuffix(lower, ".jsonc") || strings.HasSuffix(lower, ".json5"):
		return detectSubtypeJSON(content)
	default:
		return ""
	}
}

// detectSubtypeYAML parses YAML content and checks for openapi/swagger top-level keys.
func detectSubtypeYAML(content string) string {
	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return ""
	}
	if _, ok := root["openapi"]; ok {
		return SubtypeOpenAPI
	}
	if _, ok := root["swagger"]; ok {
		return SubtypeOpenAPI
	}
	return ""
}

// detectSubtypeJSON parses JSON content and checks for openapi/swagger top-level keys.
func detectSubtypeJSON(content string) string {
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		return ""
	}
	if _, ok := root["openapi"]; ok {
		return SubtypeOpenAPI
	}
	if _, ok := root["swagger"]; ok {
		return SubtypeOpenAPI
	}
	return ""
}

// ConvertSpecToJSON converts YAML content to JSON for ReDoc consumption.
// Returns the JSON string or empty string on failure.
// Handles non-string map keys from YAML by recursive normalization.
func ConvertSpecToJSON(content string) string {
	if content == "" {
		return ""
	}
	var root interface{}
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return ""
	}
	if root == nil {
		return ""
	}
	normalized := normalizeYAMLTypes(root)
	jsonBytes, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

// normalizeYAMLTypes recursively converts map[interface{}]interface{} to
// map[string]interface{} so json.Marshal can handle it.
func normalizeYAMLTypes(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v := range val {
			// Non-string keys are formatted via fmt.Sprint for safety.
			key, ok := k.(string)
			if !ok {
				key = anyToString(k)
			}
			out[key] = normalizeYAMLTypes(v)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v := range val {
			out[k] = normalizeYAMLTypes(v)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, v := range val {
			out[i] = normalizeYAMLTypes(v)
		}
		return out
	default:
		return v
	}
}

// anyToString converts a value to string for non-string YAML map keys.
func anyToString(v interface{}) string {
	return fmt.Sprint(v)
}
