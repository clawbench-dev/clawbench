import { describe, expect, it } from 'vitest'

/**
 * Selector contract for Mermaid diagrams in chat bubbles (issue #570).
 *
 * The diagram used to be capped by the chat bubble's 200px thumbnail rule; a
 * 302×1182 flowchart then came out 47px wide and unreadable. Mermaid is now
 * exempted via `:has(> .mermaid)` on the figure, and the USER bubble's
 * shrink-to-fit `.msg-card` is stretched so the child's `width: 100%` has a
 * definite percentage base.
 *
 * jsdom cannot evaluate the resulting geometry, but it CAN evaluate selectors,
 * which is the part that silently breaks: the figure is NOT a direct child of
 * `.msg-card`. The real tree (ChatMessageItem → ContentBlocks) is
 *
 *   .chat-message.user
 *     > .msg-card
 *       > .msg-content-wrapper
 *         > .content-blocks
 *           > div                      ← the text block's v-html host
 *             > .image-block-wrapper
 *               > .mermaid
 *
 * A `:has(> .image-block-wrapper > .mermaid)` selector parses fine and matches
 * nothing at that depth, so the bug survives any source-text assertion. This
 * test builds the REAL tree and asserts the selectors used by media-block.css
 * match it.
 */

/** Mirrors the production nesting of a chat message. */
function buildMessage(role: 'user' | 'assistant'): HTMLElement {
    const msg = document.createElement('div')
    msg.className = `chat-message ${role}`
    msg.innerHTML = `
      <div class="msg-card">
        <div class="msg-content-wrapper">
          <div class="content-blocks">
            <div>
              <div class="image-block-wrapper">
                <div class="image-block-header">
                  <span class="image-block-header-actions">
                    <button class="image-block-view-btn"><svg viewBox="0 0 24 24"></svg></button>
                  </span>
                </div>
                <div class="mermaid"><svg viewBox="0 0 302 1182"></svg></div>
              </div>
            </div>
          </div>
        </div>
      </div>`
    return msg
}

describe('chat mermaid fill-width selectors', () => {
    it('matches the figure through the real (deep) message nesting', () => {
        const root = document.createElement('div')
        root.appendChild(buildMessage('assistant'))
        const figure = root.querySelector('.image-block-wrapper')!

        // The rule that exempts mermaid from the 200px thumbnail cap.
        expect(figure.matches('.chat-message .image-block-wrapper:has(> .mermaid)')).toBe(true)
    })

    it('does not match a non-diagram figure inside a message', () => {
        // The negative case must live INSIDE a `.chat-message`, otherwise it
        // passes for the trivial reason that no ancestor matches — and would
        // keep passing even if the `:has(> .mermaid)` discriminator were
        // deleted from the rule entirely.
        const root = document.createElement('div')
        const msg = buildMessage('assistant')
        // Swap the diagram for a raster image in the same position.
        msg.querySelector('.image-block-wrapper')!.innerHTML =
            '<span class="lightbox-img-wrap"><img class="lightbox-img" src="a.png"></span>'
        root.appendChild(msg)

        const figure = root.querySelector('.image-block-wrapper')!
        expect(figure.matches('.chat-message .image-block-wrapper')).toBe(true)
        expect(figure.matches('.chat-message .image-block-wrapper:has(> .mermaid)')).toBe(false)
    })

    it('matches the USER bubble card so its width:100% child has a definite base', () => {
        const root = document.createElement('div')
        root.appendChild(buildMessage('user'))
        const card = root.querySelector('.msg-card')!

        expect(card.matches('.chat-message.user .msg-card:has(.image-block-wrapper > .mermaid)')).toBe(true)
    })

    it('does not stretch an assistant card (it is already stretch-aligned)', () => {
        const root = document.createElement('div')
        root.appendChild(buildMessage('assistant'))
        const card = root.querySelector('.msg-card')!

        // The user-only rule must not reach the assistant row.
        expect(card.matches('.chat-message.user .msg-card:has(.image-block-wrapper > .mermaid)')).toBe(false)
    })

    it('does not stretch a user card that has no diagram', () => {
        const root = document.createElement('div')
        const msg = document.createElement('div')
        msg.className = 'chat-message user'
        msg.innerHTML = '<div class="msg-card"><div class="msg-content-wrapper">'
            + '<div class="content-blocks"><div><p>just text</p></div></div></div></div>'
        root.appendChild(msg)
        const card = root.querySelector('.msg-card')!

        expect(card.matches('.chat-message.user .msg-card:has(.image-block-wrapper > .mermaid)')).toBe(false)
    })
})
