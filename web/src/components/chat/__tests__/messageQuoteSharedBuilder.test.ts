import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Both ways of quoting a chat message must go through the one builder.
 *
 * The meta-bar "quote this message" button (ChatPanelContent) and selecting
 * text inside a message (useQuoteQuestion) are the same user-facing action, and
 * must produce the same kind of quote. They were written separately and HAD
 * drifted: the button's path omitted the session id entirely, which silently
 * made those quotes unopenable (a message id is only unique within a session).
 *
 * This is a source-level guard on purpose. A behavioural test cannot catch the
 * regression, because re-inlining a literal payload in either file still
 * produces a *working* quote for the cases that test happens to cover — the
 * failure mode is one path quietly diverging from the other. Only asserting
 * that both call the shared builder catches that.
 */
describe('chat message quote — shared builder', () => {
  const META_PATH = 'src/components/chat/ChatPanelContent.vue'
  const SELECTION_PATH = 'src/composables/useQuoteQuestion.ts'

  it('the meta-bar button builds its payload with buildMessageQuote', () => {
    const src = readWebFile(META_PATH)

    expect(src).toContain('buildMessageQuote(')
    expect(src).toMatch(/import[^\n]*buildMessageQuote[^\n]*from '@\/utils\/quoteItem/)
  })

  it('the selection path builds its payload with buildMessageQuote', () => {
    const src = readWebFile(SELECTION_PATH)

    expect(src).toContain('buildMessageQuote(')
    expect(src).toMatch(/import[^\n]*buildMessageQuote[^\n]*from '@\/utils\/quoteItem/)
  })

  // The specific shape of the old drift: a hand-rolled message quote literal.
  // If either path grows one back, the two can diverge again.
  it('neither path hand-rolls a message-quote payload', () => {
    for (const path of [META_PATH, SELECTION_PATH]) {
      const src = readWebFile(path)
      expect(src, `${path} must not inline a sourceKind:'message' payload`)
        .not.toContain("sourceKind: 'message'")
    }
  })
})

/**
 * The drawer's staged/sent distinction depends on the PARENT passing `mode`.
 *
 * The drawer itself defaults to 'staged' (editable), so dropping the binding at
 * the mount site does not break anything visibly — it silently makes every sent
 * quote editable again, which is exactly the behaviour the user asked to
 * remove. A behavioural test on the drawer cannot catch it (the drawer is
 * correct; the wiring is missing), so this is asserted at the source level.
 */
describe('quote detail drawer — mode wiring', () => {
  it('passes the quote mode from the controller to the drawer', () => {
    const src = readWebFile('src/components/chat/ChatPanelContent.vue')

    expect(src).toMatch(/<QuoteDetailDrawer[\s\S]*?:mode="quoteDetail\.mode\.value"/)
  })
})
