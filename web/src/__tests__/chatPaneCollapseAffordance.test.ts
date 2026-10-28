import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the two wide-screen panes are collapsed from the split divider — one
 * hover handle on each side of the line — and the chat pane comes back from a
 * 24px rail left at the right edge.
 *
 * Evolution of the return path, so nobody re-litigates it by accident:
 *  1. A chat toggle pinned to the bottom of the vertical dock. Rejected: it was
 *     the sixth identical pill in the rail and read as just another tab.
 *  2. A bottom-right floating button. Rejected: it left no trace of the pane's
 *     former position, so a collapsed chat looked like a missing feature.
 *  3. A 24px rail on the right edge (current). It keeps the boundary the user
 *     just collapsed, so the pane reads as "pushed aside", and it has room for
 *     the unread count while the conversation is out of sight.
 *
 * App.vue has no mount test (it is the whole application), so this is asserted
 * at the source level — same pattern as bottomDockKeyboardHide.test.ts.
 */
describe('chat pane collapse affordance', () => {
  const APP = 'src/App.vue'

  function splitViewTag(src: string): string {
    const open = src.indexOf('<SplitView')
    expect(open).toBeGreaterThan(-1)
    return src.slice(open, src.indexOf('>', open))
  }

  /** The `<button class="chat-rail" …>` opening tag. */
  function railTag(src: string): string {
    const start = src.indexOf('class="chat-rail"')
    expect(start, 'the collapsed-chat rail must exist').toBeGreaterThan(-1)
    const tagStart = src.lastIndexOf('<button', start)
    return src.slice(tagStart, src.indexOf('>', start))
  }

  it('wires BOTH divider handles to the collapse handler', () => {
    const tag = splitViewTag(readWebFile(APP))
    expect(tag).toContain(':collapse-left="isWideScreen"')
    expect(tag).toContain(':collapse-right="isWideScreen"')
    expect(tag).toContain('@collapse="onSplitCollapse"')
  })

  it('routes each side to its own setter, through the shared guards', () => {
    const src = readWebFile(APP)
    const start = src.indexOf('function onSplitCollapse')
    expect(start, 'onSplitCollapse must exist').toBeGreaterThan(-1)
    const body = src.slice(start, src.indexOf('\n}', start))
    // Right hides chat, left hides the left pane. Both go through the composable
    // setters so the "never both hidden" guard stays in one place.
    expect(body).toContain("if (side === 'right')")
    expect(body).toContain('setChatCollapsed(true)')
    expect(body).toContain('setLeftCollapsed(true)')
  })

  it('offers a right-edge rail that only exists while chat is hidden', () => {
    const src = readWebFile(APP)
    const tag = railTag(src)
    expect(tag).toContain('data-action="show-chat"')
    // Wide screen only: narrow layout still has the chat dock tab.
    expect(tag).toContain('v-if="isWideScreen && chatCollapsed"')
    expect(tag).toContain('@click.stop="toggleChatPane"')
    expect(tag).toContain(':aria-label="t(\'nav.showChat\')"')
  })

  it('keeps the unread badge inside the rail', () => {
    // The rail is 24px wide and flush against the viewport edge, so the dock's
    // convention (`right: -6dp`, hanging off the icon) would clip. Neither may
    // it grow leftwards: that spills over the panel the rail sits beside.
    const src = readWebFile(APP)
    const start = src.indexOf('.chat-rail-badge {')
    expect(start).toBeGreaterThan(-1)
    const rule = src.slice(start, src.indexOf('}', start))
    expect(rule).toContain('left: 50%')
    expect(rule).toContain('transform: translateX(-50%)')
    expect(rule, 'must not hang off the icon horizontally').not.toContain('right: 100%')
    expect(rule, 'must not hang off the icon horizontally').not.toContain('left: 100%')
    // Tightened padding: the widest case ("99+") has to fit in 24px.
    expect(rule).toContain('padding: 0 1px')
  })

  it('shows the unread count on the rail', () => {
    const src = readWebFile(APP)
    const start = src.indexOf('class="chat-rail"')
    const end = src.indexOf('</button>', start)
    const block = src.slice(start, end)
    expect(block).toContain('chatUnreadCount > 0')
    expect(block).toContain('formatBadgeCount(store.state.chatUnreadCount)')
  })

  it('uses the chat bubble icon on the rail', () => {
    // Same glyph as the narrow-layout chat dock tab: the rail restores the
    // conversation, so it must not wear a panel/sidebar icon (that reads as
    // "layout control") nor the crossed-out bubble (`MessageSquareOff` means
    // "mute").
    const src = readWebFile(APP)
    const start = src.indexOf('class="chat-rail"')
    const block = src.slice(start, src.indexOf('</button>', start))
    expect(block).toContain('<MessageSquare />')
    expect(block).not.toContain('<PanelRightOpen')
    expect(src).not.toMatch(/<MessageSquareOff/)
    expect(src).toMatch(/import\s*\{[^}]*\bMessageSquare\b[^}]*\}\s*from\s*'lucide-vue-next'/)
  })

  it('sizes the rail at 24 design px, capped so it does not eat the editor', () => {
    const src = readWebFile(APP)
    const start = src.indexOf('.chat-rail {')
    expect(start).toBeGreaterThan(-1)
    const rule = src.slice(start, src.indexOf('}', start))
    expect(rule).toContain('width: min(calc(24 * var(--dp)), 30px)')
    expect(rule).toContain('position: absolute')
    expect(rule).toContain('right: 0')
    // It is an overlay, so the pane beneath must give up exactly its width —
    // otherwise the rail covers the left pane's last 24px.
    expect(src).toContain('.content-area.has-chat-rail')
    expect(src).toMatch(/\.content-area\.has-chat-rail\s*\{[^}]*padding-right:\s*min\(calc\(24 \* var\(--dp\)\), 30px\)/)
    const areaTag = src.match(/<div class="content-area"[^>]*>/)
    expect(areaTag, 'the content area must be tagged for the rail').not.toBeNull()
    expect(areaTag![0]).toContain("'has-chat-rail': isWideScreen && chatCollapsed")
  })

  it('does not rely on a fixed background step for contrast', () => {
    // Design-guide red line 4: `--bg-tertiary` is not reliably distinguishable
    // from `--bg-secondary`, and the panes on either side are `--bg-secondary`.
    const src = readWebFile(APP)
    const start = src.indexOf('.chat-rail {')
    const rule = src.slice(start, src.indexOf('}', start))
    expect(rule).toMatch(/background:\s*color-mix\(in srgb, var\(--text-primary\) 6%, var\(--bg-secondary\)\)/)
    expect(rule).toMatch(/border-left:\s*1px solid color-mix\(in srgb, var\(--text-primary\) 22%, transparent\)/)
  })

  it('removes every other chat-toggle entry point', () => {
    // One return path only. The dock button and the floating FAB were both
    // tried and rejected — leftovers would leave competing affordances.
    const src = readWebFile(APP)
    expect(src).not.toContain('wide-dock-bottom')
    expect(src).not.toContain('chat-restore-fab')
    expect(src).not.toContain('data-action="hide-chat"')
  })
})
