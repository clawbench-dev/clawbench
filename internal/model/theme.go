package model

import (
	"path/filepath"
	"strings"
)

// themeAllowedExts is the whitelist of wallpaper formats the theme-background
// handler accepts. A deliberate subset of image formats: png/jpeg (raster,
// service-side re-encoded/resized), gif/webp (raster, stored verbatim) and svg
// (vector, stored verbatim). bmp/ico/tiff/avif are excluded — rarely used as
// wallpapers and/or poorly supported as CSS background sources.
var themeAllowedExts = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
}

// IsThemeAllowedExt reports whether name has an extension acceptable as a
// custom wallpaper source (case-insensitive).
func IsThemeAllowedExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return themeAllowedExts[ext]
}

// DefaultThemeDir returns the default custom wallpaper directory:
// <DataDir>/theme. Returns an empty string when DataDir is unset (not yet
// resolved).
func DefaultThemeDir() string {
	if DataDir == "" {
		return ""
	}
	return filepath.Join(DataDir, "theme")
}
