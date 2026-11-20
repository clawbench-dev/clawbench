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

  function colRightChatTag(src: string): string {
    const m = src.match(/<div class="col-right-chat"[^>]*>/)
    if (!m) throw new Error('col-right-chat opening tag not found')
    return m[0]
  }

  function chatPanelRowTag(src: string): string {
    const m = src.match(/<div class="chat-panel-row"[^>]*>/)
    if (!m) throw new Error('chat-panel-row opening tag not found')
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

  // The drag handlers must live on `.chat-col` — the ONLY element in this tree
  // whose box is the chat column alone.
  //
  // They were on `.col-right` first (lit the sidebar too), then briefly on
  // `.col-right-chat`. That move looked like a fix but changed nothing: both
  // wrappers are `flex: 1` rows that also contain <SessionSidebar>, so the
  // enter/leave range still covered the session list and the overlay stayed lit
  // while the pointer was over it. `.chat-col` closes before <SessionSidebar>
  // opens; nothing above it does.
  it('keeps the drag handlers off the wrappers that contain the session sidebar', () => {
    const src = readWebFile(APP)

    for (const [name, tag] of [
      ['col-right', colRightTag(src)],
      ['col-right-chat', colRightChatTag(src)],
      ['chat-panel-row', chatPanelRowTag(src)],
    ] as const) {
      for (const handler of ['onChatColDragEnter', 'onChatColDragOver', 'onChatColDragLeave', 'onChatColDrop']) {
        expect(tag, `${name} must NOT handle ${handler}`).not.toContain(handler)
      }
    }
  })

  it('handles the drags on the chat column only', () => {
    const tag = chatColTag(readWebFile(APP))

    for (const handler of ['onChatColDragEnter', 'onChatColDragOver', 'onChatColDragLeave', 'onChatColDrop']) {
      expect(tag, `chat-col must handle ${handler}`).toContain(handler)
    }
  })

  // Structural precondition for the guard above: if the chat column ever came to
  // CONTAIN the sidebar, its enter/leave range would silently swallow the
  // session list again and every assertion above would still pass.
  it('closes the chat column before the session sidebar starts', () => {
    const src = readWebFile(APP)
    const colOpen = src.indexOf('<div class="chat-col"')
    const sidebar = src.indexOf('<SessionSidebar', colOpen)

    expect(colOpen).toBeGreaterThan(-1)
    expect(sidebar, 'SessionSidebar must follow .chat-col').toBeGreaterThan(colOpen)
    // The chat column's own closing tag precedes the sidebar, so they are siblings.
    const between = src.slice(colOpen, sidebar)
    const opens = (between.match(/<div\b/g) ?? []).length
    const closes = (between.match(/<\/div>/g) ?? []).length
    expect(closes, 'chat-col must be closed before <SessionSidebar>').toBe(opens)
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
