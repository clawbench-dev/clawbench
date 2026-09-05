/**
 * Font configuration — system font-stack switching with a subset of bundled
 * open-source fonts.
 *
 * Users pick from:
 *   - bundled  → self-hosted open-source fonts (OFL, Latin subset). They work
 *                on every device because the woff2 is served by the app; the
 *                file is only downloaded once the family is used in rendered
 *                text (see web/src/assets/self-hosted-fonts.css).
 *   - system   → fonts built into common OSes (macOS / Windows / Linux /
 *                Android). Only effective when installed on the device;
 *                otherwise the browser falls back through the default stack.
 *
 * The effective stacks are exposed to CSS through two custom properties on
 * <html>: --font-ui (interface / prose text) and --font-mono (code, terminal,
 * file viewer, path chips …).
 */

export const DEFAULT_FONT_CHOICE = 'default'

/**
 * Default UI (sans) font stack — must match the one hard-coded in
 * web/css/base.css before variable-ization.
 */
export const DEFAULT_UI_STACK = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, 'Helvetica Neue', sans-serif"

/**
 * Default monospace font stack — mirrors the historical code font stack used
 * across the app (code blocks / file viewer / search bar …).
 */
export const DEFAULT_MONO_STACK = "'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace"

/**
 * Default terminal (xterm) font stack. xterm is canvas-rendered and reads a
 * concrete fontFamily — historically a JetBrains-first stack. Shared by
 * useTerminalTabs (new instances) and TerminalPanelContent (existing ones).
 */
export const DEFAULT_TERMINAL_MONO_STACK = "'JetBrains Mono', 'Fira Code', 'Cascadia Code', monospace"

export type FontKind = 'default' | 'bundled' | 'system' | 'custom'

export interface FontChoice {
  /** Stable storage value: 'default' or a font family name. */
  id: string
  /** UI grouping: bundled (self-hosted) / system (built-in).
   *  Group display order in the picker follows the candidate list order. */
  kind: FontKind
}

/** Well-known open-source monospace fonts self-hosted by the app (Latin). */
const BUNDLED_MONO: string[] = [
  'JetBrains Mono',
  'Fira Code',
  'Cascadia Code',
  'Source Code Pro',
  'IBM Plex Mono',
]

/** Monospace fonts built into common operating systems. */
const SYSTEM_MONO: string[] = [
  'Menlo',
  'Courier New',
  'DejaVu Sans Mono',
  'Consolas',
  'Segoe UI Mono',
  'Roboto Mono',
]

/** Well-known open-source UI (sans) fonts self-hosted by the app (Latin). */
const BUNDLED_UI: string[] = [
  'Inter',
  'Source Sans 3',
  'IBM Plex Sans',
]

// ── Custom (user-supplied) fonts ─────────────────────────────────────────
// Fonts dropped into the configured custom font directory (Settings →
// Appearance → 自定义字体目录). The list is populated at runtime by
// customFonts.loadCustomFonts() from GET /api/fonts/list and must NOT live in
// the static candidate tables above: those tables drive static i18n label
// completeness tests, while custom families use the file stem as their
// display label (value-string fallback) and vary per installation.
let customChoices: FontChoice[] = []

/** Replace the runtime custom-font candidate registry (called after each scan). */
export function setCustomFontChoices(choices: FontChoice[]): void {
  customChoices = choices
}

/** The current runtime custom-font candidates (empty before the first scan). */
export function getCustomFontChoices(): FontChoice[] {
  return customChoices
}

/** True when id is a currently-registered custom font family. */
export function isCustomFontId(id: string | undefined | null): boolean {
  if (!id) return false
  return customChoices.some(c => c.id === id)
}

/** CJK-capable system fonts (macOS / Windows / Linux), shared by the UI
 *  candidate list and the code-font fallback pool (Chinese comments). */
const SYSTEM_UI_CJK: string[] = [
  'PingFang SC',
  'Microsoft YaHei',
  'SimHei',           // 黑体
  'SimSun',           // 宋体
  'NSimSun',          // 新宋体
  'KaiTi',            // 楷体
  'FangSong',         // 仿宋
  'DengXian',         // 等线
  // 华文系列（macOS 随附）
  'STCaiyun',         // 华文彩云
  'STHeiti',          // 华文黑体
  'STFangsong',       // 华文仿宋
  'STHupo',           // 华文琥珀
  'STKaiti',          // 华文楷体
  'STLiti',           // 华文隶变
  'STSong',           // 华文宋体
  'STXihei',          // 华文细黑
  'STXingkai',        // 华文行楷
  'STXinwei',         // 华文新魏
  'STZhongsong',      // 华文中宋
  // Windows 中文字体
  'FZShuTi',          // 方正舒体
  'FZYaoTi',          // 方正姚体
  'LiSu',             // 隶书
  'YouYuan',          // 幼圆
  // Linux / Android 系统 CJK 字体（Android 无 SimSun/PingFang 等，只有 Noto）
  'Noto Sans CJK SC',     // Android/Linux 思源黑体系统名
  'Noto Serif CJK SC',   // 思源宋体
  'Noto Sans SC',
  'WenQuanYi Micro Hei', // 文泉驿微米黑
  'WenQuanYi Zen Hei',   // 文泉驿正黑
]

/** UI (sans / serif / CJK) fonts built into common operating systems. Font ids
 *  must stay dot-free (vue-i18n path resolution). */
const SYSTEM_UI: string[] = [
  ...SYSTEM_UI_CJK,
  'Arial',
  'Calibri',
  'Cambria',
  'Georgia',
  'Segoe UI',
  'Tahoma',
  'Times New Roman',
  'Trebuchet MS',
  'Verdana',
  'Roboto',
]

const toChoices = (kind: FontKind, ids: string[]): FontChoice[] => ids.map(id => ({ id, kind }))

export const MONO_FONT_CHOICES: FontChoice[] = [
  { id: DEFAULT_FONT_CHOICE, kind: 'default' },
  ...toChoices('bundled', BUNDLED_MONO),
  ...toChoices('system', SYSTEM_MONO),
]

/** Full UI candidate pool: every bundled (UI + monospace) and system (UI/CJK +
 *  monospace) font, so the interface font pickers (primary + fallback) can
 *  select any available family. */
export const UI_FONT_CHOICES: FontChoice[] = [
  { id: DEFAULT_FONT_CHOICE, kind: 'default' },
  ...toChoices('bundled', [...BUNDLED_UI, ...BUNDLED_MONO]),
  ...toChoices('system', [...SYSTEM_UI, ...SYSTEM_MONO]),
]

/**
 * Candidate pool for the CODE-FONT fallback picker. Primary code fonts are
 * Latin-only monospace faces; to keep Chinese comments readable, the fallback
 * additionally lists CJK system fonts (used glyph-by-glyph when the primary
 * lacks a codepoint, e.g. 中文注释).
 */
export const MONO_FALLBACK_CHOICES: FontChoice[] = [
  { id: DEFAULT_FONT_CHOICE, kind: 'default' },
  ...toChoices('bundled', BUNDLED_MONO),
  ...toChoices('system', SYSTEM_MONO),
  ...toChoices('system', SYSTEM_UI_CJK),
]

export const MONO_FONT_KEY = 'clawbench-settings-fontMono'
export const UI_FONT_KEY = 'clawbench-settings-fontUi'
export const MONO_FALLBACK_KEY = 'clawbench-settings-fontMonoFallback'
export const UI_FALLBACK_KEY = 'clawbench-settings-fontUiFallback'

function readChoice(key: string, storage: Pick<Storage, 'getItem'> = localStorage): string {
  try {
    const raw = storage.getItem(key)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (typeof parsed === 'string' && parsed) return parsed
    }
  } catch { /* ignore */ }
  return DEFAULT_FONT_CHOICE
}

export function readMonoFont(storage: Pick<Storage, 'getItem'> = localStorage): string {
  return readChoice(MONO_FONT_KEY, storage)
}

export function readUiFont(storage: Pick<Storage, 'getItem'> = localStorage): string {
  return readChoice(UI_FONT_KEY, storage)
}

export function readMonoFallbackFont(storage: Pick<Storage, 'getItem'> = localStorage): string {
  return readChoice(MONO_FALLBACK_KEY, storage)
}

export function readUiFallbackFont(storage: Pick<Storage, 'getItem'> = localStorage): string {
  return readChoice(UI_FALLBACK_KEY, storage)
}

/**
 * Escape a font family name for inclusion in a single-quoted CSS font-family /
 * @font-face family string: backslash then embedded single quote.
 * Names without those characters pass through unchanged.
 */
export function escapeCssFamilyName(name: string): string {
  return name.replace(/\\/g, '\\\\').replace(/'/g, "\\'")
}

/**
 * Quote a font family name for a CSS font stack when it needs it (whitespace,
 * embedded single quote or backslash). Simple identifiers pass through
 * unquoted. Embedded quotes/backslashes are CSS-escaped so arbitrary
 * custom-font stems (e.g. "Ace 'Round") cannot break the stack.
 */
export function quoteFamilyName(name: string): string {
  if (/[\s'\\]/.test(name) && !/^['"]/.test(name)) return `'${escapeCssFamilyName(name)}'`
  return name
}

/**
 * Build a full CSS font stack from an optional primary and an optional
 * fallback choice, both resolved on top of the default stack:
 *   - both 'default' (or empty)  → defaultStack unchanged
 *   - primary only               → `primary, defaultStack`
 *   - fallback only              → `fallback, defaultStack`
 *   - both present               → `primary, fallback, defaultStack`
 */
export function buildDualFontStack(
  main: string | undefined | null,
  fallback: string | undefined | null,
  defaultStack: string,
): string {
  const parts: string[] = []
  if (main && main !== DEFAULT_FONT_CHOICE) parts.push(quoteFamilyName(main))
  if (fallback && fallback !== DEFAULT_FONT_CHOICE && fallback !== main) parts.push(quoteFamilyName(fallback))
  if (parts.length === 0) return defaultStack
  return `${parts.join(', ')}, ${defaultStack}`
}

/**
 * Build a full CSS font stack for a single selected font choice.
 * 'default' returns the given default stack untouched; otherwise the chosen
 * family is inserted at the head so an installed font takes effect while an
 * uninstalled one falls back to the default stack.
 */
export function buildFontStack(choice: string | undefined | null, defaultStack: string): string {
  return buildDualFontStack(choice, undefined, defaultStack)
}

/** Resolve a stored choice against the candidate list; returns null for unknown ids. */
export function resolveChoice(choice: string | undefined | null, candidates: FontChoice[]): FontChoice | null {
  if (!choice) return null
  return candidates.find(c => c.id === choice) ?? null
}

/** Kind of a stored choice against a candidate list; 'default' when unknown. */
export function choiceKind(choice: string | undefined | null, candidates: FontChoice[]): FontKind {
  return resolveChoice(choice, candidates)?.kind ?? 'default'
}

/** True when the choice is a self-hosted bundled font (needs webfont loading). */
export function isBundledChoice(choice: string | undefined | null, candidates: FontChoice[]): boolean {
  return choiceKind(choice, candidates) === 'bundled'
}

/**
 * Trigger + wait for a font family's @font-face file to finish loading.
 * No-op when the Font Loading API is unavailable (jsdom / old engines) or the
 * face is not declared. Needed by canvas renderers (xterm) which measure
 * glyphs synchronously and do not re-measure when a webfont arrives late.
 */
export async function ensureFontLoaded(family: string): Promise<void> {
  try {
    const fonts = document.fonts
    if (!fonts?.load) return
    await Promise.all([
      fonts.load(`16px "${family}"`),
      fonts.load(`bold 16px "${family}"`),
    ])
  } catch { /* face undeclared / load failure → renderer keeps fallback stack */ }
}

/** Wait for every bundled family currently selected (mono + ui primary and
 *  their fallbacks) to load. */
export async function ensureSelectedBundledFontsLoaded(
  monoChoice: string = readMonoFont(),
  monoFallback: string = readMonoFallbackFont(),
  uiChoice: string = readUiFont(),
  uiFallback: string = readUiFallbackFont(),
): Promise<void> {
  await Promise.all([
    isBundledChoice(monoChoice, MONO_FONT_CHOICES) ? ensureFontLoaded(monoChoice) : undefined,
    isBundledChoice(monoFallback, MONO_FALLBACK_CHOICES) ? ensureFontLoaded(monoFallback) : undefined,
    isBundledChoice(uiChoice, UI_FONT_CHOICES) ? ensureFontLoaded(uiChoice) : undefined,
    isBundledChoice(uiFallback, UI_FONT_CHOICES) ? ensureFontLoaded(uiFallback) : undefined,
  ])
}

/** Set a --font-* custom property on <html> from a mono/ui choice (+fallback). */
export function applyFontToDocument(
  doc: Pick<Document, 'documentElement'>,
  prop: string,
  main: string,
  fallback: string,
  defaultStack: string,
): void {
  doc.documentElement.style.setProperty(prop, buildDualFontStack(main, fallback, defaultStack))
}

/** Apply mono/ui primary + fallback choices to <html> custom properties.
 *  Unknown ids (e.g. tampered localStorage) fall back to the default stack. */
export function applyFontConfig(
  doc: Pick<Document, 'documentElement'> = document,
  monoChoice: string = readMonoFont(),
  monoFallback: string = readMonoFallbackFont(),
  uiChoice: string = readUiFont(),
  uiFallback: string = readUiFallbackFont(),
): void {
  const monoValid = resolveChoice(monoChoice, MONO_FONT_CHOICES) !== null || isCustomFontId(monoChoice)
  const monoFbValid = resolveChoice(monoFallback, MONO_FALLBACK_CHOICES) !== null || isCustomFontId(monoFallback)
  const uiValid = resolveChoice(uiChoice, UI_FONT_CHOICES) !== null || isCustomFontId(uiChoice)
  const uiFbValid = resolveChoice(uiFallback, UI_FONT_CHOICES) !== null || isCustomFontId(uiFallback)
  applyFontToDocument(doc, '--font-mono', monoValid ? monoChoice : DEFAULT_FONT_CHOICE, monoFbValid ? monoFallback : DEFAULT_FONT_CHOICE, DEFAULT_MONO_STACK)
  applyFontToDocument(doc, '--font-ui', uiValid ? uiChoice : DEFAULT_FONT_CHOICE, uiFbValid ? uiFallback : DEFAULT_FONT_CHOICE, DEFAULT_UI_STACK)
}

// ── Availability detection ──────────────────────────────────────────────

/** Per-family availability cache shared across the two candidate lists. */
const availabilityCache = new Map<string, boolean>()

/** Reset the availability cache (mainly for tests). */
export function _resetAvailabilityCache(): void {
  availabilityCache.clear()
}

/** Font probe text — mixed widths/digits so two fonts rarely measure equal. */
const PROBE_TEXT = 'abcdefghijklmnopqrstuvwxyz0123456789il1I0O'

/** CJK probe text — catches Chinese-only faces (e.g. Noto Sans CJK) which do
 *  not contain Latin glyphs and would otherwise be measured as the fallback. */
const PROBE_CJK_TEXT = '中文注释测试一二三四五'

function probeFontExists(id: string): boolean {
  const canvas = document.createElement('canvas')
  const ctx = canvas.getContext?.('2d')
  if (!ctx) {
    // No canvas 2D context (jsdom / old engines): cannot probe → optimistic.
    return true
  }
  // A font "exists" when rendering probe text under `"<id>", sans-serif`
  // differs from plain sans-serif. If the family is unknown (or lacks the
  // glyphs, e.g. a Latin-only face measured with Chinese text), the browser
  // falls back to sans-serif → identical width.
  // Run BOTH a Latin probe (catches Latin-capable faces) and a CJK probe so
  // Chinese-only faces (e.g. Noto Sans CJK on Android, 文泉驿) are detected —
  // probing Latin-only text against a CJK-only family always matches the
  // fallback width and would falsely report the font as missing.
  if (measureDiffers(ctx, PROBE_TEXT, id)) return true
  return measureDiffers(ctx, PROBE_CJK_TEXT, id)
}

function measureDiffers(ctx: CanvasRenderingContext2D, text: string, id: string): boolean {
  ctx.font = '16px sans-serif'
  const base = ctx.measureText(text).width
  ctx.font = `16px "${id}", sans-serif`
  const candidate = ctx.measureText(text).width
  return Math.abs(candidate - base) > 0.01
}

/**
 * Whether a font family is actually usable on this device.
 * Bundled fonts are always available (self-hosted, app-served). System fonts
 * are probed with a canvas measureText trick (document.fonts.check() is NOT
 * used: it returns true for any unknown family name, so it cannot tell which
 * fonts a device has). When canvas 2D is unavailable (jsdom, old engines) we
 * optimistically keep the option — detection is a UX nicety, never a
 * correctness gate.
 */
export function isFontAvailable(id: string, kind: FontKind): boolean {
  if (kind === 'bundled' || kind === 'custom') return true
  const cached = availabilityCache.get(id)
  if (cached !== undefined) return cached
  let available = true
  try {
    available = probeFontExists(id)
  } catch { /* fall through → keep option */ }
  availabilityCache.set(id, available)
  return available
}

/** Preload every bundled font face (called when the picker opens so the
 *  self-rendered option previews use real glyphs). Safe to fire-and-forget. */
export async function preloadBundledFonts(candidates: FontChoice[]): Promise<void> {
  await Promise.all(
    candidates.filter(c => c.kind === 'bundled').map(c => ensureFontLoaded(c.id)),
  )
}

/**
 * Filter a candidate list down to fonts actually available on this device:
 *   - 'default' and 'bundled' are always kept;
 *   - 'system'/'open' are kept only when isFontAvailable() says so;
 *   - the currently selected id is always kept so the picker never shows an
 *     empty selection even if its font is (temporarily) undetectable.
 * Also fires bundled-font preloading so previews can render real glyphs.
 */
export async function filterAvailableFonts(
  candidates: FontChoice[],
  selectedId?: string | null,
): Promise<FontChoice[]> {
  void preloadBundledFonts(candidates)
  const result: FontChoice[] = []
  for (const c of candidates) {
    if (c.id === DEFAULT_FONT_CHOICE || c.kind === 'bundled' || c.kind === 'custom' || c.id === selectedId) {
      result.push(c)
    } else if (isFontAvailable(c.id, c.kind)) {
      result.push(c)
    }
  }
  return result
}
