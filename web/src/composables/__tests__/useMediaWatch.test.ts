import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// The composable has no store dependency (that would pull in i18n); the
// project root is injected instead.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import {
  mediaVersionFor,
  withVersionParam,
  mediaPathFromUrl,
  mediaPathFromImg,
  patchImagesForPath,
  bumpMediaVersion,
  mediaPaths,
  syncMediaPathsFromDom,
  trackMediaPath,
  ensureMediaObserver,
  resetMediaWatch,
  setMediaProjectRoot,
  mutationTouchesMedia,
  MAX_MEDIA_WATCH_PATHS,
  _syncMediaPathsForTesting,
} from '@/composables/useMediaWatch.ts'

/** Build an <img> and attach it so document queries find it. */
function appendImg(attrs: Record<string, string>): HTMLImageElement {
  const img = document.createElement('img')
  for (const [k, v] of Object.entries(attrs)) img.setAttribute(k, v)
  document.body.appendChild(img)
  return img
}

beforeEach(() => {
  setMediaProjectRoot('/proj')
})

afterEach(() => {
  setMediaProjectRoot('')
})

describe('withVersionParam', () => {
  it('appends t= when no query string exists', () => {
    expect(withVersionParam('/api/local-file/a.png', 3)).toBe('/api/local-file/a.png?t=3')
  })

  it('appends with & when a query string already exists', () => {
    expect(withVersionParam('/api/file/thumb?path=a.png&w=200', 3)).toBe(
      '/api/file/thumb?path=a.png&w=200&t=3'
    )
  })

  it('replaces an existing t= instead of accumulating params', () => {
    expect(withVersionParam('/api/local-file/a.png?t=1', 2)).toBe('/api/local-file/a.png?t=2')
    expect(withVersionParam('/api/file/thumb?path=a.png&t=1&w=200', 2)).toBe(
      '/api/file/thumb?path=a.png&w=200&t=2'
    )
  })

  it('does not leave a dangling separator', () => {
    expect(withVersionParam('/api/local-file/a.png?t=1', 5)).toBe('/api/local-file/a.png?t=5')
  })

  it('returns empty input unchanged', () => {
    expect(withVersionParam('', 3)).toBe('')
  })
})

describe('mediaPathFromUrl', () => {
  it('extracts a project-relative path from /api/local-file/', () => {
    expect(mediaPathFromUrl('/api/local-file/assets/a.png')).toBe('assets/a.png')
  })

  it('ignores the cache-buster', () => {
    expect(mediaPathFromUrl('/api/local-file/assets/a.png?t=123')).toBe('assets/a.png')
  })

  it('decodes percent-encoded segments (CJK filenames)', () => {
    expect(mediaPathFromUrl('/api/local-file/assets/%E5%9B%BE%E7%89%87.png')).toBe('assets/图片.png')
  })

  it('resolves the ?path= absolute form back to project-relative', () => {
    expect(mediaPathFromUrl('/api/local-file/?path=' + encodeURIComponent('/proj/assets/a.png'))).toBe(
      'assets/a.png'
    )
  })

  it('extracts the path from the thumbnail endpoint', () => {
    expect(mediaPathFromUrl('/api/file/thumb?path=assets%2Fa.png&w=200')).toBe('assets/a.png')
  })

  it('keeps an absolute path outside the project as-is', () => {
    // The backend drops it; the frontend must not silently rewrite it.
    expect(mediaPathFromUrl('/api/local-file/?path=' + encodeURIComponent('/elsewhere/a.png'))).toBe(
      '/elsewhere/a.png'
    )
  })

  it('rejects non-local URLs', () => {
    expect(mediaPathFromUrl('https://example.com/a.png')).toBeNull()
    expect(mediaPathFromUrl('data:image/png;base64,AAAA')).toBeNull()
    expect(mediaPathFromUrl('')).toBeNull()
  })

  it('rejects the anonymous share endpoint (no watcher there)', () => {
    expect(mediaPathFromUrl('/api/share/tok123/local/a.png')).toBeNull()
  })
})

describe('mediaPathFromImg', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('prefers data-attach-src (the decoded path)', () => {
    const img = appendImg({ src: '/api/local-file/a.png', 'data-attach-src': 'assets/a.png' })
    expect(mediaPathFromImg(img)).toBe('assets/a.png')
  })

  it('falls back to data-full-src when the inline src is a thumbnail', () => {
    const img = appendImg({
      src: '/api/file/thumb?path=assets%2Fa.png&w=200',
      'data-full-src': '/api/local-file/assets/a.png',
    })
    expect(mediaPathFromImg(img)).toBe('assets/a.png')
  })

  it('uses src when neither data attribute is present', () => {
    const img = appendImg({ src: '/api/local-file/assets/a.png' })
    expect(mediaPathFromImg(img)).toBe('assets/a.png')
  })

  it('returns null for an external image', () => {
    const img = appendImg({ src: 'https://example.com/a.png' })
    expect(mediaPathFromImg(img)).toBeNull()
  })
})

describe('bumpMediaVersion', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    resetMediaWatch()
  })

  it('starts at 0 and increments on each bump', () => {
    expect(mediaVersionFor('assets/a.png')).toBe(0)
    bumpMediaVersion('assets/a.png')
    expect(mediaVersionFor('assets/a.png')).toBe(1)
    bumpMediaVersion('assets/a.png')
    expect(mediaVersionFor('assets/a.png')).toBe(2)
  })

  it('normalizes an absolute path to the same key as the relative one', () => {
    bumpMediaVersion('/proj/assets/a.png')
    expect(mediaVersionFor('assets/a.png')).toBe(1)
  })

  it('keeps versions independent per path', () => {
    bumpMediaVersion('assets/a.png')
    bumpMediaVersion('assets/a.png')
    bumpMediaVersion('assets/b.png')
    expect(mediaVersionFor('assets/a.png')).toBe(2)
    expect(mediaVersionFor('assets/b.png')).toBe(1)
  })

  it('rewrites the src of every on-screen element showing that path', () => {
    const img = appendImg({ src: '/api/local-file/assets/a.png' })

    bumpMediaVersion('assets/a.png')

    expect(img.getAttribute('src')).toBe('/api/local-file/assets/a.png?t=1')
  })

  it('rewrites data-full-src too, so the lightbox gets the new bytes', () => {
    const img = appendImg({
      src: '/api/file/thumb?path=assets%2Fa.png&w=200',
      'data-full-src': '/api/local-file/assets/a.png',
    })

    bumpMediaVersion('assets/a.png')

    expect(img.getAttribute('data-full-src')).toBe('/api/local-file/assets/a.png?t=1')
    expect(img.getAttribute('src')).toBe('/api/file/thumb?path=assets%2Fa.png&w=200&t=1')
  })

  it('patches every element rendering the same file', () => {
    const a = appendImg({ src: '/api/local-file/assets/a.png' })
    const b = appendImg({ 'data-attach-src': 'assets/a.png', src: '/api/local-file/assets/a.png' })
    const other = appendImg({ src: '/api/local-file/assets/other.png' })

    bumpMediaVersion('assets/a.png')

    expect(a.getAttribute('src')).toContain('t=1')
    expect(b.getAttribute('src')).toContain('t=1')
    expect(other.getAttribute('src')).not.toContain('t=')
  })

  it('does not touch an element already at the new version twice', () => {
    const img = appendImg({ src: '/api/local-file/assets/a.png' })
    bumpMediaVersion('assets/a.png')
    const afterFirst = img.getAttribute('src')
    // A second bump must advance the version, not duplicate the param.
    bumpMediaVersion('assets/a.png')
    expect(img.getAttribute('src')).toBe('/api/local-file/assets/a.png?t=2')
    expect(afterFirst).toBe('/api/local-file/assets/a.png?t=1')
  })
})

describe('patchImagesForPath', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    resetMediaWatch()
  })

  it('returns the number of elements patched', () => {
    appendImg({ src: '/api/local-file/a.png' })
    appendImg({ src: '/api/local-file/a.png' })
    appendImg({ src: '/api/local-file/b.png' })

    expect(patchImagesForPath('a.png', 7)).toBe(2)
  })

  it('does not match a different file with a shared suffix', () => {
    appendImg({ src: '/api/local-file/deep/a.png' })
    expect(patchImagesForPath('a.png', 7)).toBe(0)
  })
})

describe('media path discovery', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    resetMediaWatch()
  })

  it('collects every local image currently rendered', () => {
    appendImg({ src: '/api/local-file/assets/a.png' })
    appendImg({ src: '/api/file/thumb?path=assets%2Fb.png&w=200' })
    appendImg({ src: 'https://example.com/c.png' })

    _syncMediaPathsForTesting()

    expect(mediaPaths.value).toEqual(['assets/a.png', 'assets/b.png'])
  })

  it('deduplicates the same file rendered in several places', () => {
    appendImg({ src: '/api/local-file/assets/a.png' })
    appendImg({ 'data-attach-src': 'assets/a.png', src: '/api/local-file/assets/a.png' })

    _syncMediaPathsForTesting()

    expect(mediaPaths.value).toEqual(['assets/a.png'])
  })

  it('drops a path once its element leaves the DOM', () => {
    const img = appendImg({ src: '/api/local-file/assets/a.png' })
    _syncMediaPathsForTesting()
    expect(mediaPaths.value).toEqual(['assets/a.png'])

    img.remove()
    _syncMediaPathsForTesting()
    expect(mediaPaths.value).toEqual([])
  })

  it('caps the list so one client cannot exhaust the watch budget', () => {
    for (let i = 0; i < MAX_MEDIA_WATCH_PATHS + 10; i++) {
      appendImg({ src: `/api/local-file/assets/img-${String(i).padStart(4, '0')}.png` })
    }

    _syncMediaPathsForTesting()

    expect(mediaPaths.value).toHaveLength(MAX_MEDIA_WATCH_PATHS)
  })

  it('produces a stable sorted order so an unchanged set is not re-sent', () => {
    appendImg({ src: '/api/local-file/b.png' })
    appendImg({ src: '/api/local-file/a.png' })
    _syncMediaPathsForTesting()
    const first = mediaPaths.value

    _syncMediaPathsForTesting()
    expect(mediaPaths.value).toBe(first)
  })

  it('includes component-tracked paths with no element in the DOM', () => {
    const release = trackMediaPath('assets/not-mounted.png')

    expect(mediaPaths.value).toContain('assets/not-mounted.png')

    release()
    expect(mediaPaths.value).not.toContain('assets/not-mounted.png')
  })

  it('refcounts component-tracked paths', () => {
    const releaseA = trackMediaPath('assets/a.png')
    const releaseB = trackMediaPath('assets/a.png')

    releaseA()
    expect(mediaPaths.value).toContain('assets/a.png')

    releaseB()
    expect(mediaPaths.value).not.toContain('assets/a.png')
  })

  it('merges DOM and component-tracked paths without duplicates', () => {
    appendImg({ src: '/api/local-file/assets/a.png' })
    const release = trackMediaPath('assets/a.png')

    _syncMediaPathsForTesting()

    expect(mediaPaths.value).toEqual(['assets/a.png'])
    release()
  })
})

describe('ensureMediaObserver', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    resetMediaWatch()
  })

  afterEach(() => {
    resetMediaWatch()
  })

  it('discovers images added after the observer starts', async () => {
    ensureMediaObserver()
    expect(mediaPaths.value).toEqual([])

    appendImg({ src: '/api/local-file/assets/late.png' })

    // The observer coalesces into a rAF; flush it.
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    await Promise.resolve()

    expect(mediaPaths.value).toEqual(['assets/late.png'])
  })

  it('picks up an in-place src swap (Vue re-render)', async () => {
    const img = appendImg({ src: '/api/local-file/assets/a.png' })
    ensureMediaObserver()
    syncMediaPathsFromDom()

    img.setAttribute('src', '/api/local-file/assets/b.png')

    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    await Promise.resolve()

    expect(mediaPaths.value).toEqual(['assets/b.png'])
  })

  it('is idempotent — a second call does not double-observe', async () => {
    ensureMediaObserver()
    ensureMediaObserver()

    appendImg({ src: '/api/local-file/assets/a.png' })
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    await Promise.resolve()

    expect(mediaPaths.value).toEqual(['assets/a.png'])
  })

  it('ignores mutations that cannot involve an image', async () => {
    ensureMediaObserver()
    // A text-only change (the common streaming case) must not schedule a scan.
    const para = document.createElement('p')
    document.body.appendChild(para)
    para.textContent = 'streaming text'

    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    await Promise.resolve()

    // The <p> has no img inside, so nothing new should be tracked.
    expect(mediaPaths.value).toEqual([])
  })

  it('discovers an image nested inside an added subtree', async () => {
    ensureMediaObserver()

    const wrap = document.createElement('div')
    const img = document.createElement('img')
    img.setAttribute('src', '/api/local-file/assets/nested.png')
    wrap.appendChild(img)
    document.body.appendChild(wrap)

    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)))
    await Promise.resolve()

    expect(mediaPaths.value).toEqual(['assets/nested.png'])
  })

  describe('mutationTouchesMedia (scan filter)', () => {
    /** Capture the records a mutation produces. */
    async function recordsFor(mutate: () => void): Promise<MutationRecord[]> {
      const seen: MutationRecord[] = []
      const obs = new MutationObserver((rs) => seen.push(...rs))
      obs.observe(document.body, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ['src', 'data-full-src', 'data-attach-src'],
      })
      mutate()
      await Promise.resolve()
      obs.disconnect()
      return seen
    }

    it('is false for a text-only change', async () => {
      const para = document.createElement('p')
      document.body.appendChild(para)

      const records = await recordsFor(() => { para.textContent = 'hello' })

      expect(records.length).toBeGreaterThan(0)
      expect(records.every((r) => !mutationTouchesMedia(r))).toBe(true)
    })

    it('is true when an <img> is added', async () => {
      const records = await recordsFor(() => {
        document.body.appendChild(document.createElement('img'))
      })

      expect(records.some(mutationTouchesMedia)).toBe(true)
    })

    it('is true when a subtree containing an <img> is added', async () => {
      const records = await recordsFor(() => {
        const wrap = document.createElement('div')
        wrap.appendChild(document.createElement('img'))
        document.body.appendChild(wrap)
      })

      expect(records.some(mutationTouchesMedia)).toBe(true)
    })

    it('is true when an existing <img> src is swapped', async () => {
      const img = document.createElement('img')
      img.setAttribute('src', '/api/local-file/a.png')
      document.body.appendChild(img)

      const records = await recordsFor(() => {
        img.setAttribute('src', '/api/local-file/b.png')
      })

      expect(records.some(mutationTouchesMedia)).toBe(true)
    })

    it('is false when a watched attribute changes on a non-image element', async () => {
      // `src` is in the attribute filter, so this DOES produce a record — the
      // predicate must still reject it because the target is not an image.
      const frame = document.createElement('iframe')
      document.body.appendChild(frame)

      const records = await recordsFor(() => {
        frame.setAttribute('src', 'about:blank')
      })

      expect(records.length).toBeGreaterThan(0)
      expect(records.every((r) => !mutationTouchesMedia(r))).toBe(true)
    })
  })
})

describe('resetMediaWatch', () => {
  it('clears versions and tracked paths', () => {
    document.body.innerHTML = ''
    appendImg({ src: '/api/local-file/assets/a.png' })
    bumpMediaVersion('assets/a.png')
    _syncMediaPathsForTesting()
    expect(mediaVersionFor('assets/a.png')).toBe(1)

    resetMediaWatch()

    expect(mediaVersionFor('assets/a.png')).toBe(0)
    expect(mediaPaths.value).toEqual([])
  })
})
