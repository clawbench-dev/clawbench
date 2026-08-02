import { describe, expect, it } from 'vitest'
import { getAgentSvg } from '@/utils/agentIcons'

const ALL_BACKENDS = [
    'claude', 'codebuddy', 'opencode', 'codex',
    'copilot', 'qoder', 'kimi', 'mimo', 'pi', 'deepseek', 'vecli', 'grok',
]

describe('agentIcons', () => {
    describe('getAgentSvg', () => {
        it('returns SVG data for all 12 backends', () => {
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
            const needsBg = ['opencode', 'mimo', 'pi', 'grok']
            for (const id of needsBg) {
                const data = getAgentSvg(id)!
                expect(data.needsBg, `backend "${id}" should have needsBg=true`).toBe(true)
            }
        })

        it('monochrome backends have monoCssClass for theme-aware color', () => {
            const monochrome = ['opencode', 'pi', 'mimo', 'grok']
            for (const id of monochrome) {
                const data = getAgentSvg(id)!
                expect(data.monoCssClass, `backend "${id}" should have monoCssClass`).toBeTruthy()
            }
        })

        it('color backends do not have monoCssClass', () => {
            const color = ['claude', 'codebuddy', 'copilot', 'qoder', 'kimi', 'deepseek']
            for (const id of color) {
                const data = getAgentSvg(id)!
                expect(data.monoCssClass, `backend "${id}" should not have monoCssClass`).toBeFalsy()
            }
        })

        it('backends with own background do not need needsBg', () => {
            const noNeedsBg = ['codebuddy', 'kimi', 'claude', 'copilot', 'qoder', 'deepseek', 'vecli']
            for (const id of noNeedsBg) {
                const data = getAgentSvg(id)!
                expect(data.needsBg, `backend "${id}" should not have needsBg`).toBeFalsy()
            }
        })

        it('gradient SVGs contain gradient URL references', () => {
            const gradientBackends = ['codebuddy', 'codex', 'copilot']
            for (const id of gradientBackends) {
                const data = getAgentSvg(id)!
                // Gradient IDs now come from lobe-icons npm package (lobe-icons-* prefix)
                // or from inline fallbacks (ai-* prefix)
                const hasGradient = data.svg.includes('url(#lobe-icons-') || data.svg.includes('url(#ai-')
                expect(hasGradient, `backend "${id}" should reference gradient defs`).toBe(true)
            }
        })
    })
})
