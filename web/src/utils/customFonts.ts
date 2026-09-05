/**
 * Custom font directory — runtime @font-face loader for user-supplied fonts.
 *
 * Font files dropped into the configured server directory (Settings →
 * Appearance → 自定义字体目录; default <data dir>/fonts) are scanned by the
 * backend. This module:
 *   1. fetches the scanned list via GET /api/fonts/list,
 *   2. injects a <style id="clawbench-custom-fonts"> declaring an @font-face
 *      per file (family = file stem, src = auth-protected font endpoint),
 *   3. registers the families in the fontConfig runtime registry so the
 *      four font pickers gain a "自定义字体 / Custom fonts" group,
 *   4. re-applies the font config + notifies canvas/JS consumers so a stored
 *      custom selection takes effect once its face is available.
 */

import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import {
  applyFontConfig,
  setCustomFontChoices,
  escapeCssFamilyName,
  readMonoFont,
  readMonoFallbackFont,
  readUiFont,
  readUiFallbackFont,
  type FontChoice,
} from '@/utils/fontConfig'

/** Backend font extension → CSS format() token for @font-face src. */
const FONT_FORMATS: Record<string, string> = {
  '.woff2': 'woff2',
  '.woff': 'woff',
  '.ttf': 'truetype',
  '.otf': 'opentype',
  '.eot': 'embedded-opentype',
}

/** <style> element id — the loader owns and replaces this node each scan. */
const STYLE_ID = 'clawbench-custom-fonts'

/** One font file as returned by the backend list endpoint. */
export interface CustomFontInfo {
  family: string
  file: string
  ext: string
  size: number
  mod_time: string
}

export interface CustomFontState {
  /** Resolved server-side font directory (shown in the settings description). */
  dir: string
  fonts: CustomFontInfo[]
  /** Whether the last list fetch succeeded (sticky per session). */
  loaded: boolean
}

const state: CustomFontState = { dir: '', fonts: [], loaded: false }

/** Currently known custom fonts (empty until the first successful scan). */
export function getCustomFonts(): CustomFontState {
  return state
}

/** Reset the module state (mainly for tests). */
export function _resetCustomFonts(): void {
  state.dir = ''
  state.fonts = []
  state.loaded = false
  setCustomFontChoices([])
  document.getElementById(STYLE_ID)?.remove()
}

/**
 * Fetch the custom font list and refresh injected @font-face + the fontConfig
 * registry. Safe to call repeatedly (idempotent): re-scans happen on entering
 * the Appearance settings page and whenever the configured directory changes.
 * Failures are silent — an unreadable/empty directory simply yields no custom
 * fonts and the pickers keep their static candidates.
 */
export async function loadCustomFonts(): Promise<void> {
  let data: { dir: string; fonts: CustomFontInfo[] }
  try {
    data = await apiGet<{ dir: string; fonts: CustomFontInfo[] }>('/api/fonts/list')
  } catch (err) {
    appLog.w('CustomFonts', 'failed to list custom fonts', err)
    return
  }

  state.dir = data.dir
  state.fonts = Array.isArray(data.fonts) ? data.fonts : []
  state.loaded = true

  injectFontFaces(state.fonts)
  setCustomFontChoices(toChoices(state.fonts))
  // Re-apply so a stored custom selection (validated against the now-populated
  // registry) takes effect. @font-face files are lazy — the browser downloads
  // them once the family is actually used. Wait for the currently-selected
  // custom face (if any) BEFORE notifying canvas/JS consumers (xterm, mermaid),
  // which measure glyphs synchronously and never re-measure a late webfont.
  applyFontConfig(document)
  await preloadSelectedCustomFonts()
  window.dispatchEvent(new CustomEvent('clawbench-font-change'))
}

function toChoices(fonts: CustomFontInfo[]): FontChoice[] {
  return fonts.map(f => ({ id: f.family, kind: 'custom' as const }))
}

function injectFontFaces(fonts: CustomFontInfo[]): void {
  let style = document.getElementById(STYLE_ID) as HTMLStyleElement | null
  if (!style) {
    style = document.createElement('style')
    style.id = STYLE_ID
    document.head.appendChild(style)
  }
  const css = fonts.map(fontFaceCss).join('\n')
  style.textContent = css
}

function fontFaceCss(f: CustomFontInfo): string {
  const fmt = FONT_FORMATS[f.ext.toLowerCase()] ?? 'truetype'
  const url = `/api/fonts/file?name=${encodeURIComponent(f.file)}`
  return `@font-face{font-family:'${escapeCssFamilyName(f.family)}';src:url('${url}') format('${fmt}');font-display:swap;}`
}

async function preloadSelectedCustomFonts(): Promise<void> {
  const families = [readMonoFont(), readMonoFallbackFont(), readUiFont(), readUiFallbackFont()]
  const selected = families.filter(f => isCustomSelection(f))
  if (selected.length === 0) return
  try {
    const fonts = document.fonts
    if (!fonts?.load) return
    await Promise.all(selected.map(f => fonts.load(`16px '${escapeCssFamilyName(f)}'`)))
  } catch { /* face load failure → renderers keep the fallback stack */ }
}

function isCustomSelection(id: string | undefined | null): boolean {
  if (!id || id === 'default') return false
  return state.fonts.some(f => f.family === id)
}
