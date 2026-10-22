import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Contract for the team member detail sheet.
 *
 * Two things are load-bearing and both fail silently:
 *
 *  1. WIDTH — the sheet carries the member's full instructions, which are
 *     long-form Markdown (headings, lists, tables, code fences). The default
 *     624px card wraps those badly, so the panel needs a wider
 *     `--modal-max-width`. That variable is read by modal-card.css on
 *     `.bs-panel.bs-wide-auto`, which BottomSheet renders inside
 *     `<Teleport to="body">` — the component's scope attribute never lands
 *     there, so the rule MUST live in a non-scoped block. Same trap as
 *     ToolDetailDrawer (see its own test).
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
  it('passes a panelClass to BottomSheet so the width rule can reach the panel', () => {
    const m = SRC.match(/<BottomSheet\b[^>]*>/)
    expect(m, 'the BottomSheet element must exist').not.toBeNull()
    expect(m![0]).toContain('team-member-sheet')
  })

  it('widens the card via --modal-max-width in a NON-SCOPED style block', () => {
    const unscoped = SRC.slice(SRC.indexOf('<style>'))
    expect(unscoped, 'the width rule must be non-scoped to reach the teleported panel')
      .toMatch(/\.team-member-sheet\s*\{[^}]*--modal-max-width:\s*\d+px/)
  })

  it('is wider than the default card, which is what "wider" means here', () => {
    const m = SRC.match(/\.team-member-sheet\s*\{[^}]*--modal-max-width:\s*(\d+)px/)
    expect(m).not.toBeNull()
    // modal-card.css defaults to 624px; anything at or below that changes nothing.
    expect(Number(m![1])).toBeGreaterThan(624)
  })

  it('keeps the width variable out of the scoped block, where it would never match', () => {
    const scoped = SRC.slice(SRC.indexOf('<style scoped>'), SRC.indexOf('<style>'))
    expect(scoped).not.toContain('--modal-max-width')
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
