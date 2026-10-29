import { describe, expect, it, vi, beforeEach } from 'vitest'

// The pass reads the project root / home dir from the store to resolve relative
// paths; `reactive` keeps it a real dependency like the live app.
//
// `vi.hoisted` runs before the module graph is evaluated, so `reactive` cannot
// be imported at the top — require it inside the factory (same pattern as
// renderToolDetail.test.ts).
const { mockStore } = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { reactive } = require('vue')
  return { mockStore: { state: reactive({ projectRoot: '/proj', homeDir: '/home/u' }) } }
})
vi.mock('@/stores/app.ts', () => ({ store: mockStore }))

vi.mock('@/composables/useLocale', () => ({ gt: (key: string) => key }))

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: { value: false }, isDesktopApp: { value: false } }),
}))

vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({
    sshInfo: { value: { enabled: false } },
    transportAllowsH2: { value: false },
  }),
}))

vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))

import { annotateToolHtml } from '@/utils/annotateToolHtml.ts'
import { ANNOTATION_SKIP_ATTR } from '@/utils/annotationScope.ts'

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html')

/** Every annotated file chip in the fragment. */
const chips = (html: string) => Array.from(parse(html).querySelectorAll<HTMLElement>('.chat-file-path'))
/** Every open-file button in the fragment. */
const openButtons = (html: string) => Array.from(parse(html).querySelectorAll('.chat-file-open-btn'))

describe('annotateToolHtml', () => {
  beforeEach(() => {
    mockStore.state.projectRoot = '/proj'
    mockStore.state.homeDir = '/home/u'
  })

  it('returns the input untouched when there is nothing to annotate', () => {
    const html = '<div class="x">just words</div>'
    const out = annotateToolHtml(html)
    expect(out.html).toBe(html)
    expect(out.detectedPaths).toEqual([])
    expect(out.detectedSHAs).toEqual([])
  })

  it('annotates a relative file path in tool output text', () => {
    const out = annotateToolHtml('<pre>src/utils/chatBlocks.ts</pre>')
    const [chip] = chips(out.html)
    expect(chip).toBeDefined()
    expect(chip.getAttribute('data-file-path')).toBe('src/utils/chatBlocks.ts')
    expect(out.detectedPaths).toContain('src/utils/chatBlocks.ts')
  })

  it('adds an open button next to the annotated path', () => {
    const out = annotateToolHtml('<pre>src/utils/chatBlocks.ts</pre>')
    expect(openButtons(out.html)).toHaveLength(1)
  })

  it('annotates a path inside <code> (the JSON fallback and Grep output shape)', () => {
    const out = annotateToolHtml('<div class="tool-json-body"><code>"file_path": "web/src/app.ts"</code></div>')
    expect(chips(out.html)).toHaveLength(1)
  })

  it('detects commit-hash candidates and reports them for verification', () => {
    const out = annotateToolHtml('<pre>commit 3f7974f58 fixed it</pre>')
    expect(out.detectedSHAs).toContain('3f7974f58')
    expect(parse(out.html).querySelector('.chat-commit-hash-pending')).not.toBeNull()
  })

  it('leaves a URL alone as a URL instead of annotating its tail as a path', () => {
    const out = annotateToolHtml('<pre>https://example.com/foo/bar.ts</pre>')
    expect(chips(out.html)).toHaveLength(0)
  })

  it('does not annotate inside a region marked as already handled', () => {
    const html = `<div class="tool-file-header" ${ANNOTATION_SKIP_ATTR}><span>src/utils/chatBlocks.ts</span></div>`
    const out = annotateToolHtml(html)
    expect(chips(out.html)).toHaveLength(0)
    expect(out.detectedPaths).toEqual([])
  })

  it('does not annotate inside a nested marked region', () => {
    const html = `<div ${ANNOTATION_SKIP_ATTR}><div><code>src/a.ts</code></div></div>`
    expect(chips(annotateToolHtml(html).html)).toHaveLength(0)
  })

  it('still annotates siblings outside the marked region', () => {
    const html = `<div ${ANNOTATION_SKIP_ATTR}><code>src/a.ts</code></div><pre>src/b.ts</pre>`
    const out = annotateToolHtml(html)
    const paths = chips(out.html).map(el => el.getAttribute('data-file-path'))
    expect(paths).toEqual(['src/b.ts'])
  })

  it('is idempotent: a second pass adds no second chip or button', () => {
    const once = annotateToolHtml('<pre>src/utils/chatBlocks.ts</pre>').html
    const twice = annotateToolHtml(once).html
    expect(chips(twice)).toHaveLength(1)
    expect(openButtons(twice)).toHaveLength(1)
  })

  it('is idempotent for commit hashes', () => {
    const once = annotateToolHtml('<pre>3f7974f58</pre>').html
    const twice = annotateToolHtml(once).html
    expect(parse(twice).querySelectorAll('.chat-commit-hash-pending')).toHaveLength(1)
  })
})
