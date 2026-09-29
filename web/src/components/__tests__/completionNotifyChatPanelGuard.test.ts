import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Regression guard: the in-app completion notification must not suppress itself
 * for the current session while the chat panel is actually off screen.
 *
 * The bug: `handleCompletionEvent` decided "the user is looking at this session"
 * with
 *
 *     const chatPanelActive = isWideScreen || activeTab.value === 'chat'
 *
 * `isWideScreen` is a ref from useWideScreenLayout, and `<script setup>` does NOT
 * auto-unwrap refs in script code. A ref object is always truthy, so the
 * expression was a constant `true` and the guard collapsed to
 * "sessionId === currentSessionId" — every event for the session you had open
 * was swallowed no matter which tab you were on.
 *
 * Why that mattered: on Android the native background service suppresses its own
 * system notification while the app is in the foreground (it assumes the in-app
 * card will cover it). With the card also suppressed, an approval request for
 * the open session produced NO notification at all on either channel — the user
 * had to notice the stalled session by themselves.
 *
 * Source-level because App.vue is a single large component with no mount-level
 * test (same approach as wideScreenFocusOnEnter.test.ts / unreadNoAutoClear).
 */

/** Strip comments so an assertion cannot be satisfied by prose alone. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')   // block comments
    .replace(/\/\/.*$/gm, '')           // line comments, whole-line or trailing
}

const code = stripComments(readWebFile('src/App.vue'))

/** Body of handleCompletionEvent (brace-matched from its `function` keyword). */
function completionHandlerBody(src: string): string {
  const at = src.indexOf('function handleCompletionEvent(')
  if (at === -1) throw new Error('handleCompletionEvent not found in App.vue')
  const open = src.indexOf('{', at)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '{') depth++
    else if (src[i] === '}') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error('unbalanced braces in handleCompletionEvent')
}

describe('in-app completion notification: chat-panel visibility guard', () => {
  const body = completionHandlerBody(code)

  it('delegates to the shared isChatPanelVisible predicate', () => {
    expect(body, 'isChatPanelVisible is no longer consulted').toContain('isChatPanelVisible(')
  })

  it('never tests a ref object in boolean context', () => {
    // The exact regression. `isWideScreen` / `chatCollapsed` are refs; using
    // either bare (without .value, or without handing it to the predicate) is a
    // constant-true expression, not a visibility check.
    expect(body).not.toMatch(/\bisWideScreen\s*\|\|/)
    expect(body).not.toMatch(/\bchatCollapsed\s*\|\|/)
    expect(body).not.toMatch(/\bactiveTab\s*===\s*['"]chat['"]/)
  })

  it('passes unwrapped values into the predicate', () => {
    // The predicate takes plain booleans/strings precisely so a bare ref is a
    // type error rather than a silent truthy. Reading the refs without `.value`
    // would defeat that.
    expect(body).toMatch(/isWideScreen:\s*isWideScreen\.value/)
    expect(body).toMatch(/chatCollapsed:\s*chatCollapsed\.value/)
    expect(body).toMatch(/activeTab:\s*activeTab\.value/)
  })

  it('still suppresses the notification for the session on screen', () => {
    // The guard's purpose must survive the fix: a completion for the session the
    // user is watching should not pop a card.
    expect(body).toMatch(/sessionId\s*===\s*sessionIdentity\.currentSessionId\.value/)
  })
})
