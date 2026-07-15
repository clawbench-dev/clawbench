/**
 * Material Icon Theme integration.
 * Resolves file/folder paths to per-file-type SVG icon URLs
 * using the vscode-material-icon-theme package.
 */

import { generateManifest } from 'material-icon-theme'

// Build icon URL map from copied SVGs via Vite's import.meta.glob
const iconModules = import.meta.glob<string>('../assets/material-icons/*.svg', {
  eager: true,
  query: '?url',
  import: 'default',
})

// Map: iconName (without .svg) → Vite content-hashed URL
const iconUrlMap = new Map<string, string>()
for (const [path, url] of Object.entries(iconModules)) {
  // path is like "../assets/material-icons/go.svg"
  const fileName = path.split('/').pop()!
  const iconName = fileName.replace(/\.svg$/, '')
  iconUrlMap.set(iconName, url)
}

// Generate manifest once at module init
const manifest = generateManifest()

// Build lookup maps from manifest data
const extMap = new Map<string, string>()
const fileNameMap = new Map<string, string>()
const folderNameMap = new Map<string, string>()
const folderNameOpenMap = new Map<string, string>()

for (const [ext, iconName] of Object.entries(manifest.fileExtensions || {})) {
  extMap.set(ext.toLowerCase(), iconName)
}

for (const [name, iconName] of Object.entries(manifest.fileNames || {})) {
  fileNameMap.set(name.toLowerCase(), iconName)
}

for (const [name, iconName] of Object.entries(manifest.folderNames || {})) {
  folderNameMap.set(name.toLowerCase(), iconName)
}

for (const [name, iconName] of Object.entries(manifest.folderNamesExpanded || {})) {
  folderNameOpenMap.set(name.toLowerCase(), iconName)
}

/** Default file icon name */
const DEFAULT_FILE_ICON = manifest.file || 'file'
/** Default folder icon name */
const DEFAULT_FOLDER_ICON = manifest.folder || 'folder'
/** Default open folder icon name */
const DEFAULT_FOLDER_OPEN_ICON = manifest.folderExpanded || 'folder-open'

/**
 * Resolve the icon name for a file path.
 * Checks: exact file name → file extension → fallback.
 */
export function getFileIconName(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/')
  const baseName = parts[parts.length - 1]

  // Try exact file name match (case-insensitive)
  const nameHit = fileNameMap.get(baseName.toLowerCase())
  if (nameHit) return nameHit

  // Try file extension match
  const dotIndex = baseName.lastIndexOf('.')
  if (dotIndex > 0) {
    // Try full extension (e.g., "tar.gz")
    const fullExt = baseName.slice(dotIndex + 1).toLowerCase()
    const fullHit = extMap.get(fullExt)
    if (fullHit) return fullHit

    // Try double extension (e.g., ".tar.gz" → "gz" already tried, try "tar.gz")
    if (dotIndex > 0) {
      const prevDot = baseName.lastIndexOf('.', dotIndex - 1)
      if (prevDot > 0) {
        const doubleExt = baseName.slice(prevDot + 1).toLowerCase()
        const doubleHit = extMap.get(doubleExt)
        if (doubleHit) return doubleHit
      }
    }
  }

  return DEFAULT_FILE_ICON
}

/**
 * Resolve the icon name for a folder.
 * @param name Folder name (not path)
 * @param open Whether the folder is expanded
 */
export function getFolderIconName(name: string, open = false): string {
  const map = open ? folderNameOpenMap : folderNameMap
  const hit = map.get(name.toLowerCase())
  if (hit) return hit
  return open ? DEFAULT_FOLDER_OPEN_ICON : DEFAULT_FOLDER_ICON
}

/**
 * Get the Vite asset URL for an icon by name.
 * Returns undefined if the icon SVG is not available.
 */
export function getIconUrl(iconName: string): string | undefined {
  return iconUrlMap.get(iconName)
}

/**
 * Get the full URL for a file's icon.
 * Combines icon name resolution with URL lookup.
 * Falls back to the default file icon URL.
 */
export function getFileIconUrl(path: string): string {
  const iconName = getFileIconName(path)
  return getIconUrl(iconName) || getIconUrl(DEFAULT_FILE_ICON) || ''
}

/**
 * Get the full URL for a folder's icon.
 * Falls back to the default folder icon URL.
 */
export function getFolderIconUrl(name: string, open = false): string {
  const iconName = getFolderIconName(name, open)
  return getIconUrl(iconName) || getIconUrl(open ? DEFAULT_FOLDER_OPEN_ICON : DEFAULT_FOLDER_ICON) || ''
}
