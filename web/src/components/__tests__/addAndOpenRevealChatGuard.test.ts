import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Regression guard: "add and open" must actually REVEAL the chat panel.
 *
 * The bug it prevents (reported as "clicking the right button does exactly the
 * same as clicking the row"): the reveal handler called
 *
 *     switchTab('chat')
 *     handleSessionSelect(sessionId)
 *
 * On wide screens `switchTab('chat')` RETURNS EARLY — chat is not a left-column
 * tab, so the function only routes non-chat tabs:
 *
 *     if (isWideScreen.value) {
 *       if (tab === 'chat') return   // <-- no-op
 *       switchLeftTab(tab)
 *       return
 *     }
 *
 * The narrow branch (which does `activeTab.value = tab`) never runs. So when the
 * user had HIDDEN the chat column, the session switched behind the still-hidden
 * panel and the user saw nothing happen — indistinguishable from a plain add.
 * The two actions must stay distinguishable in every layout.
 *
 * Source-level because App.vue is one large component with no mount-level test
 * (same approach as completionNotifyChatPanelGuard.test.ts).
 */

/** Strip comments so an assertion cannot be satisfied by prose alone. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
}

const code = stripComments(readWebFile('src/App.vue'))

/** Body of a top-level `function <name>(` (brace-matched). */
function functionBody(src: string, name: string): string {
  const at = src.indexOf(`function ${name}(`)
  if (at === -1) throw new Error(`${name} not found in App.vue`)
  const open = src.indexOf('{', at)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error(`unbalanced braces in ${name}`)
}

/** The arrow-function body passed to setOpenSessionHandler. */
function openSessionHandlerBody(src: string): string {
  const at = src.indexOf('setOpenSessionHandler(')
  if (at === -1) throw new Error('setOpenSessionHandler not found in App.vue')
  const open = src.indexOf('{', at)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error('unbalanced braces in setOpenSessionHandler')
}

describe('"add and open" reveals the chat panel', () => {
  const body = openSessionHandlerBody(code)

  it('expands a collapsed wide-screen chat column', () => {
    // The fix. Without this the wide-screen path is a silent no-op.
    expect(body, 'the collapsed chat column is never expanded').toMatch(/setChatCollapsed\(false\)/)
  })

  it('guards the expansion on actually being wide-screen + collapsed', () => {
    // Expanding unconditionally would fight the narrow layout, where the
    // `chatCollapsed` flag is not what hides the panel.
    expect(body).toMatch(/isWideScreen\.value/)
    expect(body).toMatch(/chatCollapsed\.value/)
  })

  it('still switches to the session', () => {
    expect(body).toMatch(/handleSessionSelect\(/)
  })

  it('the early-return it works around is still present upstream', () => {
    // If switchTab ever stops early-returning for chat, this guard is stale —
    // fail loudly rather than silently keep a redundant fix.
    const switchTabBody = functionBody(code, 'switchTab')
    expect(switchTabBody, 'switchTab no longer early-returns for chat on wide screens')
      .toMatch(/if\s*\(\s*tab\s*===\s*['"]chat['"]\s*\)\s*return/)
  })
})
