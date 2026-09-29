import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the drag-and-drop highlight must cover the CHAT, not the whole pane.
 *
 * `.col-right` contains both the chat column and the session sidebar. The
 * highlight class was originally bound there, so dragging anything onto the pane
 * lit up the sidebar too and read as "the whole pane is the target" — the user
 * reported exactly that.
 *
 * App.vue has no mount test (it is the entire application), and the class is a
 * template binding rather than a literal in the bundle, so this is asserted at
 * the source level.
 */
describe('drag drop highlight scope', () => {
  const APP = 'src/App.vue'

  function chatColTag(src: string): string {
    const m = src.match(/<div class="chat-col"[^>]*>/)
    if (!m) throw new Error('chat-col opening tag not found')
    return m[0]
  }

  function colRightTag(src: string): string {
    const m = src.match(/<div class="col-right"[^>]*>/)
    if (!m) throw new Error('col-right opening tag not found')
    return m[0]
  }

  it('binds the highlight to the chat column', () => {
    const src = readWebFile(APP)

    expect(chatColTag(src)).toContain(':class="{ \'chat-drop-active\': chatDropActive }"')
  })

  it('does NOT bind the highlight to the pane that contains the session sidebar', () => {
    const src = readWebFile(APP)

    // Regression guard for the reported bug: this is where it used to live.
    expect(colRightTag(src)).not.toContain('chat-drop-active')
  })

  // The drag events stay on .col-right on purpose: dropping anywhere on the pane
  // (including over the sidebar) must still work. The highlight previews the
  // DESTINATION, not the cursor's location.
  it('keeps the drag handlers on the whole right pane', () => {
    const tag = colRightTag(readWebFile(APP))

    for (const handler of ['onChatColDragEnter', 'onChatColDragOver', 'onChatColDragLeave', 'onChatColDrop']) {
      expect(tag, `col-right must still handle ${handler}`).toContain(handler)
    }
  })

  // The pill is absolutely centred; centring it on .chat-panel-row (which spans
  // the sidebar too) would place it off the chat.
  it('centres the drop hint inside the chat column', () => {
    const src = readWebFile(APP)
    const colOpen = src.indexOf('<div class="chat-col"')
    const hint = src.indexOf('class="chat-drop-hint"')
    const colClose = src.indexOf('</div>', src.indexOf('class="chat-drop-hint"'))

    expect(hint, 'drop hint must exist').toBeGreaterThan(-1)
    expect(hint, 'drop hint must be inside .chat-col').toBeGreaterThan(colOpen)
    // …and before the sidebar, which is the next sibling of .chat-col.
    const sidebar = src.indexOf('<SessionSidebar', colOpen)
    expect(hint).toBeLessThan(sidebar)
    expect(colClose).toBeGreaterThan(hint)
  })
})
