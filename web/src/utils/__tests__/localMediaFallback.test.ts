import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  applyMediaMissingFallback,
  installLocalMediaFallback,
  uninstallLocalMediaFallback,
  isLocalMediaUrl,
  mediaPathFromSrc,
  MEDIA_MISSING_CLASS,
} from '@/utils/localMediaFallback'

// The module reads `gt()` from useLocale, which needs a live i18n instance.
// Stub it so assertions can match a stable string instead of pulling vue-i18n in.
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

/** Build the figure markup the media-block factory produces for a local image. */
function figure(src: string): string {
  return `<div class="image-block-wrapper"><div class="image-block-header">`
    + `<button class="image-block-view-btn"></button></div>`
    + `<span class="lightbox-img-wrap"><img class="chat-img lightbox-img" src="${src}"></span></div>`
}

describe('isLocalMediaUrl', () => {
  it('accepts the local file-serving endpoints', () => {
    expect(isLocalMediaUrl('/api/fs/raw/a.png')).toBe(true)
    expect(isLocalMediaUrl('/api/fs/thumb?target=a.png&w=1200')).toBe(true)
    expect(isLocalMediaUrl('/api/fs/raw/?target=/tmp/a.png')).toBe(true)
    // Share pages serve local media through the token-scoped endpoint.
    expect(isLocalMediaUrl('/api/share/tok123/local/a.png')).toBe(true)
  })

  it('rejects external and embedded srcs', () => {
    expect(isLocalMediaUrl('https://example.com/a.png')).toBe(false)
    expect(isLocalMediaUrl('//cdn.example.com/a.png')).toBe(false)
    expect(isLocalMediaUrl('data:image/png;base64,AAAA')).toBe(false)
    expect(isLocalMediaUrl('assets/a.png')).toBe(false)
  })
})

describe('mediaPathFromSrc', () => {
  it('extracts the target query param', () => {
    expect(mediaPathFromSrc('/api/fs/thumb?target=img/logo.png&w=1200')).toBe('img/logo.png')
    expect(mediaPathFromSrc('/api/fs/raw/?target=/tmp/chart.png')).toBe('/tmp/chart.png')
  })

  it('extracts the path suffix of the relative form', () => {
    expect(mediaPathFromSrc('/api/fs/raw/docs/diagram.png')).toBe('docs/diagram.png')
    // Percent-encoded non-ASCII segments are decoded for display.
    expect(mediaPathFromSrc('/api/fs/raw/%E4%B8%AD%E6%96%87/a.png')).toBe('中文/a.png')
  })

  it('returns an empty string when nothing can be derived', () => {
    expect(mediaPathFromSrc('')).toBe('')
    expect(mediaPathFromSrc('https://example.com/a.png')).toBe('')
  })
})

describe('applyMediaMissingFallback', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('replaces a failed local image with a labelled placeholder', () => {
    document.body.innerHTML = figure('/api/fs/raw/docs/diagram.png')
    const img = document.querySelector('img') as HTMLImageElement

    expect(applyMediaMissingFallback(img)).toBe(true)

    // The <img> is hidden, not removed, so the fallback stays reversible.
    expect(img.classList.contains('local-media-hidden')).toBe(true)
    expect(img.getAttribute('data-media-missing')).toBe('1')

    const ph = document.querySelector(`.${MEDIA_MISSING_CLASS}`)
    expect(ph).not.toBeNull()
    // The failure is explained AND names the file the reader expected.
    expect(ph!.textContent).toContain('imageBlock.loadFailed')
    expect(ph!.textContent).toContain('diagram.png')
  })

  it('hides the figure toolbar, which would open a lightbox onto nothing', () => {
    document.body.innerHTML = figure('/api/fs/raw/a.png')
    const img = document.querySelector('img') as HTMLImageElement
    applyMediaMissingFallback(img)

    expect(document.querySelector('.image-block-wrapper')!.classList.contains('image-block-missing')).toBe(true)
  })

  it('is idempotent — a repeated error does not stack placeholders', () => {
    document.body.innerHTML = figure('/api/fs/raw/a.png')
    const img = document.querySelector('img') as HTMLImageElement

    expect(applyMediaMissingFallback(img)).toBe(true)
    expect(applyMediaMissingFallback(img)).toBe(false)

    expect(document.querySelectorAll(`.${MEDIA_MISSING_CLASS}`)).toHaveLength(1)
  })

  it('ignores external and embedded images', () => {
    document.body.innerHTML = figure('https://example.com/a.png')
    const img = document.querySelector('img') as HTMLImageElement

    expect(applyMediaMissingFallback(img)).toBe(false)
    expect(document.querySelector(`.${MEDIA_MISSING_CLASS}`)).toBeNull()
    expect(img.hasAttribute('data-media-missing')).toBe(false)
  })

  it('restores the image if it later loads successfully', async () => {
    // The file may be created after the message rendered (a turn names a file
    // before writing it, or the user fixes it). useMediaWatch bumps the ?t=
    // version and the browser retries — the fallback must then stand down.
    document.body.innerHTML = figure('/api/fs/raw/later.png')
    const img = document.querySelector('img') as HTMLImageElement
    applyMediaMissingFallback(img)

    img.dispatchEvent(new Event('load'))

    expect(document.querySelector(`.${MEDIA_MISSING_CLASS}`)).toBeNull()
    expect(img.classList.contains('local-media-hidden')).toBe(false)
    expect(img.hasAttribute('data-media-missing')).toBe(false)
    expect(document.querySelector('.image-block-wrapper')!.classList.contains('image-block-missing')).toBe(false)
  })

  it('escapes a file name that looks like markup', () => {
    // The placeholder is built with textContent, never innerHTML, so a crafted
    // src cannot inject nodes into the conversation.
    document.body.innerHTML = figure('/api/fs/raw/%3Cimg%20src%3Dx%20onerror%3Dalert(1)%3E.png')
    const img = document.querySelector('img') as HTMLImageElement
    applyMediaMissingFallback(img)

    const ph = document.querySelector(`.${MEDIA_MISSING_CLASS}`)!
    expect(ph.querySelector('img')).toBeNull()
    expect(ph.textContent).toContain('<img src=x onerror=alert(1)>.png')
  })
})

describe('installLocalMediaFallback', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    uninstallLocalMediaFallback()
  })
  afterEach(() => {
    uninstallLocalMediaFallback()
  })

  it('catches a resource error on a content image (capture phase)', () => {
    // Resource errors do not bubble, so the listener must be installed in the
    // capture phase; a bubbling one would never fire.
    installLocalMediaFallback()
    document.body.innerHTML = figure('/api/fs/raw/missing.png')
    const img = document.querySelector('img') as HTMLImageElement

    img.dispatchEvent(new Event('error'))

    expect(document.querySelector(`.${MEDIA_MISSING_CLASS}`)).not.toBeNull()
  })

  it('ignores an <img> that is not content media (no lightbox-img marker)', () => {
    // The file manager's own thumbnails have a component-level @error that
    // falls back to a type icon; two fallbacks must not fight over them.
    installLocalMediaFallback()
    document.body.innerHTML = '<img src="/api/fs/thumb?target=a.png&w=80">'
    const img = document.querySelector('img') as HTMLImageElement

    img.dispatchEvent(new Event('error'))

    expect(document.querySelector(`.${MEDIA_MISSING_CLASS}`)).toBeNull()
  })

  it('is idempotent and uninstall stops handling', () => {
    const first = installLocalMediaFallback()
    installLocalMediaFallback() // second install must be a no-op

    document.body.innerHTML = figure('/api/fs/raw/a.png')
    const img = document.querySelector('img') as HTMLImageElement
    img.dispatchEvent(new Event('error'))
    expect(document.querySelector(`.${MEDIA_MISSING_CLASS}`)).not.toBeNull()

    // After teardown a new failure must not be handled.
    first()
    document.body.innerHTML = figure('/api/fs/raw/b.png')
    ;(document.querySelector('img') as HTMLImageElement).dispatchEvent(new Event('error'))
    expect(document.querySelector(`.${MEDIA_MISSING_CLASS}`)).toBeNull()
  })
})
