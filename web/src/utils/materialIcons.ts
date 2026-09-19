/**
 * Material Icon Theme integration.
 * Resolves file/folder paths to per-file-type SVG icon URLs
 * using the vscode-material-icon-theme package.
 *
 * Icons are served as static assets at /material-icons/<name>.svg
 * (copied from node_modules by the material-icons-copy Vite plugin) and
 * fetched lazily on first access, then cached.
 */

import { generateManifest } from 'material-icon-theme'
import { appLog } from '@/utils/appLog'

// Static asset base URL (absolute so it resolves regardless of the current
// page path). Icons live in .clawbench-web/material-icons/ (build output) and are
// copied there by vite.config.ts material-icons-copy plugin.
// import.meta.glob is intentionally NOT used: globbing 1250 SVGs into the JS
// module graph inflated rollup's peak build memory to ~3.4GB.
const ICON_BASE = '/material-icons/'

// Cache: iconName → resolved URL (populated on first access)
const iconUrlCache = new Map<string, string>()

// Pending loads: iconName → Promise<string | undefined> (dedup concurrent loads)
const iconUrlPending = new Map<string, Promise<string | undefined>>()

/**
 * Lookup maps built from the material-icon-theme manifest.
 *
 * Built LAZILY, not at module init. `generateManifest()` walks the whole
 * icon-theme dataset (1378 file extensions + 2131 file names + 1250 icon
 * definitions) and measured ~370ms — a synchronous main-thread long task.
 * This module is pulled in by a lazily-loaded chunk that also contains the
 * markdown annotators, so running it at module init blocked the main thread
 * for most of a second right after that chunk evaluated (Chrome trace:
 * 727ms `v8.evaluateModule`).
 *
 * Nothing here is needed until an icon is actually resolved, so we defer the
 * whole construction to the first call. `manifestMaps` is the memoized
 * result; the `??=` keeps it idempotent.
 */
interface ManifestMaps {
  extMap: Map<string, string>
  fileNameMap: Map<string, string>
  folderNameMap: Map<string, string>
  folderNameOpenMap: Map<string, string>
  defaultFileIcon: string
  defaultFolderIcon: string
  defaultFolderOpenIcon: string
}

let manifestMaps: ManifestMaps | null = null

function getManifestMaps(): ManifestMaps {
  if (manifestMaps) return manifestMaps

  const manifest = generateManifest()

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

  manifestMaps = {
    extMap,
    fileNameMap,
    folderNameMap,
    folderNameOpenMap,
    defaultFileIcon: manifest.file || 'file',
    defaultFolderIcon: manifest.folder || 'folder',
    defaultFolderOpenIcon: manifest.folderExpanded || 'folder-open',
  }
  return manifestMaps
}

/**
 * Resolve the icon name for a file path.
 * Checks: exact file name → file extension → fallback.
 */
export function getFileIconName(path: string): string {
  const { fileNameMap, extMap, defaultFileIcon } = getManifestMaps()
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

  return defaultFileIcon
}

/**
 * Resolve the icon name for a folder.
 * @param name Folder name (not path)
 * @param open Whether the folder is expanded
 */
export function getFolderIconName(name: string, open = false): string {
  const { folderNameMap, folderNameOpenMap, defaultFolderIcon, defaultFolderOpenIcon } = getManifestMaps()
  const map = open ? folderNameOpenMap : folderNameMap
  const hit = map.get(name.toLowerCase())
  if (hit) return hit
  return open ? defaultFolderOpenIcon : defaultFolderIcon
}

/**
 * Get the static asset URL for an icon by name (lazy-loaded and cached).
 * Returns undefined if the icon SVG is not available.
 */
export async function getIconUrl(iconName: string): Promise<string | undefined> {
  // Check cache first
  const cached = iconUrlCache.get(iconName)
  if (cached) return cached

  // Dedup concurrent loads
  const pending = iconUrlPending.get(iconName)
  if (pending) return pending

  const url = `${ICON_BASE}${iconName}.svg`

  const loadPromise = checkIconExists(url).then((ok) => {
    if (!ok) {
      iconUrlPending.delete(iconName)
      return undefined
    }
    iconUrlCache.set(iconName, url)
    iconUrlPending.delete(iconName)
    return url
  }).catch((err) => {
    iconUrlPending.delete(iconName)
    appLog.w('MaterialIcons', `Failed to check icon: ${iconName}`, err)
    return undefined
  })

  iconUrlPending.set(iconName, loadPromise)
  return loadPromise
}

/**
 * HEAD request to verify the icon asset exists before returning its URL.
 * Icons absent from the material-icon-theme package must not resolve to a
 * 404 <img> src; callers fall back to the default icon in that case.
 */
async function checkIconExists(url: string): Promise<boolean> {
  try {
    const res = await fetch(url, { method: 'HEAD' })
    return res.ok
  } catch {
    return false
  }
}

/**
 * Get the full URL for a file's icon (async).
 * Combines icon name resolution with URL lookup.
 * Falls back to the default file icon URL.
 */
export async function getFileIconUrl(path: string): Promise<string> {
  const { defaultFileIcon } = getManifestMaps()
  const iconName = getFileIconName(path)
  return (await getIconUrl(iconName)) || (await getIconUrl(defaultFileIcon)) || ''
}

/**
 * Get the full URL for a folder's icon (async).
 * Falls back to the default folder icon URL.
 */
export async function getFolderIconUrl(name: string, open = false): Promise<string> {
  const { defaultFolderIcon, defaultFolderOpenIcon } = getManifestMaps()
  const iconName = getFolderIconName(name, open)
  return (await getIconUrl(iconName)) || (await getIconUrl(open ? defaultFolderOpenIcon : defaultFolderIcon)) || ''
}
