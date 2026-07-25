import { describe, expect, it } from 'vitest'
import { getAgentSvg } from '@/utils/agentIcons'

const ALL_BACKENDS = [
    'claude', 'codebuddy', 'opencode', 'codex', 'cline',
    'copilot', 'qoder', 'kimi', 'mimo', 'pi', 'deepseek', 'vecli',
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

        it('backends needing background have needsBg flag', () => {
            const needsBg = ['opencode', 'codex', 'mimo', 'pi']
            for (const id of needsBg) {
                expect(getAgentSvg(id)!.needsBg, `backend "${id}" should have needsBg=true`).toBe(true)
            }
        })

        it('backends with own background do not need needsBg', () => {
            const noNeedsBg = ['codebuddy', 'kimi', 'claude', 'cline', 'copilot', 'qoder', 'deepseek', 'vecli']
            for (const id of noNeedsBg) {
                const data = getAgentSvg(id)!
                expect(data.needsBg, `backend "${id}" should not have needsBg`).toBeFalsy()
            }
        })

        it('gradient SVGs contain url(#ai- references', () => {
            const gradientBackends = ['codebuddy', 'codex', 'copilot']
            for (const id of gradientBackends) {
                const data = getAgentSvg(id)!
                expect(data.svg, `backend "${id}" should reference gradient defs`).toContain('url(#ai-')
            }
        })
    })
})
