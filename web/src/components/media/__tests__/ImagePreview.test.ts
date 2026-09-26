import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import ImagePreview from '@/components/media/ImagePreview.vue'
import { readAttachDragData, cleanupDragGhost } from '@/utils/attachDrag'

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

})
