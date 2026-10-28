import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { getAgentSvg } from '@/utils/agentIcons'

const ALL_BACKENDS = [
    'claude', 'codebuddy', 'opencode', 'codex',
    'copilot', 'qoder', 'kimi', 'mimo', 'pi', 'deepseek', 'dsh', 'vecli', 'grok', 'antigravity', 'hermes', 'cursor',
    'commandcode', 'openclaw', 'qodercn', 'custom-acp',
]

describe('agentIcons', () => {
    describe('getAgentSvg', () => {
        it('returns SVG data for every known backend', () => {
            for (const id of ALL_BACKENDS) {
                const data = getAgentSvg(id)
                expect(data, `backend "${id}" should have SVG data`).not.toBeNull()
                expect(data!.svg.length, `backend "${id}" SVG should not be empty`).toBeGreaterThan(0)
                expect(data!.viewBox, `backend "${id}" should have viewBox`).toBeTruthy()
            }
        })

        it('returns null for unknown backends', () => {
            expect(getAgentSvg('nonexistent')).toBeNull()
            expect(getAgentSvg('')).toBeNull()
        })

        it('all SVG data contains path or rect elements', () => {
            for (const id of ALL_BACKENDS) {
                const data = getAgentSvg(id)!
                expect(
                    data.svg.includes('<path') || data.svg.includes('<rect'),
                    `backend "${id}" SVG should contain path or rect elements`,
                ).toBe(true)
            }
        })

        it('backends needing background have needsBg flag (background via CSS --bg-tertiary)', () => {
            const needsBg = ['opencode', 'mimo', 'pi', 'grok', 'hermes', 'cursor', 'commandcode']
            for (const id of needsBg) {
                const data = getAgentSvg(id)!
                expect(data.needsBg, `backend "${id}" should have needsBg=true`).toBe(true)
            }
        })

        it('monochrome backends have monoCssClass for theme-aware color', () => {
            const monochrome = ['opencode', 'pi', 'mimo', 'grok', 'hermes', 'cursor', 'commandcode', 'kimi', 'custom-acp']
            for (const id of monochrome) {
                const data = getAgentSvg(id)!
                expect(data.monoCssClass, `backend "${id}" should have monoCssClass`).toBeTruthy()
            }
        })

        it('color backends do not have monoCssClass', () => {
            const color = ['claude', 'codebuddy', 'copilot', 'qoder', 'qodercn', 'deepseek', 'dsh', 'antigravity', 'openclaw']
            for (const id of color) {
                const data = getAgentSvg(id)!
                expect(data.monoCssClass, `backend "${id}" should not have monoCssClass`).toBeFalsy()
            }
        })

        it('backends with own background do not need needsBg', () => {
            const noNeedsBg = ['codebuddy', 'claude', 'copilot', 'qoder', 'qodercn', 'deepseek', 'dsh', 'vecli', 'antigravity', 'openclaw']
            for (const id of noNeedsBg) {
                const data = getAgentSvg(id)!
                expect(data.needsBg, `backend "${id}" should not have needsBg`).toBeFalsy()
            }
        })

        it('gradient SVGs contain gradient URL references', () => {
            const gradientBackends = ['codebuddy', 'codex', 'copilot', 'openclaw']
            for (const id of gradientBackends) {
                const data = getAgentSvg(id)!
                // Gradient IDs now come from lobe-icons npm package (lobe-icons-* prefix)
                // or from inline fallbacks (ai-* prefix)
                const hasGradient = data.svg.includes('url(#lobe-icons-') || data.svg.includes('url(#ai-')
                expect(hasGradient, `backend "${id}" should reference gradient defs`).toBe(true)
            }
        })

        // Guards the class of bug that hit OpenClaw and Kimi: a mark painted in a
        // single flat colour that matches (or nearly matches) the surface behind
        // it. Neither theme is safe by default — white ink disappears on light
        // backgrounds, near-black ink disappears on dark ones — so every icon
        // must either carry multiple distinct colours, a gradient, a mono class
        // (which flips with the theme), or an explicit contrasting backdrop.
        it('no icon paints every fill the same colour as a theme surface', () => {
            const LIGHT = '#ffffff'
            const DARK = '#0d1117'
            /** Channel-wise max distance between two #rgb/#rrggbb colours. */
            const rgb = (hex: string): [number, number, number] | null => {
                const m = hex.trim().match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i)
                if (!m) return null
                const raw = m[1].length === 3 ? m[1].split('').map(c => c + c).join('') : m[1]
                return [0, 2, 4].map(i => parseInt(raw.slice(i, i + 2), 16)) as [number, number, number]
            }
            const distance = (a: [number, number, number], b: [number, number, number]) =>
                Math.max(...a.map((v, i) => Math.abs(v - b[i])))
            // Below this the ink is effectively the background colour; saturated
            // brand colours (Kimi blue, Qoder green) sit far above it even when
            // their WCAG luminance ratio is modest.
            const INVISIBLE = 48
            const LIGHT_RGB = rgb(LIGHT)!
            const DARK_RGB = rgb(DARK)!

            for (const id of ALL_BACKENDS) {
                const data = getAgentSvg(id)!
                // A theme-aware mono colour or a backdrop already guarantees
                // separation from the surface; only bare colour icons can fail.
                if (data.monoCssClass || data.needsBg) continue
                // Gradient-filled marks supply their own colour range (the flat
                // literals in those files are white accents painted ON the
                // gradient, e.g. CodeBuddy's face), so a flat-fill scan would
                // misread them as white-only.
                if (/url\(#/.test(data.svg)) continue

                const fills = [...data.svg.matchAll(/\bfill="(#[0-9a-fA-F]{3,6})"/g)]
                    .map(m => rgb(m[1]))
                    .filter((c): c is [number, number, number] => c !== null)
                expect(fills.length, `backend "${id}" should have at least one literal fill colour`).toBeGreaterThan(0)

                const invisibleOnLight = fills.every(c => distance(c, LIGHT_RGB) < INVISIBLE)
                const invisibleOnDark = fills.every(c => distance(c, DARK_RGB) < INVISIBLE)
                expect(
                    !invisibleOnLight && !invisibleOnDark,
                    `backend "${id}" has every fill within ${INVISIBLE} of the `
                    + `${invisibleOnLight ? 'light' : 'dark'} surface colour — the mark would be invisible. `
                    + `Give it a monoCssClass or needsBg.`,
                ).toBe(true)
            }
        })

        // Regression: `custom-acp` had no entry, so every user-defined ACP agent
        // (issue #557 — they all share this one backend id) fell through to the
        // initial-letter fallback. A letter reads as a missing icon, not as
        // "your own agent", and it looked especially broken in lists where every
        // other row had a brand mark.
        it('resolves a generic mark for user-defined ACP agents', () => {
            const data = getAgentSvg('custom-acp')
            expect(data, 'custom-acp must not fall back to an initial letter').not.toBeNull()
            expect(data!.viewBox).toBe('0 0 24 24')
            // Monochrome + theme-aware: there is no brand colour to hard-code for
            // an arbitrary user agent, and a fixed fill would break on some of the
            // 36 themes.
            expect(data!.monoCssClass).toBe('mono-custom-acp')
            expect(data!.svg).toContain('currentColor')
        })

        // The specific regression: lobe-icons' `-color` variants sometimes paint
        // the glyph pure white assuming a brand-coloured tile that is NOT part of
        // the SVG. Rendering such a file on a plain background leaves nothing but
        // a stray dot visible, so these backends must use the mono variant.
        it('kimi uses the mono mark, not the white-on-white color variant', () => {
            const svg = getAgentSvg('kimi')!.svg
            const literalFills = [...svg.matchAll(/\bfill="(#[0-9a-fA-F]{3,6})"/g)].map(m => m[1].toLowerCase())
            expect(literalFills).not.toContain('#fff')
            expect(literalFills).not.toContain('#ffffff')
            expect(getAgentSvg('kimi')!.monoCssClass).toBe('mono-kimi')
        })

        // Every monoCssClass referenced by the map must have a rule in the shared
        // stylesheet — a typo silently falls back to inherited `color` and the
        // icon turns into whatever the surrounding text colour is.
        it('every monoCssClass has a colour rule in mono-icon-colors.css', () => {
            const css = readFileSync(resolve(__dirname, '../../assets/mono-icon-colors.css'), 'utf8')
            const defined = new Set([...css.matchAll(/^\.([\w-]+)\s*\{/gm)].map(m => m[1]))
            for (const id of ALL_BACKENDS) {
                const cls = getAgentSvg(id)?.monoCssClass
                if (!cls) continue
                expect(defined.has(cls), `backend "${id}" uses .${cls} but no rule defines it`).toBe(true)
            }
        })
    })
})
