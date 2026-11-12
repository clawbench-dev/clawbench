import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for the member panel's extra identity detail (underlying agent name +
 * model).
 *
 * The detail is shown ONLY when the CARD is wide enough; on a phone the member
 * name and the status pill must keep their room. jsdom has no CSS engine (no
 * layout, no container queries), so this is a source contract:
 *
 *   1. the panel declares itself a size container (`member-panel`),
 *   2. the slot is visible by default (wide card),
 *   3. a container query hides it on a narrow card,
 *   4. the slot stays in the DOM — visibility is CSS, not `v-if` — so a resized
 *      window brings the information back without a remount.
 *
 * The behavioural half (what the slot contains) lives in
 * GroupMemberPanel.test.ts.
 */
const src = readWebFile('src/components/chat/GroupMemberPanel.vue')

describe('GroupMemberPanel identity detail', () => {
  it('makes the panel a size container named member-panel', () => {
    expect(src).toMatch(/\.member-panel\s*\{[^}]*container-type:\s*inline-size/)
    expect(src).toMatch(/\.member-panel\s*\{[^}]*container-name:\s*member-panel/)
  })

  it('keeps the identity detail visible by default (wide card)', () => {
    const base = src.match(/\.member-row__meta\s*\{([^}]*)\}/)
    expect(base).not.toBeNull()
    expect(base![1]).toMatch(/display:\s*inline-flex/)
  })

  it('hides the identity detail on a narrow card, inside the container query', () => {
    const block = src.match(/@container member-panel \(max-width: 28rem\)\s*\{([\s\S]*?)\n\}/)
    expect(block).not.toBeNull()
    expect(block![1]).toMatch(/\.member-row__meta\s*\{[^}]*display:\s*none/)
  })

  it('keeps the detail in the DOM (CSS hides it, not v-if)', () => {
    expect(src).toMatch(/v-if="hasMeta\(m\)"\s+class="member-row__meta"/)
  })
})
