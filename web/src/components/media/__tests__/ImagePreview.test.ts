import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import ImagePreview from '@/components/media/ImagePreview.vue'
import { readAttachDragData, cleanupDragGhost } from '@/utils/attachDrag'
import { installLocalMediaFallback, uninstallLocalMediaFallback } from '@/utils/localMediaFallback'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (k: string) => k }) }
})

// Drive the draggable gate directly instead of depending on jsdom's viewport.
const mockIsWideScreen = ref(false)
vi.mock('@/composables/useWideScreenLayout.ts', () => ({
  useWideScreenLayout: () => ({ isWideScreen: mockIsWideScreen }),
}))
function _setWideScreenForTest(v: boolean) { mockIsWideScreen.value = v }
function _resetForTest() { mockIsWideScreen.value = false }

// Minimal DataTransfer stand-in: enough for setAttachDragData/readAttachDragData.
function mockDataTransfer(): DataTransfer {
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
    setDragImage: vi.fn(),
    effectAllowed: 'none',
  } as unknown as DataTransfer
}

// ── Mocks ──

// Must define mock state before vi.mock since mocks are hoisted
// Use a factory that references a mutable object
const mockStoreState = {
  currentDir: '/project/src',
  currentFile: { path: '/project/src/image.png', name: 'image.png' },
}

// The component reads store.state.dirEntries directly (not .value), so we
// need dirEntries to be a plain array that we can swap via a getter.
let _dirEntries = [
  { name: 'image.png', type: 'file' },
  { name: 'photo.jpg', type: 'file' },
  { name: 'doc.md', type: 'file' },
  { name: 'pic.gif', type: 'file' },
]

const mockSelectFile = vi.fn()

vi.mock('@/stores/app.ts', () => ({
  store: {
    get state() {
      return {
        currentDir: mockStoreState.currentDir,
        currentFile: mockStoreState.currentFile,
        get dirEntries() { return _dirEntries },
      }
    },
    selectFile: (...args: any[]) => mockSelectFile(...args),
  },
}))

vi.mock('@/utils/path.ts', () => ({
  baseName: (path: string) => {
    const parts = path.split('/')
    return parts[parts.length - 1]
  },
  joinPath: (...parts: string[]) => parts.filter(Boolean).join('/'),
  isAbsolutePath: (path: string) => path.startsWith('/'),
}))

vi.mock('@/utils/fileType.ts', () => ({
  getFileType: (name: string) => ({
    isMarkdown: name.endsWith('.md'),
    isHtml: false,
    isImage: /\.(png|jpg|jpeg|gif|svg|webp)$/i.test(name),
    isAudio: false,
    isVideo: false,
    isPdf: false,
    color: '#000',
  }),
}))

describe('ImagePreview', () => {
  beforeEach(() => {
    mockSelectFile.mockClear()
    mockStoreState.currentDir = '/project/src'
    mockStoreState.currentFile = { path: '/project/src/image.png', name: 'image.png' }
    _dirEntries = [
      { name: 'image.png', type: 'file' },
      { name: 'photo.jpg', type: 'file' },
      { name: 'doc.md', type: 'file' },
      { name: 'pic.gif', type: 'file' },
    ]
  })

  // A dragstart without a matching dragend leaves the 5s ghost safety timer
  // pending; clear it so the suite reports no async leaks.
  afterEach(() => {
    cleanupDragGhost()
    _resetForTest()
  })

  function mountPreview(props = {}) {
    return mount(ImagePreview, {
      props: {
        file: { path: '/project/src/image.png', name: 'image.png' },
        ...props,
      },
      attachTo: document.body,
    })
  }

  // ── Rendering ──

  it('renders container', () => {
    const wrapper = mountPreview()
    expect(wrapper.find('.image-preview-container').exists()).toBe(true)
  })

  it('renders image element with correct src', () => {
    const wrapper = mountPreview()
    const img = wrapper.find('.image-preview-img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('/api/fs/raw/')
    expect(img.attributes('alt')).toBe('image.png')
  })

  // ── Navigation arrows ──

  it('shows prev arrow when not at first image', () => {
    // image.png is first, so no prev arrow
    const wrapper = mountPreview()
    expect(wrapper.find('.img-nav-prev').exists()).toBe(false)
  })

  it('shows next arrow when not at last image', () => {
    // image.png is first, and there are more images after
    const wrapper = mountPreview()
    expect(wrapper.find('.img-nav-next').exists()).toBe(true)
  })

  it('shows prev arrow for middle image', () => {
    const wrapper = mountPreview({
      file: { path: '/project/src/photo.jpg', name: 'photo.jpg' },
    })
    expect(wrapper.find('.img-nav-prev').exists()).toBe(true)
    expect(wrapper.find('.img-nav-next').exists()).toBe(true)
  })

  it('hides next arrow at last image', () => {
    const wrapper = mountPreview({
      file: { path: '/project/src/pic.gif', name: 'pic.gif' },
    })
    expect(wrapper.find('.img-nav-next').exists()).toBe(false)
    expect(wrapper.find('.img-nav-prev').exists()).toBe(true)
  })

  // ── Counter badge ──

  it('shows counter badge when multiple images', () => {
    const wrapper = mountPreview()
    const counter = wrapper.find('.img-counter')
    expect(counter.exists()).toBe(true)
    expect(counter.text()).toContain('1')
    expect(counter.text()).toContain('3') // 3 images (png, jpg, gif)
  })

  it('hides counter badge for single image', () => {
    _dirEntries = [
      { name: 'image.png', type: 'file' },
      { name: 'doc.md', type: 'file' },
    ]
    const wrapper = mountPreview()
    expect(wrapper.find('.img-counter').exists()).toBe(false)
  })

  it('updates counter when switching to different image', () => {
    const wrapper = mountPreview({
      file: { path: '/project/src/photo.jpg', name: 'photo.jpg' },
    })
    const counter = wrapper.find('.img-counter')
    expect(counter.text()).toContain('2') // second of 3
  })

  // ── Navigation (goPrev / goNext) ──

  it('calls store.selectFile when clicking next arrow', async () => {
    const wrapper = mountPreview()
    await wrapper.find('.img-nav-next').trigger('click')

    expect(mockSelectFile).toHaveBeenCalled()
  })

  it('calls store.selectFile when clicking prev arrow', async () => {
    const wrapper = mountPreview({
      file: { path: '/project/src/photo.jpg', name: 'photo.jpg' },
    })
    await wrapper.find('.img-nav-prev').trigger('click')

    expect(mockSelectFile).toHaveBeenCalled()
  })

  // ── Keyboard navigation ──

  it('navigates to next on ArrowRight', async () => {
    const wrapper = mountPreview()
    await wrapper.find('.image-preview-container').trigger('keydown', { key: 'ArrowRight' })

    expect(mockSelectFile).toHaveBeenCalled()
  })

  it('navigates to prev on ArrowLeft', async () => {
    const wrapper = mountPreview({
      file: { path: '/project/src/photo.jpg', name: 'photo.jpg' },
    })
    await wrapper.find('.image-preview-container').trigger('keydown', { key: 'ArrowLeft' })

    expect(mockSelectFile).toHaveBeenCalled()
  })

  // ── Sibling image list ──

  it('only includes image files in sibling list', () => {
    const wrapper = mountPreview()
    // The counter shows 3 (png, jpg, gif) - doc.md is excluded
    const counter = wrapper.find('.img-counter')
    expect(counter.text()).toContain('3')
  })

  it('handles empty directory entries', () => {
    _dirEntries = []
    const wrapper = mountPreview()
    expect(wrapper.find('.img-counter').exists()).toBe(false)
    expect(wrapper.find('.img-nav-prev').exists()).toBe(false)
    expect(wrapper.find('.img-nav-next').exists()).toBe(false)
  })

  // ── Cache busting ──

  it('includes timestamp in media URL for cache busting', () => {
    const wrapper = mountPreview()
    const img = wrapper.find('.image-preview-img')
    const src = img.attributes('src')
    expect(src).toMatch(/t=\d+/)
  })

  // ── Drag-to-attach (internal payload, no re-upload) ──

  it('is draggable on wide screens and writes the attach payload on dragstart', async () => {
    _setWideScreenForTest(true)
    const wrapper = mountPreview()
    const img = wrapper.find('.image-preview-img')
    expect(img.attributes('draggable')).toBe('true')

    const dt = mockDataTransfer()
    img.element.dispatchEvent(Object.assign(new Event('dragstart'), { dataTransfer: dt }))
    expect(readAttachDragData(dt)).toEqual({ path: '/project/src/image.png', isDir: false })
    _resetForTest()
  })

  it('is not draggable on narrow screens', () => {
    _setWideScreenForTest(false)
    const wrapper = mountPreview()
    expect(wrapper.find('.image-preview-img').attributes('draggable')).toBe('false')
    _resetForTest()
  })

  it('removes the drag ghost on dragend', () => {
    _setWideScreenForTest(true)
    const wrapper = mountPreview()
    const img = wrapper.find('.image-preview-img')
    img.element.dispatchEvent(Object.assign(new Event('dragstart'), { dataTransfer: mockDataTransfer() }))
    expect(document.querySelector('[data-attach-ghost]')).toBeTruthy()

    img.element.dispatchEvent(new Event('dragend'))
    expect(document.querySelector('[data-attach-ghost]')).toBeNull()
    _resetForTest()
  })

  // ── Zoom & pan ──

  // `trigger()` cannot set clientX/deltaY (jsdom exposes them as getters), so
  // these dispatch real events instead.
  function fireWheel(wrapper: ReturnType<typeof mountPreview>, deltaY: number, clientX = 100, clientY = 100) {
    wrapper.find('.image-preview-body').element.dispatchEvent(
      new WheelEvent('wheel', { deltaY, clientX, clientY, bubbles: true, cancelable: true }),
    )
    return nextTick()
  }

  function zoomIn(wrapper: ReturnType<typeof mountPreview>) {
    return fireWheel(wrapper, -100)
  }

  function fireMouse(target: EventTarget, type: string, clientX = 0, clientY = 0) {
    target.dispatchEvent(new MouseEvent(type, { clientX, clientY, bubbles: true, cancelable: true, button: 0 }))
    return nextTick()
  }

  function transformOf(wrapper: ReturnType<typeof mountPreview>) {
    return (wrapper.find('.image-preview-img').element as HTMLElement).style.transform
  }

  it('zooms in on wheel up and shows the zoom badge', async () => {
    const wrapper = mountPreview()
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(false)

    await zoomIn(wrapper)

    expect(transformOf(wrapper)).toContain('scale(1.15)')
    expect(wrapper.find('.img-zoom-badge').text()).toBe('115%')
  })

  it('zooms back out to the resting size and drops the badge', async () => {
    const wrapper = mountPreview()
    await zoomIn(wrapper)

    await fireWheel(wrapper, 100)

    expect(transformOf(wrapper)).toContain('scale(1)')
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(false)
  })

  it('never zooms out below the resting size', async () => {
    const wrapper = mountPreview()
    await fireWheel(wrapper, 100, 10, 10)

    expect(transformOf(wrapper)).toContain('scale(1)')
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(false)
  })

  it('pans by dragging while zoomed, clamped to the scaled image bounds', async () => {
    const wrapper = mountPreview()
    // At 1.15× a 400×300 image in a 200×200 pane, the pan range is ±130 × ±72.5.
    const img = stubLayout(wrapper)
    await zoomIn(wrapper)

    await fireMouse(img, 'mousedown', 100, 100)
    await fireMouse(window, 'mousemove', 140, 120)
    expect(transformOf(wrapper)).toContain('translate(40px, 20px)')

    // Dragging far past the edge stops at the bound instead of losing the image.
    await fireMouse(window, 'mousemove', 999, 999)
    expect(transformOf(wrapper)).toContain('translate(130px, 72.5px)')

    await fireMouse(window, 'mouseup', 999, 999)
    await fireMouse(window, 'mousemove', 0, 0)
    expect(transformOf(wrapper)).toContain('translate(130px, 72.5px)')
  })

  it('does not pan at the resting size', async () => {
    const wrapper = mountPreview()
    const img = wrapper.find('.image-preview-img').element as HTMLElement
    await fireMouse(img, 'mousedown', 100, 100)
    await fireMouse(window, 'mousemove', 160, 160)

    expect(transformOf(wrapper)).toContain('translate(0px, 0px)')
  })

  it('resets the zoom on Escape, double click and the badge', async () => {
    const wrapper = mountPreview()
    const img = wrapper.find('.image-preview-img').element as HTMLElement

    await zoomIn(wrapper)
    wrapper.find('.image-preview-container').element.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    )
    await nextTick()
    expect(transformOf(wrapper)).toContain('scale(1)')

    await zoomIn(wrapper)
    await fireMouse(img, 'dblclick')
    expect(transformOf(wrapper)).toContain('scale(1)')

    await zoomIn(wrapper)
    await wrapper.find('.img-zoom-badge').trigger('click')
    expect(transformOf(wrapper)).toContain('scale(1)')
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(false)
  })

  it('drops the zoom when navigating to another image', async () => {
    const wrapper = mountPreview()
    await zoomIn(wrapper)
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(true)

    await wrapper.setProps({ file: { path: '/project/src/photo.jpg', name: 'photo.jpg' } })

    expect(transformOf(wrapper)).toContain('scale(1)')
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(false)
  })

  it('gives up native drag-to-attach while zoomed so the drag pans instead', async () => {
    _setWideScreenForTest(true)
    const wrapper = mountPreview()
    expect(wrapper.find('.image-preview-img').attributes('draggable')).toBe('true')

    await zoomIn(wrapper)

    expect(wrapper.find('.image-preview-img').attributes('draggable')).toBe('false')
    _resetForTest()
  })

  // jsdom has no TouchEvent constructor: hand the handler a plain event with
  // the shape it reads.
  function fireTouch(target: EventTarget, type: string, points: Array<[number, number]>) {
    const ev = new Event(type, { bubbles: true, cancelable: true })
    const touches = points.map(([clientX, clientY]) => ({ clientX, clientY }))
    Object.assign(ev, { touches, changedTouches: touches })
    target.dispatchEvent(ev)
    return nextTick()
  }

  function stubLayout(wrapper: ReturnType<typeof mountPreview>) {
    const body = wrapper.find('.image-preview-body').element as HTMLElement
    const img = wrapper.find('.image-preview-img').element as HTMLElement
    Object.defineProperty(body, 'clientWidth', { value: 200, configurable: true })
    Object.defineProperty(body, 'clientHeight', { value: 200, configurable: true })
    Object.defineProperty(img, 'offsetWidth', { value: 400, configurable: true })
    Object.defineProperty(img, 'offsetHeight', { value: 300, configurable: true })
    body.getBoundingClientRect = () => ({ left: 0, top: 0, width: 200, height: 200, right: 200, bottom: 200, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect
    return img
  }

  it('pinch-zooms on touch, then pans with one finger', async () => {
    const wrapper = mountPreview()
    const img = stubLayout(wrapper)

    // Two fingers 40px apart → 80px apart = 2×.
    await fireTouch(img, 'touchstart', [[80, 100], [120, 100]])
    await fireTouch(img, 'touchmove', [[60, 100], [140, 100]])
    expect(transformOf(wrapper)).toContain('scale(2)')

    // Lift one finger: the remaining one becomes a pan handle.
    await fireTouch(img, 'touchend', [[140, 100]])
    await fireTouch(img, 'touchmove', [[180, 130]])
    expect(transformOf(wrapper)).toContain('translate(40px, 30px)')
    expect(transformOf(wrapper)).toContain('scale(2)')

    await fireTouch(img, 'touchend', [])
    await fireTouch(img, 'touchmove', [[0, 0]])
    expect(transformOf(wrapper)).toContain('translate(40px, 30px)')
  })

  it('pinching back to the resting size resets the zoom', async () => {
    const wrapper = mountPreview()
    const img = stubLayout(wrapper)

    await fireTouch(img, 'touchstart', [[80, 100], [120, 100]])
    await fireTouch(img, 'touchmove', [[40, 100], [160, 100]])
    expect(transformOf(wrapper)).toContain('scale(3)')

    await fireTouch(img, 'touchmove', [[95, 100], [105, 100]])
    expect(transformOf(wrapper)).toContain('scale(1)')
    expect(wrapper.find('.img-zoom-badge').exists()).toBe(false)
  })

  it('leaves one-finger touches alone at the resting size', async () => {
    const wrapper = mountPreview()
    const img = stubLayout(wrapper)

    await fireTouch(img, 'touchstart', [[100, 100]])
    await fireTouch(img, 'touchmove', [[180, 150]])

    expect(transformOf(wrapper)).toContain('translate(0px, 0px)')
  })

  // ── Load failure (the full-screen image viewer) ──

  it('degrades a failed image through the shared fallback', async () => {
    // This is the surface from the bug report: the broken-image glyph and its
    // alt text used to sit next to the placeholder, because the hide rule was
    // scoped to the markdown figure only. The <img> carries `lightbox-img`, so
    // the document-level fallback covers it — assert the whole chain, not just
    // the class, so a regression in either half is caught.
    installLocalMediaFallback()
    try {
      const wrapper = mountPreview()
      const img = wrapper.find('.image-preview-img').element as HTMLImageElement

      img.dispatchEvent(new Event('error'))
      await nextTick()

      // Hidden (so no glyph), still present (so a retry can revive it)…
      expect(img.classList.contains('local-media-hidden')).toBe(true)
      expect(document.body.contains(img)).toBe(true)
      // …and explained by the shared element.
      const ph = document.querySelector('.media-load-error')
      expect(ph).not.toBeNull()
      expect(ph!.textContent).toContain('image.png')
    } finally {
      uninstallLocalMediaFallback()
      document.querySelector('.media-load-error')?.remove()
    }
  })
})
