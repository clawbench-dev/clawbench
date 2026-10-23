import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Session-search hit → jump to the matched message (issue #580).
 *
 * The chain spans four files and every link is easy to drop silently:
 *
 *   SessionSearchDrawer  chunk jump button → emit('open', session, messageId)
 *   App.vue              parks setPendingMessageNavigation BEFORE opening
 *   ChatPanelContent     consumes it and reaches the message by PAGING
 *   ChatMessageList      exposes the paging jump, not just the loaded-only one
 *
 * App.vue and ChatPanelContent have no mountable component test for this path
 * (App.vue is only ever read as source text, and the chat panel needs a live
 * session), so the routing is pinned here as source guards. The behaviour of
 * each piece is covered by its own suite: SessionSearchDrawer.test.ts for the
 * emit, useUserMsgIndex.test.ts for the paging + return value.
 */
describe('session-search jump wiring', () => {
  const app = readWebFile('src/App.vue')
  const panel = readWebFile('src/components/chat/ChatPanelContent.vue')
  const list = readWebFile('src/components/chat/ChatMessageList.vue')
  const drawer = readWebFile('src/components/session/SessionSearchDrawer.vue')

  // Slice to the END of the handler rather than a fixed character count: the
  // explanatory comments grow over time, and a too-small window silently
  // truncates the very statements being asserted (indexOf then returns -1).
  function openFromSearchBody(): string {
    const start = app.indexOf('function handleOpenFromSearch')
    expect(start).toBeGreaterThan(-1)
    return app.slice(start, app.indexOf('\n}', start) + 2)
  }

  it('App parks the target message BEFORE opening the session', () => {
    const body = openFromSearchBody()
    const park = body.indexOf('setPendingMessageNavigation(session.session_id, messageId')
    const open = body.indexOf('handleSessionSelect(')
    expect(park).toBeGreaterThan(-1)
    expect(open).toBeGreaterThan(-1)
    // Parking after the switch would race the panel's consumer: it fires on the
    // session-identity change, which the switch produces.
    expect(park).toBeLessThan(open)
  })

  it('App tags the request as a search hit (failure copy depends on it)', () => {
    // Quote/notification jumps share this channel; claiming "that search result
    // is gone" for them would be wrong.
    expect(openFromSearchBody()).toContain("'search'")
  })

  it('App routes a CURRENT-session hit around the already-active guard', () => {
    // handleSessionSelect returns early when the id matches the current one (it
    // exists to stop list navigation from reloading the open session). A search
    // hit for the session already on screen must still scroll, so the handler
    // has to short-circuit before calling it.
    const body = openFromSearchBody()
    const sameSessionCheck = body.indexOf('sessionIdentity.currentSessionId.value')
    expect(sameSessionCheck).toBeGreaterThan(-1)
    expect(sameSessionCheck).toBeLessThan(body.indexOf('handleSessionSelect('))
    // handleSessionSelect is the only other place that closes the list drawer,
    // so the short-circuit has to close it too (on mobile the list sits under
    // the search sheet).
    const branch = body.slice(sameSessionCheck)
    expect(branch).toContain('sessionIdentity.sessionDrawer.close()')
  })

  it('the panel reaches the target by PAGING, not by the loaded-only scroll', () => {
    // scrollToMessage only searches the already-loaded window (20 messages) and
    // silently does nothing when the hit is older. A search hit routinely is.
    const start = panel.indexOf('function scrollBottom')
    const body = panel.slice(start)
    const consume = body.indexOf('jumpToUserMessage')
    expect(consume).toBeGreaterThan(-1)
    // The old loaded-only call must be gone from the consume path.
    const consumeBlock = body.slice(0, body.indexOf('// Async render flush'))
    expect(consumeBlock).not.toContain('scrollToMessage(')
  })

  it('the panel re-runs when the request itself arrives (watch source)', () => {
    // Searching the CURRENT session leaves both the session id and the message
    // count unchanged, so a watch on those alone never fires: the request would
    // sit parked until an unrelated arrival happened to satisfy it.
    const start = panel.indexOf('Consume a pending')
    expect(start).toBeGreaterThan(-1)
    const body = panel.slice(start, start + 2600)
    const watchStart = body.indexOf('watch(')
    // The dependency array closes with `],` before the callback; slicing to the
    // first ')' or '=>' would stop inside the array's own entries.
    const deps = body.slice(watchStart, body.indexOf('],', watchStart))
    expect(deps).toContain('pendingMessageNavigation')
  })

  it('the panel waits for the session switch to finish before judging the target', () => {
    // switchSession clears the array synchronously and refetches. Consuming
    // during that window searches an empty list, concludes the message is gone
    // and shows a spurious failure toast. The ORDER is what matters: merely
    // having the guard somewhere in the block would still pass with it moved
    // below the consume call, which is exactly the bug.
    const start = panel.indexOf('Consume a pending')
    const body = panel.slice(start, start + 2600)
    const guard = body.indexOf('if (session.switching.value) return')
    const consume = body.indexOf('consumePendingMessageNavigation()')
    expect(guard).toBeGreaterThan(-1)
    expect(consume).toBeGreaterThan(-1)
    expect(guard).toBeLessThan(consume)
  })

  it('the list exposes the paging jump so the panel can reach it', () => {
    const expose = list.slice(list.indexOf('defineExpose({'))
    expect(expose).toContain('jumpToUserMessage')
  })

  it('the drawer passes the matched message id with the open event', () => {
    expect(drawer).toContain("emit('open', selectedSession, chunk.message_id)")
    // Browse previews / title-only matches are not search hits.
    expect(drawer).toContain('!isBrowseMode && !selectedSession?.title_only')
  })

  it('the drawer hides the jump on ARCHIVED sessions', () => {
    // The history endpoint filters archived rows out, so opening one 404s and
    // the chat panel's recovery branch silently opens a DIFFERENT session. The
    // footer already swaps "Open" for "Resume" for this exact reason; the jump
    // button must follow the same rule.
    const btn = drawer.slice(drawer.indexOf('class="detail-chunk-jump"') - 700)
    const cond = btn.slice(0, btn.indexOf('type="button"'))
    expect(cond).toContain('!selectedSession?.archived')
  })
})
