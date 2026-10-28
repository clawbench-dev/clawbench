import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Contract for the team member detail sheet.
 *
 * Two things are load-bearing and both fail silently:
 *
 *  1. WIDTH — the sheet carries the member's full instructions, which are
 *     long-form Markdown (headings, lists, tables, code fences). The narrow tier
 *     wraps those badly, so the panel asks for the shared `size="wide"` tier.
 *     It must NOT widen itself with a local `--modal-max-width` override: that
 *     is how four drawers ended up wider than everything else, and
 *     modalWidth.css.test.ts now rejects any such override repo-wide.
 *
 *  2. NO PROMPT FETCH — the roster's `description` is byte-for-byte the spawn
 *     frame's `prompt` (verified against a real `codebuddy --acp` process by
 *     internal/ai/codebuddy_team_description_probe_integration_test.go), and it
 *     survives an interruption while the fetched prompt does not. Re-adding a
 *     `/api/ai/chat/tool-call` call would reintroduce a blank sheet exactly when
 *     the user is recovering from a dropped session.
 *
 * jsdom has no layout engine, so the width half is a source contract.
 */

const SRC = readWebFile('src/components/chat/TeamMemberDetail.vue')

describe('TeamMemberDetail — wide desktop card', () => {
  it('asks BottomSheet for the wide tier', () => {
    const m = SRC.match(/<BottomSheet\b[^>]*>/)
    expect(m, 'the BottomSheet element must exist').not.toBeNull()
    expect(m![0]).toMatch(/\ssize="wide"/)
  })

  it('does not widen itself with a local --modal-max-width override', () => {
    expect(SRC).not.toContain('--modal-max-width')
  })
})

describe('TeamMemberDetail — role text comes from the roster', () => {
  it('does not fetch the member prompt', () => {
    // The prompt and the description are the same text; fetching it again is
    // pure downside (it goes blank after a reconnect).
    expect(SRC).not.toContain('/api/ai/chat/tool-call')
    expect(SRC).not.toContain('toolId')
    expect(SRC).not.toContain('tool-id')
  })

  it('renders the description as Markdown with path annotation', () => {
    // A plain <pre> dump would show the literal '#', and skipping
    // verifyFilePaths would leave the path chips dead.
    expect(SRC).toContain('renderMarkdown(')
    expect(SRC).toContain('verifyFilePaths')
    // The v-html target needs :deep() rules — its nodes carry no scope attribute.
    expect(SRC).toMatch(/:deep\(/)
  })
})

describe('TeamMemberDetail — section title alignment', () => {
  // The role section title used to start at the sheet's inline padding (2px)
  // while the fact-grid labels above it sit 8px further in (the grid's own
  // padding), so the title read as glued to the border. The Markdown body is
  // deliberately flush — long instructions want the full width — which is why
  // the TITLE needs its own indent rather than the body getting one.
  it('indents the section title to the LABEL TEXT, not to the grid box', () => {
    // Measured in Chromium: the grid BOX starts at 8px (its own padding) but the
    // label TEXT starts at 16px, because the body contributes --space-1 and the
    // grid contributes --space-4. Indenting the title by the grid padding alone
    // left it 7.5px left of the labels — the "off by one step" look. The title
    // therefore takes both steps, and must use `margin-inline`: padding on a
    // block-level element stays inside its box, so the text shifts but the box
    // (and its measured left edge) does not.
    const src = readWebFile('src/components/chat/TeamMemberDetail.vue')

    const title = /\.team-member-detail__section-title\s*\{([^}]*)\}/.exec(src)
    expect(title, '.team-member-detail__section-title must exist').not.toBeNull()
    expect(title![1]).toMatch(/padding-inline:\s*var\(--space-4\)/)

    const facts = /\.team-member-detail__facts\s*\{([^}]*)\}/.exec(src)
    expect(facts).not.toBeNull()
    expect(facts![1]).toMatch(/padding:\s*var\(--space-4\)/)
  })

  it('leaves the Markdown body flush, so long instructions keep the width', () => {
    const src = readWebFile('src/components/chat/TeamMemberDetail.vue')
    const md = /\.team-member-detail__markdown\s*\{([^}]*)\}/.exec(src)
    expect(md).not.toBeNull()
    // Its padding is the card's own inset; there must be no extra inline indent
    // that would narrow the text column.
    expect(md![1]).not.toMatch(/padding-inline|margin-inline/)
  })
})
