/**
 * Shared canvas primitives for the animated wallpaper styles.
 *
 * These are style-agnostic: colour parsing, linear blends, luminance and value
 * noise. Every animated style needs them, so they live here rather than being
 * duplicated per style (the `test/ps3-wave` prototype shipped its own copies of
 * `noise2` / `parseHex` / `mix` / `rgba`, which is exactly what this module
 * removes).
 *
 * Style-specific shape math stays in its own module (`waveMath.ts` for the XMB
 * bands, `animatedWallpapers/silk.ts` for the silk-haze bands).
 *
 * Nothing in this module touches the DOM.
 */

/** A parsed RGB triple. */
export interface Rgb {
  r: number
  g: number
  b: number
}

/**
 * Fallback used when a theme CSS variable is missing or malformed.
 *
 * `--accent-color` / `--bg-primary` have no `:root` fallback in variables.css —
 * they only exist under `[data-theme="..."]`. An empty read would otherwise
 * become NaN, and a NaN colour makes the canvas silently keep its previous
 * fillStyle (no throw), i.e. a background that quietly never updates.
 */
export const FALLBACK_ACCENT: Rgb = { r: 254, g: 128, b: 25 } // gruvbox-dark accent
export const FALLBACK_BG: Rgb = { r: 40, g: 40, b: 40 } // gruvbox-dark bg-primary

export const WHITE: Rgb = { r: 255, g: 255, b: 255 }
export const BLACK: Rgb = { r: 0, g: 0, b: 0 }

/** Full turn in radians. */
export const TAU = Math.PI * 2

/**
 * Parse a CSS hex colour (`#rgb`, `#rrggbb`, with or without `#`) into RGB.
 *
 * Returns `null` — never NaN components — when the input is empty or malformed,
 * so callers can fall back to a known colour. Callers must not assume a valid
 * result: use `parseHexColorOr(value, fallback)` when a usable colour is
 * required.
 */
export function parseHexColor(value: string | null | undefined): Rgb | null {
  if (typeof value !== 'string') return null
  const hex = value.trim().replace('#', '')
  if (!/^[0-9a-fA-F]+$/.test(hex)) return null

  const full = hex.length === 3
    ? hex.split('').map((c) => c + c).join('')
    : hex
  if (full.length !== 6) return null

  const n = parseInt(full, 16)
  if (!Number.isFinite(n)) return null
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 }
}

/** Parse a hex colour, falling back when the input is missing or malformed. */
export function parseHexColorOr(value: string | null | undefined, fallback: Rgb): Rgb {
  return parseHexColor(value) ?? fallback
}

/** Parse the theme accent CSS variable, with the gruvbox fallback. */
export function parseAccentColor(value: string | null | undefined): Rgb {
  return parseHexColorOr(value, FALLBACK_ACCENT)
}

/** Parse the theme background CSS variable, with the gruvbox fallback. */
export function parseBackgroundColor(value: string | null | undefined): Rgb {
  return parseHexColorOr(value, FALLBACK_BG)
}

/**
 * Relative luminance (WCAG) of an RGB colour, 0..1.
 *
 * Used to decide which way to push a derived colour so it contrasts with what it
 * is drawn on, instead of assuming every theme is dark.
 */
export function luminance(c: Rgb): number {
  const f = (v: number) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b)
}

/** Linear blend: k=0 returns `a`, k=1 returns `b`. */
export function mixRgb(a: Rgb, b: Rgb, k: number): Rgb {
  return {
    r: Math.round(a.r + (b.r - a.r) * k),
    g: Math.round(a.g + (b.g - a.g) * k),
    b: Math.round(a.b + (b.b - a.b) * k),
  }
}

/** `rgba(...)` string for a canvas fillStyle. */
export function rgba(c: Rgb, alpha: number): string {
  return `rgba(${c.r}, ${c.g}, ${c.b}, ${alpha})`
}

/** Deterministic permutation table, so a style looks identical on every load. */
const PERM = new Uint8Array(512)
{
  const p = new Uint8Array(256)
  for (let i = 0; i < 256; i++) p[i] = i
  let s = 1337
  for (let i = 255; i > 0; i--) {
    s = (s * 1664525 + 1013904223) >>> 0
    const j = s % (i + 1)
    const tmp = p[i]; p[i] = p[j]; p[j] = tmp
  }
  for (let i = 0; i < 512; i++) PERM[i] = p[i & 255]
}

const INV255 = 1 / 255
function hash2(ix: number, iy: number): number {
  return PERM[(PERM[ix & 255] + (iy & 255)) & 255] * INV255
}

/** Quintic smoothing: continuous first and second derivative, so no kinks. */
function smoother(t: number): number {
  return t * t * t * (t * (t * 6 - 15) + 10)
}

/** 2D value noise in 0..1. */
export function noise2(x: number, y: number): number {
  const x0 = Math.floor(x), y0 = Math.floor(y)
  const fx = smoother(x - x0), fy = smoother(y - y0)
  const n00 = hash2(x0, y0), n10 = hash2(x0 + 1, y0)
  const n01 = hash2(x0, y0 + 1), n11 = hash2(x0 + 1, y0 + 1)
  const a = n00 + (n10 - n00) * fx
  const b = n01 + (n11 - n01) * fx
  return a + (b - a) * fy
}

/**
 * Deterministic PRNG for per-style scatter (star positions, filament jitter).
 *
 * Seeded explicitly so a style's layout is reproducible across loads — a random
 * starfield would reshuffle on every theme change or remount.
 */
export function makeRng(seed: number): () => number {
  let s = seed >>> 0
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0
    return s / 4294967296
  }
}
