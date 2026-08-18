import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import PdfPreview from '../PdfPreview.vue'

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

// Mock download utils
vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string, opts?: any) => `/api/local-file/${path}?download=1`,
  downloadFileByPath: vi.fn(),
}))

// Mock pdfjs-dist. getDocument's return value is set per-test so existing
// tests (which never resolve a document) keep failing over to the error path.
vi.mock('pdfjs-dist/legacy/build/pdf.mjs', () => ({
  GlobalWorkerOptions: {},
  getDocument: vi.fn(),
}))

vi.mock('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url', () => ({
  default: 'mock-worker-url',
}))

// jsdom has no IntersectionObserver and no canvas 2d context. Stub both so the
// render pipeline (loadPdf -> fitWidth -> observer setup -> renderPage) can run.
const ctxStub = { setTransform: vi.fn(), scale: vi.fn(), translate: vi.fn() }
let originalIO: typeof IntersectionObserver | undefined

beforeEach(() => {
  originalIO = globalThis.IntersectionObserver
  globalThis.IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof IntersectionObserver
  HTMLCanvasElement.prototype.getContext = vi.fn(() => ctxStub as any) as any
})

afterEach(() => {
  if (originalIO === undefined) delete (globalThis as any).IntersectionObserver
  else globalThis.IntersectionObserver = originalIO
  vi.restoreAllMocks()
})

const stubs = {
  ChevronLeft: true,
  ChevronRight: true,
  ZoomIn: true,
  ZoomOut: true,
  Download: true,
  Loader: true,
  FileX: true,
  MoveHorizontal: true,
}

describe('PdfPreview', () => {
  function mountPdf(props = {}) {
    return mount(PdfPreview, {
      props: {
        file: { name: 'doc.pdf', path: 'doc.pdf' },
        ...props,
      },
      global: { stubs },
    })
  }

  it('renders the PDF container', () => {
    const wrapper = mountPdf()
    expect(wrapper.find('.pdf-preview-container').exists()).toBe(true)
  })

  // Note: toolbar (.pdf-toolbar), page info (.pdf-page-info), and zoom label
  // (.pdf-zoom-label) were removed in a UI redesign. These elements no longer
  // exist in PdfPreview.vue.

  it('shows loading overlay initially', () => {
    const wrapper = mountPdf()
    expect(wrapper.find('.pdf-loading-overlay').exists()).toBe(true)
  })

  it('allows native wheel scroll and only prevents default on ctrl+wheel zoom', async () => {
    const wrapper = mountPdf()
    await flushPromises()
    const scrollEl = wrapper.find('.pdf-pages-scroll')

    const plain = new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true })
    scrollEl.element.dispatchEvent(plain)
    // Plain wheel must scroll the container natively (not be swallowed).
    expect(plain.defaultPrevented).toBe(false)

    const ctrl = new WheelEvent('wheel', { deltaY: 100, bubbles: true, cancelable: true, ctrlKey: true })
    scrollEl.element.dispatchEvent(ctrl)
    // Ctrl+wheel zooms the PDF instead of the page, so it must preventDefault.
    expect(ctrl.defaultPrevented).toBe(true)
  })

  it('exposes outline ref', () => {
    const wrapper = mountPdf()
    const vm = wrapper.vm as any
    expect(vm.outline).toBeDefined()
  })

  it('exposes scrollToPage method', () => {
    const wrapper = mountPdf()
    const vm = wrapper.vm as any
    expect(typeof vm.scrollToPage).toBe('function')
  })

  it('cancels the in-flight render of a page when it is re-rendered (no concurrent draws)', async () => {
    const tasks: Array<{ cancel: () => void; promise: Promise<void> }> = []
    const fakePage = {
      getViewport: vi.fn(() => ({ width: 100, height: 100 })),
      render: vi.fn(() => {
        let rejectFn: (e?: unknown) => void = () => {}
        const promise = new Promise<void>((_, rej) => { rejectFn = rej })
        const task = {
          cancel: vi.fn(() => rejectFn(new Error('cancelled'))),
          promise,
        }
        tasks.push(task as any)
        return task
      }),
    }
    const fakeDoc = {
      numPages: 1,
      getPage: vi.fn(async () => fakePage),
      getOutline: vi.fn(async () => null),
      destroy: vi.fn(),
    }

    const pdfjs = await import('pdfjs-dist/legacy/build/pdf.mjs')
    ;(pdfjs.getDocument as any).mockReturnValue({ promise: Promise.resolve(fakeDoc) })

    const wrapper = mountPdf()
    await flushPromises()

    const vm = wrapper.vm as any
    const beforeFirst = tasks.length
    // First render starts an in-flight task that never resolves on its own.
    const p1 = vm.renderPage(1, true)
    await flushPromises()
    expect(tasks).toHaveLength(beforeFirst + 1)
    const inFlight = tasks[tasks.length - 1]

    // A concurrent re-render of the same page must cancel the in-flight one.
    const p2 = vm.renderPage(1, true)
    await flushPromises()
    expect(inFlight.cancel).toHaveBeenCalledTimes(1)

    // Settle all pending renders so the test doesn't leak async work.
    for (const t of tasks) t.cancel()
    await Promise.all([p1, p2].filter(Boolean))
  })

  // Zoom label removed with toolbar redesign
})
