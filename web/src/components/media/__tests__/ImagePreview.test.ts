import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import ImagePreview from '@/components/media/ImagePreview.vue'

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
    expect(img.attributes('src')).toContain('/api/local-file/')
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

  // ── Mouse drag state ──

  it('starts drag on mousedown', async () => {
    const wrapper = mountPreview()
    const body = wrapper.find('.image-preview-body')
    await body.trigger('mousedown', { button: 0, clientX: 100 })

    expect(wrapper.vm.isDragging).toBe(true)
  })

  it('ignores right-click mousedown', async () => {
    const wrapper = mountPreview()
    const body = wrapper.find('.image-preview-body')
    await body.trigger('mousedown', { button: 2, clientX: 100 })

    expect(wrapper.vm.isDragging).toBe(false)
  })

  // ── Touch events ──

  it('starts drag on touchstart', async () => {
    const wrapper = mountPreview()
    const body = wrapper.find('.image-preview-body')
    await body.trigger('touchstart', { touches: [{ clientX: 100 }] })

    expect(wrapper.vm.isDragging).toBe(true)
  })

  it('resets drag offset on touchend', async () => {
    const wrapper = mountPreview()
    const body = wrapper.find('.image-preview-body')
    await body.trigger('touchstart', { touches: [{ clientX: 100 }] })
    await body.trigger('touchend')

    expect(wrapper.vm.isDragging).toBe(false)
    expect(wrapper.vm.dragOffsetX).toBe(0)
  })

  // ── Cleanup ──

  it('removes event listeners on unmount', () => {
    const addSpy = vi.spyOn(document, 'addEventListener')
    const removeSpy = vi.spyOn(document, 'removeEventListener')

    const wrapper = mountPreview()
    expect(addSpy).toHaveBeenCalledWith('mousemove', expect.any(Function))
    expect(addSpy).toHaveBeenCalledWith('mouseup', expect.any(Function))

    wrapper.unmount()
    expect(removeSpy).toHaveBeenCalledWith('mousemove', expect.any(Function))
    expect(removeSpy).toHaveBeenCalledWith('mouseup', expect.any(Function))

    addSpy.mockRestore()
    removeSpy.mockRestore()
  })
})
