import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The meeting detail header's quote button must behave like the issue/PR detail
 * header's: open the shared quote bar in composer mode, let the user annotate,
 * and only put a card in the chat when they commit.
 *
 * It used to stage the quote on click and jump to the chat tab — which reads as
 * "attach this", with nowhere to say why. App.vue has no component test (its
 * only tests read it as source text), and the handler is a plain function inside
 * `<script setup>`, so the WIRING is what gets pinned here: which handler the
 * panel is bound to, and which dispatcher that handler uses. The composer itself
 * is covered by `useQuoteQuestion.test.ts`.
 */
describe('meeting quote composer wiring', () => {
  const app = readWebFile('src/App.vue')

  function functionBody(name: string): string {
    const start = app.indexOf(`function ${name}(`)
    expect(start, `${name} not found in App.vue`).toBeGreaterThan(-1)
    const body = app.slice(start)
    return body.slice(0, body.indexOf('\n}'))
  }

  it('binds the meeting panel to the composer handler', () => {
    // Binding the drag handler here is the whole defect: it stages immediately
    // and switches tabs, so the button skips the composer entirely.
    expect(app).toContain('@quote="handleMeetingQuoteButton"')
    expect(app).not.toContain('@quote="handleMeetingQuote"')
  })

  it('opens the shared composer instead of staging the card', () => {
    const body = functionBody('handleMeetingQuoteButton')
    // Same entry point as the forge / file-browser headers — the bar, its
    // destination row and the delivery path are all shared.
    expect(body).toContain('quoteQuestion.openComposer(')
    // A pre-built payload, because a meeting is addressed by token and its text
    // has to be fetched before the bar can describe it.
    expect(body).toContain('quote,')
    // Staging here would skip the annotation step and the picker/active-session
    // dispatch that every other entry point goes through.
    expect(body).not.toContain('addStagedQuote')
    expect(body).not.toContain('switchTab')
  })

  it('keeps the drag path staging directly', () => {
    // Dropping a row on the chat already means "put this in the conversation",
    // so the composer detour stays out of that path.
    const body = functionBody('handleMeetingQuote')
    expect(body).toContain('stageQuoteInChat(quote)')
    expect(body).not.toContain('openComposer')
    // And the helper really stages: it must reach addStagedQuote rather than
    // routing back into the composer under a different name.
    const stage = functionBody('stageQuoteInChat')
    expect(stage).toContain('addStagedQuote(quote)')
    expect(stage).not.toContain('openComposer')
  })
})
