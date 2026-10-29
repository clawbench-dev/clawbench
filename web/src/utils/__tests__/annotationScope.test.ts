import { describe, expect, it, vi, beforeEach } from 'vitest'

/**
 * The `data-no-annotate` opt-out must hold for EVERY annotator.
 *
 * Each one walks the document independently, so a guard added to four of five
 * would still let the fifth rewrite a region the producer declared off-limits
 * (a tool file header, an ask card's option labels, a file's own source). This
 * file asserts the contract once per annotator rather than relying on each
 * annotator's own suite to remember it.
 */

const mockIsAppMode = { value: true }

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode, isDesktopApp: { value: false } }),
}))
vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({
    sshInfo: { value: { enabled: true } },
    transportAllowsH2: { value: true },
  }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: vi.fn() }) }))
vi.mock('@/composables/useLocale', () => ({ gt: (key: string) => key }))
vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/proj', homeDir: '/home/u' } },
}))

import { annotateFilePathsIn } from '@/composables/useFilePathAnnotation.ts'
import { annotateCommitHashesIn } from '@/composables/useCommitHashAnnotation.ts'
import { annotateLocalhostUrlsIn } from '@/composables/useLocalhostAnnotation.ts'
import { annotateBareUrlsIn } from '@/composables/useUrlAnnotation.ts'
import { annotateWorktreePathsIn, warmWorktreeCache } from '@/composables/useWorktreeAnnotation.ts'
import { ANNOTATION_SKIP_ATTR } from '@/utils/annotationScope.ts'

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html')

/** Wrap `inner` in a marked region (or leave it bare) and run one annotator. */
function run(annotator: (doc: Document) => void, inner: string, skipped: boolean): Document {
  const attr = skipped ? ` ${ANNOTATION_SKIP_ATTR}` : ''
  const doc = parse(`<div${attr}>${inner}</div>`)
  annotator(doc)
  return doc
}

const cases: Array<[string, (doc: Document) => void, string, string]> = [
  [
    'file paths',
    (doc) => { annotateFilePathsIn(doc, { projectRoot: '/proj', homeDir: '/home/u' }) },
    '<pre>src/a.ts</pre>',
    '.chat-file-path',
  ],
  [
    'commit hashes',
    (doc) => { annotateCommitHashesIn(doc) },
    '<pre>3f7974f58</pre>',
    '.chat-commit-hash-pending',
  ],
  [
    'localhost URLs',
    (doc) => { annotateLocalhostUrlsIn(doc) },
    '<pre>http://localhost:3000/api</pre>',
    '.chat-url-open-btn',
  ],
  [
    'bare URLs',
    (doc) => { annotateBareUrlsIn(doc) },
    '<pre>github.com/foo/bar</pre>',
    '.md-external-link',
  ],
]

describe.each(cases)('%s annotator honours the opt-out', (_name, annotator, inner, selector) => {
  it('annotates the same content when the region is NOT marked', () => {
    // Guards against a vacuous pass: the selector must actually match here, or
    // "nothing matched when skipped" would be true for the wrong reason.
    const doc = run(annotator, inner, false)
    expect(doc.body.querySelector(selector)).not.toBeNull()
  })

  it('leaves the content untouched when the region IS marked', () => {
    const doc = run(annotator, inner, true)
    expect(doc.body.querySelector(selector)).toBeNull()
  })
})

describe('worktree annotator honours the opt-out', () => {
  const WORKTREE_PATH = '/proj/.worktrees/feat'

  beforeEach(async () => {
    // The annotator reads a module-level cache, and a cache miss makes it a
    // no-op — which would make BOTH assertions vacuous. Seed it through the
    // real warm-up path (a stubbed /api/git/worktrees) rather than poking the
    // cache directly, so the fixture matches what the app actually stores.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        worktrees: [
          { path: '/proj', displayPath: '.', branch: 'main', isCurrent: true, dirty: false, changeCount: 0, untrackedCount: 0, locked: false, missing: false },
          { path: WORKTREE_PATH, displayPath: '.worktrees/feat', branch: 'feat', isCurrent: false, dirty: false, changeCount: 0, untrackedCount: 0, locked: false, missing: false },
        ],
      }),
    }))
    await warmWorktreeCache('/proj')
    vi.unstubAllGlobals()
  })

  const inner = `<pre>${WORKTREE_PATH}/src/a.ts</pre>`
  const annotate = (doc: Document) => { annotateWorktreePathsIn(doc, { projectRoot: '/proj' }) }

  it('annotates the same content when the region is NOT marked', () => {
    const doc = parse(`<div>${inner}</div>`)
    annotate(doc)
    expect(doc.body.querySelector('.chat-worktree-btn')).not.toBeNull()
  })

  it('leaves the content untouched when the region IS marked', () => {
    const doc = parse(`<div ${ANNOTATION_SKIP_ATTR}>${inner}</div>`)
    annotate(doc)
    expect(doc.body.querySelector('.chat-worktree-btn')).toBeNull()
  })
})
