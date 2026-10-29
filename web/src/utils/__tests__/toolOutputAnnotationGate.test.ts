import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Every call site that renders a tool's output WHILE IT IS STILL RUNNING must
 * pass `annotate: false`.
 *
 * The annotation pass is a parse + re-serialize plus five full-document walks
 * (one per annotator), on a fragment that can reach the backend's 50 KB output
 * cap — and the host re-renders on every streamed update, so annotating
 * mid-stream turns a growing tool output into a per-frame stall. It also chips
 * paths the model has not finished writing.
 *
 * `formatToolOutput`'s default is `true` (a forgotten call site over-annotates
 * rather than silently losing the feature), which is exactly why the streaming
 * ones need pinning: nothing at runtime distinguishes them.
 *
 * A source guard rather than a behavioural test: the cost only shows up with a
 * real growing output, and jsdom cannot measure it (its DOMParser dominates the
 * profile by an order of magnitude over a real browser's).
 */
const STREAMING_CALL_SITES: Array<[string, string]> = [
  ['ChatPanelContent.vue', 'web/src/components/chat/ChatPanelContent.vue'],
  ['TaskExecDetail.vue', 'web/src/components/task/TaskExecDetail.vue'],
]

describe('streaming tool-output call sites disable annotation', () => {
  it.each(STREAMING_CALL_SITES)('%s gates on done', (_name, path) => {
    const src = readWebFile(path)
    const calls = src.match(/formatToolOutput\([^)]*\)/g) || []
    expect(calls.length, `${path} no longer calls formatToolOutput`).toBeGreaterThan(0)
    for (const call of calls) {
      expect(call, `ungated streaming render in ${path}: ${call}`).toMatch(/annotate:\s*!!data\.done/)
    }
  })

  it('the drawer gates both its live and its polled render', () => {
    const src = readWebFile('web/src/composables/useToolDetailDrawer.ts')
    // Live block: annotate only when the block is done.
    expect(src).toMatch(/formatToolOutput\(block\.output as string,[^)]*annotate: !!block\.done/)
    // Polled detail fetch: the response's own done flag wins, the live block's
    // is the fallback (the endpoint may not report done at all).
    expect(src).toMatch(/annotate: finished/)
    expect(src).toMatch(/const finished = data\.done !== undefined \? !!data\.done : !!block\.done/)
  })
})
