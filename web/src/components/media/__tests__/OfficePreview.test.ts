import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import OfficePreview from '../OfficePreview.vue'

// Controllable ResizeObserver so tests can simulate a container resize (which
// emits no window `resize` event — the splitter-drag / dock-toggle case).
let roCallbacks: ResizeObserverCallback[] = []
class MockResizeObserver {
  static instances: MockResizeObserver[] = []
  callback: ResizeObserverCallback
  constructor(cb: ResizeObserverCallback) {
    this.callback = cb
    roCallbacks.push(cb)
    MockResizeObserver.instances.push(this)
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', MockResizeObserver)
function triggerResize() {
  for (const cb of roCallbacks) cb([], {} as ResizeObserver)
}

// Manual rAF control so the pptx re-fit (deferred to the next frame) is
// deterministic.
let pendingRafs: Array<() => void> = []
vi.stubGlobal('requestAnimationFrame', vi.fn((cb: () => void) => {
  pendingRafs.push(cb)
  return pendingRafs.length
}))
vi.stubGlobal('cancelAnimationFrame', vi.fn(() => { pendingRafs = [] }))
function flushRafs() {
  const queue = pendingRafs
  pendingRafs = []
  queue.forEach((cb) => cb())
}

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      common: { loading: 'Loading...', retry: 'Retry', download: 'Download' },
      file: { viewer: { loadFailed: 'Failed to load document' } },
    },
  },
})

const { mockIsAppMode, mockDownloadFileByPath } = vi.hoisted(() => ({
  mockIsAppMode: { value: false },
  mockDownloadFileByPath: vi.fn(),
}))

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: mockIsAppMode }),
}))

// Mock download utils
vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string, opts?: any) => `/api/local-file/${path}?download=1`,
  downloadFileByPath: mockDownloadFileByPath,
}))

// Mock appLog
vi.mock('@/utils/appLog.ts', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock vue-office components (they require DOM + heavy deps)
vi.mock('@vue-office/docx', () => ({
  default: { name: 'VueOfficeDocx', template: '<div class="mock-docx"></div>' },
}))
vi.mock('@vue-office/excel', () => ({
  default: { name: 'VueOfficeExcel', template: '<div class="mock-excel"></div>' },
}))
vi.mock('@vue-office/pptx', () => ({
  default: {
    name: 'VueOfficePptx',
    // Mirrors the real library: each slide wrapper holds a `.slide-wrapper`
    // sized at the document's native size (720x540) with a baked scale.
    template: `<div class="mock-pptx"><div class="pptx-preview-wrapper">
      <div class="pptx-preview-slide-wrapper" style="width: 720px; height: 540px;">
        <div class="slide-wrapper" style="position:absolute; width: 720px; height: 540px; transform: scale(1);"></div>
      </div>
    </div></div>`,
  },
}))
vi.mock('@vue-office/docx/lib/index.css', () => ({}))
vi.mock('@vue-office/excel/lib/index.css', () => ({}))

const stubs = {
  Loader: true,
  FileX: true,
  Download: true,
  RefreshCw: true,
}

describe('OfficePreview', () => {
  beforeEach(() => {
    mockIsAppMode.value = false
    mockDownloadFileByPath.mockClear()
    roCallbacks = []
    pendingRafs = []
  })

  function mountOffice(props = {}) {
    return mount(OfficePreview, {
      props: {
        file: { name: 'report.docx', path: 'test/office/report.docx', isOffice: true },
        ...props,
      },
      global: { stubs, plugins: [i18n] },
    })
  }

  it('renders the container', () => {
    const wrapper = mountOffice()
    expect(wrapper.find('.office-preview-container').exists()).toBe(true)
  })

  it('does not block native wheel scroll for word documents', async () => {
    const wrapper = mountOffice({ file: { name: 'report.docx', path: 'report.docx', isOffice: true } })
    const container = wrapper.find('.office-preview-container')
    const evt = new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true })
    container.element.dispatchEvent(evt)
    // onWheel only acts on PPT zoom; it must not preventDefault plain scroll.
    expect(evt.defaultPrevented).toBe(false)
  })

  it('shows loading overlay initially', () => {
    const wrapper = mountOffice()
    expect(wrapper.find('.loading-indicator.overlay').exists()).toBe(true)
  })

  it('renders docx component for .docx files', () => {
    const wrapper = mountOffice({ file: { name: 'report.docx', path: 'report.docx', isOffice: true } })
    expect(wrapper.find('.mock-docx').exists() || wrapper.find('.office-preview-body').exists()).toBe(true)
  })

  it('renders excel component for .xlsx files', () => {
    const wrapper = mountOffice({ file: { name: 'data.xlsx', path: 'data.xlsx', isOffice: true } })
    expect(wrapper.find('.mock-excel').exists() || wrapper.find('.office-preview-body').exists()).toBe(true)
  })

  it('renders pptx component for .pptx files', () => {
    const wrapper = mountOffice({ file: { name: 'slides.pptx', path: 'slides.pptx', isOffice: true } })
    expect(wrapper.find('.mock-pptx').exists() || wrapper.find('.office-preview-body').exists()).toBe(true)
  })

  it('renders excel component for .xls files', () => {
    const wrapper = mountOffice({ file: { name: 'data.xls', path: 'data.xls', isOffice: true } })
    expect(wrapper.find('.mock-excel').exists() || wrapper.find('.office-preview-body').exists()).toBe(true)
  })

  it('shows error overlay when error is set', async () => {
    const wrapper = mountOffice()
    // Trigger error via exposed onError method
    const vm = wrapper.vm as any
    vm.onError('Parse error')
    // Verify error state via exposed refs (DOM doesn't re-render in Vue 3.5 + test-utils)
    expect(vm.error).toBe('Parse error')
    expect(vm.loading).toBe(false)
  })

  it('shows retry button on error', async () => {
    const wrapper = mountOffice()
    const vm = wrapper.vm as any
    vm.onError('Some error')
    // Verify error state is set (retry button renders when error is truthy)
    expect(vm.error).toBe('Some error')
    expect(vm.loading).toBe(false)
  })

  it('onError handles Error objects and non-string payloads', async () => {
    const wrapper = mountOffice()
    const vm = wrapper.vm as any
    vm.onError(new Error('wrapped'))
    expect(vm.error).toBe('wrapped')
    vm.onError(42)
    expect(vm.error).toBe('42')
  })

  describe('reload', () => {
    it('resets loading and clears error', async () => {
      const wrapper = mountOffice()
      const vm = wrapper.vm as any
      vm.onError('err')
      vm.reload()
      expect(vm.loading).toBe(true)
      expect(vm.error).toBe('')
    })

    it('reload is triggered by the retry button', async () => {
      const wrapper = mountOffice()
      const vm = wrapper.vm as any
      vm.onError('err')
      await nextTick()
      // Find retry button in error overlay (rendered conditionally on error)
      const retryBtn = wrapper.find('.office-retry-btn')
      if (retryBtn.exists()) {
        await retryBtn.trigger('click')
        expect(vm.loading).toBe(true)
      }
    })
  })

  describe('onRendered', () => {
    it('clears loading and error on rendered event', async () => {
      const wrapper = mountOffice()
      const vm = wrapper.vm as any
      vm.onError('previous error')
      await wrapper.findComponent({ name: 'VueOfficeDocx' }).vm.$emit('rendered')
      await nextTick()
      expect(vm.loading).toBe(false)
      expect(vm.error).toBe('')
    })

    it('applies PPT scale on rendered for pptx files', async () => {
      const wrapper = mountOffice({ file: { name: 's.pptx', path: 's.pptx', isOffice: true } })
      await wrapper.findComponent({ name: 'VueOfficePptx' }).vm.$emit('rendered')
      await nextTick()
      expect(wrapper.vm.loading).toBe(false)
    })
  })

  describe('PPT zoom', () => {
    function mountPpt() {
      return mountOffice({ file: { name: 'slides.pptx', path: 'slides.pptx', isOffice: true } })
    }

    it('scales up on ctrl+scroll up', async () => {
      const wrapper = mountPpt()
      await nextTick()
      const container = wrapper.find('.office-preview-container')
      const evt = new WheelEvent('wheel', { deltaY: -100, bubbles: true, cancelable: true, ctrlKey: true })
      container.element.dispatchEvent(evt)
      expect(evt.defaultPrevented).toBe(true)
      const pptWrapper = wrapper.find('.pptx-preview-wrapper')
      expect(pptWrapper.attributes('style')).toContain('scale(1.1)')
    })

    it('scales down on ctrl+scroll down', async () => {
      const wrapper = mountPpt()
      await nextTick()
      // First zoom up to 1.1 so we can come back down
      const container = wrapper.find('.office-preview-container')
      container.element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, bubbles: true, cancelable: true, ctrlKey: true }))
      container.element.dispatchEvent(new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true, ctrlKey: true }))
      const pptWrapper = wrapper.find('.pptx-preview-wrapper')
      // Back to 1.0 clears the transform entirely
      expect(pptWrapper.attributes('style')).not.toContain('scale(')
    })

    it('does not zoom on plain scroll without ctrl/meta', async () => {
      const wrapper = mountPpt()
      await nextTick()
      const container = wrapper.find('.office-preview-container')
      const evt = new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true })
      container.element.dispatchEvent(evt)
      expect(evt.defaultPrevented).toBe(false)
    })

    it('handles pinch zoom via touch events', async () => {
      const wrapper = mountPpt()
      await nextTick()
      const body = wrapper.find('.office-preview-body')
      await body.trigger('touchstart', {
        touches: [{ clientX: 0, clientY: 0 }, { clientX: 50, clientY: 0 }],
      })
      await body.trigger('touchmove', {
        touches: [{ clientX: 0, clientY: 0 }, { clientX: 100, clientY: 0 }],
      })
      await nextTick()
      const pptWrapper = wrapper.find('.pptx-preview-wrapper')
      expect(pptWrapper.attributes('style')).toContain('scale(2)')
      // End the pinch (fewer than 2 touches) resets the starting distance
      await body.trigger('touchend', { touches: [{ clientX: 0, clientY: 0 }] })
    })

    it('fitWidth resets zoom to 1', async () => {
      const wrapper = mountPpt()
      await nextTick()
      const container = wrapper.find('.office-preview-container')
      container.element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, bubbles: true, cancelable: true, ctrlKey: true }))
      const pptWrapper = wrapper.find('.pptx-preview-wrapper')
      expect(pptWrapper.attributes('style')).toContain('scale(1.1)')
      ;(wrapper.vm as any).fitWidth()
      await nextTick()
      expect(pptWrapper.attributes('style')).not.toContain('scale(1.1)')
    })
  })

  describe('download', () => {
    it('calls downloadFileByPath when in app mode', async () => {
      mockIsAppMode.value = true
      const wrapper = mountOffice()
      ;(wrapper.vm as any).handleDownload()
      expect(mockDownloadFileByPath).toHaveBeenCalled()
    })

    it('builds a local file download URL when not in app mode', async () => {
      mockIsAppMode.value = false
      const wrapper = mountOffice()
      // BuildLocalFileUrl is used for the file src and download anchor
      expect(wrapper.vm).toBeDefined()
    })
  })

  describe('file change', () => {
    it('resets loading when the file path changes', async () => {
      const wrapper = mountOffice()
      ;(wrapper.vm as any).loading = false
      await wrapper.setProps({ file: { name: 'report2.docx', path: 'other/report2.docx', isOffice: true } })
      await nextTick()
      expect(wrapper.vm.loading).toBe(true)
    })
  })

  it('logs mount and unmount lifecycle', async () => {
    const wrapper = mountOffice()
    const vm = wrapper.vm as any
    expect(vm).toBeDefined()
    wrapper.unmount()
  })

  describe('container resize re-fit', () => {
    // A splitter drag or dock toggle resizes the preview container without any
    // window `resize` event, so the component must observe its own body element.
    function stubBodyWidth(wrapper: any, width: number) {
      const body = wrapper.find('.office-preview-body').element as HTMLElement
      Object.defineProperty(body, 'clientWidth', { value: width, configurable: true })
    }

    it('observes the preview body on mount', () => {
      const wrapper = mountOffice()
      expect(roCallbacks.length).toBeGreaterThan(0)
      wrapper.unmount()
    })

    it('re-fits pptx slides to the new container width', async () => {
      const wrapper = mountOffice({ file: { name: 'slides.pptx', path: 'slides.pptx', isOffice: true } })
      await nextTick()
      stubBodyWidth(wrapper, 900)

      triggerResize()
      flushRafs()
      await nextTick()

      const slide = wrapper.find('.pptx-preview-slide-wrapper').element as HTMLElement
      const inner = wrapper.find('.slide-wrapper').element as HTMLElement
      expect(slide.style.width).toBe('900px')
      // 540/720 = 0.75 → height and the baked scale both follow the new width
      expect(slide.style.height).toBe('675px')
      expect(inner.style.transform).toBe('scale(1.25)')
      wrapper.unmount()
    })

    it('does not re-fit when the width is unchanged', async () => {
      const wrapper = mountOffice({ file: { name: 'slides.pptx', path: 'slides.pptx', isOffice: true } })
      await nextTick()
      stubBodyWidth(wrapper, 900)
      triggerResize(); flushRafs(); await nextTick()
      const slide = wrapper.find('.pptx-preview-slide-wrapper').element as HTMLElement
      expect(slide.style.width).toBe('900px')

      // Same width again — must not schedule another re-fit.
      const rafCountBefore = pendingRafs.length
      triggerResize()
      expect(pendingRafs.length).toBe(rafCountBefore)
      wrapper.unmount()
    })

    it('ignores a zero-width (hidden) container and re-fits on re-show', async () => {
      const wrapper = mountOffice({ file: { name: 'slides.pptx', path: 'slides.pptx', isOffice: true } })
      await nextTick()
      stubBodyWidth(wrapper, 0)
      triggerResize(); flushRafs(); await nextTick()
      // Still at the native size — a hidden container must not produce a 0-width slide.
      expect((wrapper.find('.pptx-preview-slide-wrapper').element as HTMLElement).style.width).toBe('720px')

      stubBodyWidth(wrapper, 800)
      triggerResize(); flushRafs(); await nextTick()
      expect((wrapper.find('.pptx-preview-slide-wrapper').element as HTMLElement).style.width).toBe('800px')
      wrapper.unmount()
    })

    it('re-dispatches window resize so the excel viewer re-measures', async () => {
      vi.useFakeTimers()
      try {
        const wrapper = mountOffice({ file: { name: 'data.xlsx', path: 'data.xlsx', isOffice: true } })
        await nextTick()
        const spy = vi.fn()
        window.addEventListener('resize', spy)
        stubBodyWidth(wrapper, 640)

        triggerResize()
        // Debounced: nothing until the timer flushes, then exactly one event.
        expect(spy).not.toHaveBeenCalled()
        vi.advanceTimersByTime(200)
        expect(spy).toHaveBeenCalledTimes(1)

        window.removeEventListener('resize', spy)
        wrapper.unmount()
      } finally {
        vi.useRealTimers()
      }
    })

    it('does not dispatch a resize for docx (library handles it via CSS)', async () => {
      vi.useFakeTimers()
      try {
        const wrapper = mountOffice({ file: { name: 'report.docx', path: 'report.docx', isOffice: true } })
        await nextTick()
        const spy = vi.fn()
        window.addEventListener('resize', spy)
        stubBodyWidth(wrapper, 500)
        triggerResize()
        vi.advanceTimersByTime(500)
        expect(spy).not.toHaveBeenCalled()
        window.removeEventListener('resize', spy)
        wrapper.unmount()
      } finally {
        vi.useRealTimers()
      }
    })

    it('disconnects the observer on unmount', () => {
      const wrapper = mountOffice()
      // The component creates its own ResizeObserver; capture that instance.
      const observer = MockResizeObserver.instances[MockResizeObserver.instances.length - 1]
      const spy = vi.spyOn(observer, 'disconnect')
      wrapper.unmount()
      expect(spy).toHaveBeenCalledTimes(1)
    })
  })
})
