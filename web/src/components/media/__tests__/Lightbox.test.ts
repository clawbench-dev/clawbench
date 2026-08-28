import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import Lightbox from '@/components/media/Lightbox.vue'

// ── Mocks ──

const mockStoreState = {
  currentDir: '/project',
  currentFile: { path: '/project/image.png', name: 'image.png' },
}

let _dirEntries = [
  { name: 'image.png', type: 'file' },
  { name: 'photo.jpg', type: 'file' },
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

vi.mock('@/utils/download.ts', () => ({
  downloadBlob: vi.fn(),
  buildLocalFileUrl: (path: string, opts?: any) => {
    const base = `/api/local-file/${path}`
    return opts?.timestamp ? `${base}?t=1234567890` : base
  },
  downloadFileByPath: vi.fn(),
}))

vi.mock('@/utils/lightbox.ts', () => ({
  extractImageName: (src: string) => {
    try {
      const url = new URL(src, 'http://localhost')
      const path = decodeURIComponent(url.pathname)
      const prefix = '/api/local-file/'
      if (path.startsWith(prefix)) {
        return path.slice(prefix.length).split('/').pop() || ''
      }
      return path.split('/').pop() || ''
    } catch { return '' }
  },
}))

const mockUnregister = vi.fn()
const mockRegisterBackHandler = vi.fn(() => mockUnregister)

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: (...args: any[]) => mockRegisterBackHandler(...args),
  PRIORITY_OVERLAY: 1000,
}))

describe('Lightbox', () => {
  beforeEach(() => {
    mockSelectFile.mockClear()
    mockRegisterBackHandler.mockClear()
    mockUnregister.mockClear()
    mockStoreState.currentDir = '/project'
    mockStoreState.currentFile = { path: '/project/image.png', name: 'image.png' }
    _dirEntries = [
      { name: 'image.png', type: 'file' },
      { name: 'photo.jpg', type: 'file' },
    ]
  })

  function mountLightbox() {
    return mount(Lightbox, {
      attachTo: document.body,
    })
  }

  // ── calcFitScale ──

  describe('calcFitScale', () => {
    it('returns 1 when image fits in viewport', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      // Mock contentRef with a large viewport
      vm.contentRef = { clientWidth: 1920, clientHeight: 1080 }
      const s = vm.calcFitScale(800, 600)
      expect(s).toBe(1)
    })

    it('scales down when image is wider than viewport', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.contentRef = { clientWidth: 400, clientHeight: 800 }
      // 400/2000 = 0.2, (800-112)/1500 = 0.458, min = 0.2
      const s = vm.calcFitScale(2000, 1500)
      expect(s).toBeCloseTo(0.2, 1)
    })

    it('scales down when image is taller than viewport', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.contentRef = { clientWidth: 800, clientHeight: 400 }
      // 800/1000 = 0.8, (400-112)/2000 = 0.144, min = 0.144
      const s = vm.calcFitScale(1000, 2000)
      expect(s).toBeCloseTo(0.144, 2)
    })

    it('returns 1 when dimensions are zero or negative', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.contentRef = { clientWidth: 800, clientHeight: 600 }
      expect(vm.calcFitScale(0, 100)).toBe(1)
      expect(vm.calcFitScale(100, 0)).toBe(1)
      expect(vm.calcFitScale(-10, -10)).toBe(1)
    })

    it('returns 1 when contentRef is null', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.contentRef = null
      expect(vm.calcFitScale(800, 600)).toBe(1)
    })

    it('considers both width and height constraints', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.contentRef = { clientWidth: 500, clientHeight: 500 }
      // 500/1000 = 0.5, (500-112)/1000 = 0.388, min = 0.388
      const s = vm.calcFitScale(1000, 1000)
      expect(s).toBeCloseTo(0.388, 2)
    })
  })

  // ── Drag disabled at fitScale ──

  describe('drag at fitScale', () => {
    it('disables mouse drag when scale equals fitScale', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.open('http://localhost/test.png')
      await nextTick()

      // scale starts at 1, fitScale starts at 1
      expect(vm.scale).toBe(1)
      expect(vm.fitScale).toBe(1)

      vm.handleMouseDown({ button: 0, clientX: 100, clientY: 100, preventDefault: vi.fn() })

      expect(vm.isDragging).toBe(false)
    })

    it('disables touch drag when scale equals fitScale', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.open('http://localhost/test.png')
      await nextTick()

      expect(vm.scale).toBe(1)

      vm.handleTouchStart({
        touches: [{ clientX: 100, clientY: 100, clientX: 100, clientY: 100 }],
        length: 1,
      })

      expect(vm.isDragging).toBe(false)
    })

    it('enables mouse drag when zoomed beyond fitScale', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.open('http://localhost/test.png')
      await nextTick()

      vm.fitScale = 0.5
      vm.scale = 1.0

      vm.handleMouseDown({ button: 0, clientX: 100, clientY: 100, preventDefault: vi.fn() })

      expect(vm.isDragging).toBe(true)
    })

    it('enables touch drag when zoomed beyond fitScale', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.open('http://localhost/test.png')
      await nextTick()

      vm.fitScale = 0.5
      vm.scale = 1.0

      vm.handleTouchStart({
        touches: [{ clientX: 100, clientY: 100, clientX: 100, clientY: 100 }],
        length: 1,
      })

      expect(vm.isDragging).toBe(true)
    })
  })

  // ── fitScale and onImageLoad ──

  describe('onImageLoad', () => {
    it('sets fitScale < 1 for large images', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      // Mock contentRef with a small viewport
      vm.contentRef = { clientWidth: 400, clientHeight: 300 }
      vm.imgRef = { naturalWidth: 2000, naturalHeight: 1500 }

      vm.onImageLoad()

      expect(vm.naturalW).toBe(2000)
      expect(vm.naturalH).toBe(1500)
      expect(vm.fitScale).toBeLessThan(1)
      expect(vm.scale).toBe(vm.fitScale)
      expect(vm.dimensionsReady).toBe(true)
    })

    it('keeps fitScale=1 for images that fit', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.contentRef = { clientWidth: 1920, clientHeight: 1080 }
      vm.imgRef = { naturalWidth: 800, naturalHeight: 600 }

      vm.onImageLoad()

      expect(vm.fitScale).toBe(1)
      expect(vm.scale).toBe(1)
      expect(vm.dimensionsReady).toBe(true)
    })
  })

  // ── resetAndRefresh ──

  describe('resetAndRefresh', () => {
    it('resets scale and dimensions', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 2
      vm.naturalW = 2000
      vm.naturalH = 1500
      vm.dimensionsReady = true
      vm.tx = 100
      vm.ty = 50
      vm.lastTx = 100
      vm.lastTy = 50

      vm.resetAndRefresh()

      expect(vm.imageLoading).toBe(true)
      expect(vm.scale).toBe(1)
      expect(vm.fitScale).toBe(1)
      expect(vm.naturalW).toBe(0)
      expect(vm.naturalH).toBe(0)
      expect(vm.dimensionsReady).toBe(false)
      expect(vm.tx).toBe(0)
      expect(vm.ty).toBe(0)
      expect(vm.lastTx).toBe(0)
      expect(vm.lastTy).toBe(0)
    })

    it('normalizes a URL that already carries a t= param (file-manager source)', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      // ImagePreview.mediaUrl already appends ?t=<mediaTimestamp>; chat passes
      // a clean data-full-src. Both must end up with a single clean ?t= param.
      vm.open('/api/local-file/a/b.png?t=100')
      await nextTick()

      const url = vm.currentUrl as string
      expect(url).toMatch(/^\/api\/local-file\/a\/b\.png\?t=\d+$/)
      expect(url.split('t=').length).toBe(2)
      expect(url).not.toContain('&t=')
    })

    it('resetAndRefresh never accumulates t= params across repeated calls', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.open('/api/local-file/a/b.png?t=100')
      await nextTick()

      // Simulate a source that already carried a cache-buster, then refresh twice.
      // Each refresh must yield one clean ?t= (the malformed path&t= case).
      vm.resetAndRefresh()
      const first = vm.currentUrl as string
      expect(first).toMatch(/^\/api\/local-file\/a\/b\.png\?t=\d+$/)
      expect(first).not.toContain('&')

      vm.resetAndRefresh()
      const second = vm.currentUrl as string
      expect(second).toMatch(/^\/api\/local-file\/a\/b\.png\?t=\d+$/)
      expect(second).not.toContain('&')
    })

    it('normalizes timestamped src in md navigation', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const imgs = [
        { src: 'http://localhost/a.png?t=111', name: 'a.png' },
        { src: 'http://localhost/b.png?t=222', name: 'b.png' },
      ]
      vm.openMdImages(imgs, 0)
      await nextTick()

      const url = vm.currentUrl as string
      expect(url).toMatch(/^http:\/\/localhost\/a\.png\?t=\d+$/)
      expect(url).not.toContain('&')

      vm.navigateNext()
      await nextTick()
      const nextUrl = vm.currentUrl as string
      expect(nextUrl).toMatch(/^http:\/\/localhost\/b\.png\?t=\d+$/)
      expect(nextUrl).not.toContain('&')
    })
  })

  // ── Wheel zoom respects fitScale ──

  describe('handleWheel', () => {
    it('resets pan when zooming below fitScale', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 0.5
      vm.tx = 100
      vm.ty = 50
      vm.lastTx = 100
      vm.lastTy = 50

      // Zoom out further (deltaY > 0 → 0.85 multiplier)
      vm.handleWheel({ deltaY: 100 })
      // 0.5 * 0.85 = 0.425, which is < fitScale (0.5)
      expect(vm.scale).toBeLessThan(vm.fitScale)
      expect(vm.tx).toBe(0)
      expect(vm.ty).toBe(0)
    })

    it('does not reset pan when zooming above fitScale', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 1.0
      vm.tx = 100
      vm.ty = 50
      vm.lastTx = 100
      vm.lastTy = 50

      // Zoom in (deltaY < 0 → 1.2 multiplier)
      vm.handleWheel({ deltaY: -100 })
      expect(vm.scale).toBeGreaterThan(1)
      expect(vm.tx).toBe(100)
      expect(vm.ty).toBe(50)
    })
  })

  // ── Touch end snaps back to fitScale ──

  describe('handleTouchEnd snap back', () => {
    it('snaps back to fitScale when zoomed below it', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 0.3
      vm.tx = 50
      vm.ty = 30

      vm.handleTouchEnd(new TouchEvent('touchend'))

      expect(vm.scale).toBe(0.5)
      expect(vm.tx).toBe(0)
      expect(vm.ty).toBe(0)
    })

    it('preserves position when at or above fitScale', () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 1.0
      vm.tx = 50
      vm.ty = 30

      vm.handleTouchEnd(new TouchEvent('touchend'))

      expect(vm.scale).toBe(1.0)
      expect(vm.lastTx).toBe(50)
      expect(vm.lastTy).toBe(30)
    })
  })

  // ── Basic rendering ──

  it('renders lightbox container (teleported to body)', () => {
    mountLightbox()
    expect(document.querySelector('.lightbox')).toBeTruthy()
  })

  it('is hidden by default', () => {
    mountLightbox()
    const el = document.querySelector('.lightbox') as HTMLElement
    expect(el?.style.display).toBe('none')
  })

  it('shows lightbox when open is called', async () => {
    const wrapper = mountLightbox()
    const vm = wrapper.vm as any
    vm.open('http://localhost/test.png')
    await nextTick()
    // Check via the reactive state, not DOM (Teleport makes DOM assertions unreliable)
    expect(vm.lightboxVisible).toBe(true)
  })

  // ── Back handler registration (edge swipe / Android back) ──

  describe('back handler', () => {
    it('registers back handler when opened', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      expect(mockRegisterBackHandler).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'lightbox',
          priority: 1000,
        }),
      )
    })

    it('back handler goBack closes the lightbox', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      const handler = mockRegisterBackHandler.mock.calls[0][0]
      expect(handler.canGoBack()).toBe(true)
      handler.goBack()
      expect(vm.lightboxVisible).toBe(false)
    })

    it('unregisters back handler when closed', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      expect(mockRegisterBackHandler).toHaveBeenCalled()

      vm.close()
      await nextTick()

      expect(mockUnregister).toHaveBeenCalled()
    })
  })

  // ── Navigation ──

  describe('navigation', () => {
    it('shows navigation when multiple image siblings exist', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      expect(vm.showNav).toBe(true)
      expect(vm.navTotalCount).toBe(2)
    })

    it('hides navigation when only one image sibling', async () => {
      _dirEntries = [{ name: 'image.png', type: 'file' }]
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      expect(vm.showNav).toBe(false)
    })

    it('navigateNext advances to next image and calls selectFile', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.navigateNext()
      expect(vm.slideDirection).toBe('left')
      expect(vm.imageLoading).toBe(true)
      expect(mockSelectFile).toHaveBeenCalled()
    })

    it('navigatePrev goes to previous image', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.navigatePrev()
      expect(vm.slideDirection).toBe('right')
      expect(vm.imageLoading).toBe(true)
    })

    it('navigateNext does nothing when showNav is false', async () => {
      _dirEntries = [{ name: 'image.png', type: 'file' }]
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      const prevUrl = vm.currentUrl
      vm.navigateNext()
      expect(vm.currentUrl).toBe(prevUrl)
    })
  })

  // ── imgStyle computed ──

  describe('imgStyle', () => {
    it('includes width/height/maxWidth/maxHeight when dimensionsReady and no svg', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.dimensionsReady = true
      vm.naturalW = 1000
      vm.naturalH = 800
      vm.currentSvg = ''

      const style = vm.imgStyle
      expect(style.width).toBe('1000px')
      expect(style.height).toBe('800px')
      expect(style.maxWidth).toBe('none')
      expect(style.maxHeight).toBe('none')
    })

    it('does not include explicit dimensions when not ready', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.dimensionsReady = false
      const style = vm.imgStyle
      expect(style.width).toBeUndefined()
    })

    it('does not include explicit dimensions when svg is present', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('', '<svg></svg>')
      await nextTick()

      vm.dimensionsReady = true
      vm.naturalW = 1000
      vm.naturalH = 800
      const style = vm.imgStyle
      expect(style.width).toBeUndefined()
    })
  })

  // ── handleContentClick ──

  describe('handleContentClick', () => {
    it('closes lightbox when clicking on contentRef', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.handleContentClick({ target: vm.contentRef })
      expect(vm.lightboxVisible).toBe(false)
    })

    it('does not close when clicking on image itself', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      // Simulate clicking on the image element (not contentRef or loading spinner)
      const fakeTarget = document.createElement('img')
      vm.handleContentClick({ target: fakeTarget })
      expect(vm.lightboxVisible).toBe(true)
    })
  })

  // ── openMdImages ──

  describe('openMdImages', () => {
    it('opens markdown image navigation', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const imgs = [
        { src: 'http://localhost/a.png', name: 'a.png' },
        { src: 'http://localhost/b.png', name: 'b.png' },
      ]
      vm.openMdImages(imgs, 0)
      await nextTick()

      expect(vm.lightboxVisible).toBe(true)
      expect(vm.mdCurrentIndex).toBe(0)
      expect(vm.mdImages).toHaveLength(2)
      expect(vm.showNav).toBe(true)
    })

    it('navigateNext/Prev works in md mode', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const imgs = [
        { src: 'http://localhost/a.png', name: 'a.png' },
        { src: 'http://localhost/b.png', name: 'b.png' },
      ]
      vm.openMdImages(imgs, 0)
      await nextTick()

      vm.navigateNext()
      expect(vm.mdCurrentIndex).toBe(1)

      vm.navigatePrev()
      expect(vm.mdCurrentIndex).toBe(0)
    })

    it('md navigation does nothing when only one image', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const imgs = [{ src: 'http://localhost/a.png', name: 'a.png' }]
      vm.openMdImages(imgs, 0)
      await nextTick()

      vm.navigateNext()
      expect(vm.mdCurrentIndex).toBe(0)
    })
  })

  // ── Swipe navigation ──

  describe('swipe navigation', () => {
    it('swipe left triggers navigateNext', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.fitScale = 1
      vm.scale = 1
      vm.touchStartX = 200
      vm.touchLastX = 100
      vm.touchStartY = 150
      vm.touchLastY = 150
      vm.hasMoved = false

      vm.handleTouchEnd(new TouchEvent('touchend'))
      // dx=100 > 50, dx > dy → navigateNext
      expect(vm.slideDirection).toBe('left')
    })

    it('swipe right triggers navigatePrev', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.fitScale = 1
      vm.scale = 1
      vm.touchStartX = 100
      vm.touchLastX = 200
      vm.touchStartY = 150
      vm.touchLastY = 150
      vm.hasMoved = false

      vm.handleTouchEnd(new TouchEvent('touchend'))
      expect(vm.slideDirection).toBe('right')
    })

    it('no swipe when hasMoved is true', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.fitScale = 1
      vm.scale = 1
      vm.touchStartX = 200
      vm.touchLastX = 100
      vm.touchStartY = 150
      vm.touchLastY = 150
      vm.hasMoved = true

      vm.handleTouchEnd(new TouchEvent('touchend'))
      expect(vm.slideDirection).toBe('')
    })
  })

  // ── handleDownload ──

  describe('handleDownload', () => {
    it('downloads SVG content as blob', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('', '<svg></svg>')
      await nextTick()

      vm.handleDownload()
      // downloadBlob should have been called (imported from mock)
    })

    it('downloads file by path when filePath is set', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      vm.open('http://localhost/test.png')
      await nextTick()

      vm.currentFilePath = '/project/image.png'
      vm.handleDownload()
    })

    it('does nothing when no url and no svg', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.currentUrl = ''
      vm.currentSvg = ''
      vm.currentFilePath = ''

      vm.handleDownload()
      // Should not throw
    })
  })

  // ── open with SVG ──

  describe('open with SVG', () => {
    it('opens SVG and does not build sibling list', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      // Use openSvg which goes through open() → sets currentSvg → watch triggers onSvgMounted
      // jsdom SVG has viewBox.baseVal but no getBBox, so provide viewBox to avoid fallback
      vm.openSvg('<svg viewBox="0 0 100 100" width="100" height="100"><rect></rect></svg>')
      await nextTick()

      expect(vm.lightboxVisible).toBe(true)
      expect(vm.currentSvg).toBeTruthy()
      expect(vm.currentUrl).toBe('')
      expect(vm.siblingFiles).toHaveLength(0)
      expect(vm.currentIndex).toBe(-1)
    })
  })

  // ── onImageLoad edge cases ──

  describe('onImageLoad edge cases', () => {
    it('does nothing when imgRef is null', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.imgRef = null
      vm.onImageLoad()

      expect(vm.naturalW).toBe(0)
      expect(vm.naturalH).toBe(0)
    })
  })

  // ── Mouse drag ──

  describe('mouse drag', () => {
    it('ignores non-left click', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.handleMouseDown({ button: 2, clientX: 100, clientY: 100, preventDefault: vi.fn() })
      expect(vm.isDragging).toBe(false)
    })

    it('moves image on mouse move when dragging', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 1.0
      vm.handleMouseDown({ button: 0, clientX: 100, clientY: 100, preventDefault: vi.fn() })
      vm.handleMouseMove({ clientX: 150, clientY: 120, preventDefault: vi.fn() })

      expect(vm.tx).toBe(50)
      expect(vm.ty).toBe(20)
    })

    it('does not move on mousemove when not dragging', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.handleMouseMove({ clientX: 150, clientY: 120, preventDefault: vi.fn() })
      expect(vm.tx).toBe(0)
    })

    it('saves last position on mouseup', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 1.0
      vm.handleMouseDown({ button: 0, clientX: 100, clientY: 100, preventDefault: vi.fn() })
      vm.handleMouseMove({ clientX: 150, clientY: 120, preventDefault: vi.fn() })
      vm.handleMouseUp()

      expect(vm.isDragging).toBe(false)
      expect(vm.lastTx).toBe(50)
      expect(vm.lastTy).toBe(20)
    })
  })

  // ── Touch drag ──

  describe('touch drag', () => {
    it('handles single touch drag', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.fitScale = 0.5
      vm.scale = 1.0

      vm.handleTouchStart({
        touches: [{ clientX: 100, clientY: 100 }],
        length: 1,
      })

      expect(vm.isDragging).toBe(true)
      expect(vm.hasMoved).toBe(false)

      vm.handleTouchMove({
        touches: [{ clientX: 120, clientY: 110 }],
        length: 1,
        preventDefault: vi.fn(),
      })

      expect(vm.hasMoved).toBe(true)
    })

    it('handles pinch zoom start', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.handleTouchStart({
        touches: [
          { clientX: 100, clientY: 100 },
          { clientX: 200, clientY: 200 },
        ],
        length: 2,
      })

      expect(vm.isDragging).toBe(false)
      expect(vm.pinchStartDist).toBeGreaterThan(0)
    })

    it('handles pinch zoom move', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      vm.pinchStartDist = 100
      vm.pinchStartScale = 1

      vm.handleTouchMove({
        touches: [
          { clientX: 50, clientY: 50 },
          { clientX: 250, clientY: 250 },
        ],
        length: 2,
        preventDefault: vi.fn(),
      })

      // Distance is ~282, ratio ~2.82, scale should be ~2.82
      expect(vm.scale).toBeGreaterThan(1)
    })
  })

  // ── collectMdImages ──

  describe('collectMdImages', () => {
    it('collects images from a container', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML = '<img src="a.png" alt="A"><img src="b.png" alt="B">'
      document.body.appendChild(container)

      const result = vm.collectMdImages(container, container.querySelectorAll('img')[1], null)
      expect(result.list).toHaveLength(2)
      expect(result.startIdx).toBe(1)

      document.body.removeChild(container)
    })

    it('prefers the data-full-src original over the thumbnail src', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<img src="/api/file/thumb?path=photo.png&w=1200" data-full-src="/api/local-file/photo.png" alt="A">' +
        '<img src="/api/file/thumb?path=photo.jpg&w=1200" data-full-src="/api/local-file/photo.jpg" alt="B">'
      document.body.appendChild(container)

      const result = vm.collectMdImages(container, container.querySelectorAll('img')[1], null)
      expect(result.list[0].src).toBe('/api/local-file/photo.png')
      expect(result.list[1].src).toBe('/api/local-file/photo.jpg')
      expect(result.list[0].src).not.toContain('/api/file/thumb')
      expect(result.list[1].src).not.toContain('/api/file/thumb')

      document.body.removeChild(container)
    })

    it('falls back to img.src when there is no data-full-src', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML = '<img src="https://ext.com/a.png" alt="A">'
      document.body.appendChild(container)

      const result = vm.collectMdImages(container, container.querySelectorAll('img')[0], null)
      expect(result.list[0].src).toBe('https://ext.com/a.png')

      document.body.removeChild(container)
    })

    it('collects mermaid SVGs alongside images in document order', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<img src="a.png" alt="Image A">' +
        '<div class="mermaid"><svg viewBox="0 0 100 50"><rect></rect></svg></div>' +
        '<img src="b.png" alt="Image B">'
      document.body.appendChild(container)

      const mermaidDiv = container.querySelector('.mermaid')
      const result = vm.collectMdImages(container, null, mermaidDiv)
      expect(result.list).toHaveLength(3)
      expect(result.list[0].src).toBeTruthy()
      expect(result.list[0].name).toBe('Image A')
      expect(result.list[1].svg).toContain('<svg')
      expect(result.list[1].src).toBe('')
      expect(result.list[2].src).toBeTruthy()
      expect(result.list[2].name).toBe('Image B')
      expect(result.startIdx).toBe(1) // clicked mermaid is index 1

      document.body.removeChild(container)
    })

    it('sets correct startIdx when clicking an image with mermaid siblings', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<div class="mermaid"><svg viewBox="0 0 100 50"><rect></rect></svg></div>' +
        '<img src="a.png" alt="A">' +
        '<img src="b.png" alt="B">'
      document.body.appendChild(container)

      const clickedImg = container.querySelectorAll('img')[1]
      const result = vm.collectMdImages(container, clickedImg, null)
      expect(result.list).toHaveLength(3)
      expect(result.startIdx).toBe(2) // b.png is index 2 (mermaid=0, a.png=1, b.png=2)

      document.body.removeChild(container)
    })

    it('skips mermaid divs without SVG child', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<img src="a.png" alt="A">' +
        '<div class="mermaid"></div>' // no SVG inside
      document.body.appendChild(container)

      const result = vm.collectMdImages(container, container.querySelectorAll('img')[0], null)
      expect(result.list).toHaveLength(1)
      expect(result.list[0].src).toBeTruthy()

      document.body.removeChild(container)
    })

    it('collects inline lightbox-svg alongside images in document order', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<img src="a.png" alt="Image A">' +
        '<span class="lightbox-svg-wrap"><svg class="lightbox-svg" viewBox="0 0 100 50"><rect></rect></svg><span class="lightbox-expand-icon"></span></span>' +
        '<img src="b.png" alt="Image B">'
      document.body.appendChild(container)

      const clickedSvg = container.querySelector('svg.lightbox-svg')
      const result = vm.collectMdImages(container, null, null, clickedSvg)
      expect(result.list).toHaveLength(3)
      expect(result.list[0].src).toBeTruthy()
      expect(result.list[0].name).toBe('Image A')
      expect(result.list[1].svg).toContain('<svg')
      expect(result.list[1].src).toBe('')
      expect(result.list[2].src).toBeTruthy()
      expect(result.startIdx).toBe(1) // clicked svg is index 1

      document.body.removeChild(container)
    })

    it('uses data-name attribute for inline svg name', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<svg class="lightbox-svg" data-name="chart.svg" viewBox="0 0 10 10"><rect></rect></svg>'
      document.body.appendChild(container)

      const clickedSvg = container.querySelector('svg.lightbox-svg')
      const result = vm.collectMdImages(container, null, null, clickedSvg)
      expect(result.list[0].name).toBe('chart.svg')

      document.body.removeChild(container)
    })
  })

  // ── deriveMermaidName ──

  describe('deriveMermaidName', () => {
    it('returns heading text when preceded by H2', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<h2>System Architecture</h2>' +
        '<div class="mermaid"><svg></svg></div>'
      document.body.appendChild(container)

      const mermaidDiv = container.querySelector('.mermaid')
      const name = vm.deriveMermaidName(mermaidDiv)
      expect(name).toBe('System Architecture')

      document.body.removeChild(container)
    })

    it('returns heading text when heading is 2 siblings before', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<h3>Data Flow</h3>' +
        '<p>Some description</p>' +
        '<div class="mermaid"><svg></svg></div>'
      document.body.appendChild(container)

      const mermaidDiv = container.querySelector('.mermaid')
      const name = vm.deriveMermaidName(mermaidDiv)
      expect(name).toBe('Data Flow')

      document.body.removeChild(container)
    })

    it('returns diagram.svg when no heading within 3 siblings', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const container = document.createElement('div')
      container.innerHTML =
        '<p>Para 1</p>' +
        '<p>Para 2</p>' +
        '<p>Para 3</p>' +
        '<p>Para 4</p>' +
        '<div class="mermaid"><svg></svg></div>'
      document.body.appendChild(container)

      const mermaidDiv = container.querySelector('.mermaid')
      const name = vm.deriveMermaidName(mermaidDiv)
      expect(name).toBe('diagram.svg')

      document.body.removeChild(container)
    })

    it('truncates heading text over 40 chars', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const longTitle = 'A'.repeat(60)
      const container = document.createElement('div')
      container.innerHTML =
        `<h2>${longTitle}</h2>` +
        '<div class="mermaid"><svg></svg></div>'
      document.body.appendChild(container)

      const mermaidDiv = container.querySelector('.mermaid')
      const name = vm.deriveMermaidName(mermaidDiv)
      expect(name).toHaveLength(40)
      expect(name).toBe('A'.repeat(40))

      document.body.removeChild(container)
    })
  })

  // ── data-full-src (thumbnail inline, full-size in lightbox) ──

  describe('data-full-src', () => {
    it('navigateMdImage uses pre-resolved src for plain objects', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      // collectMdImages pre-resolves data-full-src into src
      vm.mdImages = [{ src: '/api/local-file/photo.png', name: 'photo.png' }]

      vm.navigateMdImage(0, 'next')
      await nextTick()

      expect(vm.currentUrl).toContain('/api/local-file/photo.png')
    })

    it('openMdImages uses pre-resolved src for plain objects', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any
      // collectMdImages pre-resolves data-full-src into src
      const imgs = [{ src: '/api/local-file/photo.jpg', name: 'photo.jpg' }]

      vm.openMdImages(imgs, 0)
      await nextTick()

      expect(vm.currentUrl).toContain('/api/local-file/photo.jpg')
    })
  })

  // ── SVG navigation in md mode ──

  describe('SVG navigation in md mode', () => {
    it('openMdImages sets currentSvg for SVG items', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const svgContent = '<svg viewBox="0 0 100 50"><rect></rect></svg>'
      const imgs = [
        { src: 'http://localhost/a.png', name: 'a.png' },
        { src: '', name: 'diagram.svg', svg: svgContent },
      ]
      vm.openMdImages(imgs, 1)
      await nextTick()

      expect(vm.currentSvg).toBe(svgContent)
      expect(vm.currentUrl).toBe('')
      expect(vm.imageLoading).toBe(false)
    })

    it('openMdImages sets currentUrl for non-SVG items', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const imgs = [
        { src: 'http://localhost/a.png', name: 'a.png' },
        { src: '', name: 'diagram.svg', svg: '<svg></svg>' },
      ]
      vm.openMdImages(imgs, 0)
      await nextTick()

      expect(vm.currentUrl).toContain('http://localhost/a.png')
      expect(vm.currentSvg).toBe('')
    })

    it('navigateMdImage switches from image to SVG', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const svgContent = '<svg viewBox="0 0 100 50"><rect></rect></svg>'
      const imgs = [
        { src: 'http://localhost/a.png', name: 'a.png' },
        { src: '', name: 'diagram.svg', svg: svgContent },
      ]
      vm.openMdImages(imgs, 0)
      await nextTick()

      vm.navigateNext()
      expect(vm.mdCurrentIndex).toBe(1)
      expect(vm.currentSvg).toBe(svgContent)
      expect(vm.currentUrl).toBe('')
    })

    it('navigateMdImage switches from SVG to image', async () => {
      const wrapper = mountLightbox()
      const vm = wrapper.vm as any

      const svgContent = '<svg viewBox="0 0 100 50"><rect></rect></svg>'
      const imgs = [
        { src: 'http://localhost/a.png', name: 'a.png' },
        { src: '', name: 'diagram.svg', svg: svgContent },
      ]
      vm.openMdImages(imgs, 1)
      await nextTick()

      vm.navigatePrev()
      expect(vm.mdCurrentIndex).toBe(0)
      expect(vm.currentUrl).toContain('http://localhost/a.png')
      expect(vm.currentSvg).toBe('')
    })
  })
})
