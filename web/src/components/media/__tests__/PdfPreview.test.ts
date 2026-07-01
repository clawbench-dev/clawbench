import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
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

// Mock pdfjs-dist to prevent actual PDF loading in tests
vi.mock('pdfjs-dist', () => ({
  GlobalWorkerOptions: {},
  getDocument: vi.fn(),
}))

vi.mock('pdfjs-dist/build/pdf.worker.min.mjs?url', () => ({
  default: 'mock-worker-url',
}))

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

  it('renders the toolbar with zoom controls', () => {
    const wrapper = mountPdf()
    expect(wrapper.find('.pdf-toolbar').exists()).toBe(true)
  })

  it('renders the page navigation controls', () => {
    const wrapper = mountPdf()
    expect(wrapper.find('.pdf-page-info').exists()).toBe(true)
  })

  it('shows loading overlay initially', () => {
    const wrapper = mountPdf()
    expect(wrapper.find('.pdf-loading-overlay').exists()).toBe(true)
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

  it('shows zoom percentage label', () => {
    const wrapper = mountPdf()
    expect(wrapper.find('.pdf-zoom-label').exists()).toBe(true)
  })
})
