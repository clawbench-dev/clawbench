import { describe, expect, it, beforeEach } from 'vitest'
import {
  ATTACH_DRAG_MIME,
  buildAttachDragImage,
  cleanupDragGhost,
} from '@/utils/attachDrag'
import {
  isApiServedSrc,
  resolveMdImageDragTarget,
  onMdImageDragStart,
  onMdImageDragEnd,
} from '@/utils/mdImageDrag'

function makeDragEvent(target: EventTarget | null): DragEvent {
  return { target } as unknown as DragEvent
}

function mockDataTransfer() {
  const store: Record<string, string> = {}
  const types: string[] = []
  return {
    setData(type: string, value: string) {
      store[type] = value
      if (!types.includes(type)) types.push(type)
    },
    getData(type: string) {
      return store[type] ?? ''
    },
    get types() {
      return Object.freeze([...types])
    },
    effectAllowed: '',
    setDragImage: () => {},
  } as unknown as DataTransfer
}

/** Build an <img> with optional lightbox class / attach data / src. */
function makeImg(attrs: { cls?: string; attachSrc?: string | null; src?: string }): HTMLImageElement {
  const img = document.createElement('img')
  if (attrs.cls) img.className = attrs.cls
  if (attrs.attachSrc) img.setAttribute('data-attach-src', attrs.attachSrc)
  if (attrs.src) img.setAttribute('src', attrs.src)
  return img
}

describe('isApiServedSrc', () => {
  it('true for /api/local-file/ and /api/file/ srcs', () => {
    expect(isApiServedSrc(makeImg({ src: '/api/local-file/docs/a.png?t=1' }))).toBe(true)
    expect(isApiServedSrc(makeImg({ src: '/api/file/thumb?path=docs/a.png&w=1200' }))).toBe(true)
  })

  it('false for external, data: and empty srcs', () => {
    expect(isApiServedSrc(makeImg({ src: 'https://x.com/a.png' }))).toBe(false)
    expect(isApiServedSrc(makeImg({ src: 'data:image/png;base64,abc' }))).toBe(false)
    expect(isApiServedSrc(makeImg({ src: '' }))).toBe(false)
  })
})

describe('resolveMdImageDragTarget', () => {
  it('returns null when the target is not an <img>', () => {
    const div = document.createElement('div')
    expect(resolveMdImageDragTarget(makeDragEvent(div))).toBeNull()
    expect(resolveMdImageDragTarget(makeDragEvent(null))).toBeNull()
  })

  it('returns null for a bare img with no local source (external src)', () => {
    const img = makeImg({ src: 'https://x.com/a.png' })
    expect(resolveMdImageDragTarget(makeDragEvent(img))).toBeNull()
  })

  it('resolves the decoded data-attach-src path', () => {
    const img = makeImg({ attachSrc: 'a/c/图 d.png', src: '/api/file/thumb?path=a/c/%E5%9B%BE%20d.png&w=1200' })
    const hit = resolveMdImageDragTarget(makeDragEvent(img))
    expect(hit).toEqual({ img, path: 'a/c/图 d.png' })
  })

  it('resolves a non-CJK data-attach-src verbatim', () => {
    const img = makeImg({ attachSrc: 'docs/assets/img.png', src: '/api/local-file/docs/assets/img.png?t=1' })
    expect(resolveMdImageDragTarget(makeDragEvent(img))).toEqual({ img, path: 'docs/assets/img.png' })
  })

  it('falls back to reverse-deriving /api/local-file/ srcs', () => {
    const img = makeImg({ src: '/api/local-file/docs/sub/img.png?t=42' })
    expect(resolveMdImageDragTarget(makeDragEvent(img))).toEqual({ img, path: 'docs/sub/img.png' })
  })

  it('does not reverse-derive thumbnail srcs without data-attach-src', () => {
    // Thumbnail URLs use a query-param path form that is not the original
    // relative file path — only the explicit attribute (or a full-size
    // /api/local-file/ URL) can be trusted.
    const img = makeImg({ src: '/api/file/thumb?path=docs/a.png&w=1200' })
    expect(resolveMdImageDragTarget(makeDragEvent(img))).toBeNull()
  })

  it('decodes percent-encoded path segments in the fallback', () => {
    const img = makeImg({ src: '/api/local-file/a/c/%E5%9B%BE%20d.png?t=7' })
    expect(resolveMdImageDragTarget(makeDragEvent(img))).toEqual({ img, path: 'a/c/图 d.png' })
  })
})

describe('onMdImageDragStart', () => {
  beforeEach(() => cleanupDragGhost())

  it('writes the attach payload and stops propagation for a local md image', () => {
    const img = makeImg({ cls: 'lightbox-img', attachSrc: 'docs/a.png', src: '/api/local-file/docs/a.png?t=1' })
    const dt = mockDataTransfer()
    let stopCalled = false
    const e = {
      target: img,
      dataTransfer: dt,
      stopPropagation: () => { stopCalled = true },
    } as unknown as DragEvent
    onMdImageDragStart(e)
    expect(stopCalled).toBe(true)
    expect(dt.getData(ATTACH_DRAG_MIME)).toBe('{"path":"docs/a.png","isDir":false}')
    expect(dt.getData('text/plain')).toBe('docs/a.png')
  })

  it('uses the image file name (last path segment) for the ghost', () => {
    const img = makeImg({ cls: 'lightbox-img', attachSrc: 'very/deep/nested/image-name.png' })
    const dt = mockDataTransfer()
    onMdImageDragStart({ target: img, dataTransfer: dt, stopPropagation: () => {} } as unknown as DragEvent)
    expect(dt.getData('text/plain')).toBe('very/deep/nested/image-name.png')
  })

  it('does nothing for a non-local (external) image', () => {
    const img = makeImg({ src: 'https://x.com/a.png' })
    let stopCalled = false
    const dt = mockDataTransfer()
    onMdImageDragStart({
      target: img,
      dataTransfer: dt,
      stopPropagation: () => { stopCalled = true },
    } as unknown as DragEvent)
    expect(stopCalled).toBe(false)
    expect(dt.getData(ATTACH_DRAG_MIME)).toBe('')
  })

  it('does nothing when the target is not an image element', () => {
    const div = document.createElement('div')
    const dt = mockDataTransfer()
    onMdImageDragStart({ target: div, dataTransfer: dt, stopPropagation: () => {} } as unknown as DragEvent)
    expect(dt.getData(ATTACH_DRAG_MIME)).toBe('')
  })
})

describe('onMdImageDragEnd', () => {
  beforeEach(() => cleanupDragGhost())

  it('cleans up the ghost after an image drag', () => {
    // Build a ghost the same way attachDrag does and confirm dragend removes it.
    const ghost = buildAttachDragImage('a.png', false)
    expect(ghost.parentElement).toBe(document.body)
    onMdImageDragEnd({ target: null } as unknown as DragEvent)
    expect(ghost.parentElement).toBeNull()
  })

  it('cleans up the ghost for any dragend (matches delegated container handler)', () => {
    const ghost = buildAttachDragImage('x.png', false)
    expect(ghost.parentElement).toBe(document.body)
    onMdImageDragEnd({ target: document.createElement('div') } as unknown as DragEvent)
    expect(ghost.parentElement).toBeNull()
  })
})
