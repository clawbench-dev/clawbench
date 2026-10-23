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

  /**
   * Strip comments before asserting on source text.
   *
   * Without this, every `indexOf` below can be satisfied by the EXPLANATORY
   * COMMENT rather than the code: the consume block documents itself with
   * "The target is reached via `jumpToUserMessage`", which is the first
   * occurrence in the file — so deleting the real call still left the guard
   * green (verified by mutating the call to `found = true`). Same convention
   * as platformAxes.test.ts.
   */
  function stripComments(text: string): string {
    return text.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '')
  }

  const panelCode = stripComments(panel)

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
    //
    // Assert the CALL ITSELF, not the identifier: the identifier also appears in
    // the block's own documentation, so `indexOf('jumpToUserMessage')` was
    // satisfied by a comment and survived deleting the call.
    expect(panelCode).toContain('await messageListRef.value?.jumpToUserMessage?.(')
    const consume = panelCode.indexOf('consumePendingMessageNavigation()')
    expect(consume).toBeGreaterThan(-1)
    expect(panelCode.indexOf('jumpToUserMessage?.(')).toBeGreaterThan(consume)
    // The old loaded-only call must be gone from the consume path.
    expect(panelCode.slice(consume)).not.toContain('scrollToMessage(')
  })

  it('the panel re-runs when the request itself arrives (watch source)', () => {
    // Searching the CURRENT session leaves both the session id and the message
    // count unchanged, so a watch on those alone never fires: the request would
    // sit parked until an unrelated arrival happened to satisfy it.
    //
    // Anchor on the CONSUME call and search backwards for its watcher: the
    // identifier also appears in the import at the top of the file, so a forward
    // search from the top finds the import instead of the watcher.
    const consume = panelCode.indexOf('consumePendingMessageNavigation()')
    expect(consume).toBeGreaterThan(-1)
    const watchStart = panelCode.lastIndexOf('watch(', consume)
    expect(watchStart).toBeGreaterThan(-1)
    // The dependency array closes with `],` before the callback; slicing to the
    // first ')' or '=>' would stop inside the array's own entries.
    const deps = panelCode.slice(watchStart, panelCode.indexOf('],', watchStart))
    expect(deps).toContain('pendingMessageNavigation')
    // …and the watcher must be the one that consumes, not an unrelated watch.
    expect(consume - watchStart).toBeLessThan(1200)
  })

  it('the panel waits for the session switch to finish before judging the target', () => {
    // switchSession clears the array synchronously and refetches. Consuming
    // during that window searches an empty list, concludes the message is gone
    // and shows a spurious failure toast.
    //
    // The decision now lives in decidePendingNavigation (see
    // pendingNavigation.test.ts for its behaviour); this pins that the panel
    // passes the switching flag in and only consumes on a `jump` verdict, so
    // the guard cannot be dropped by editing the call site.
    const callStart = panelCode.indexOf('decidePendingNavigation(')
    expect(callStart).toBeGreaterThan(-1)
    const call = panelCode.slice(callStart, panelCode.indexOf(')', callStart) + 1)
    expect(call).toContain('session.switching.value')

    const consume = panelCode.indexOf('consumePendingMessageNavigation()')
    expect(consume).toBeGreaterThan(-1)
    const gate = panelCode.indexOf("if (decision.kind !== 'jump') return")
    expect(gate).toBeGreaterThan(-1)
    // The gate must precede the consume call: a request consumed before the
    // verdict is checked is gone for good, and the jump never happens.
    expect(gate).toBeLessThan(consume)
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
