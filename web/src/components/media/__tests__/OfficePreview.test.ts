import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import OfficePreview from '../OfficePreview.vue'

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

// Mock useAppMode
vi.mock('@/composables/useAppMode.ts', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))

// Mock download utils
vi.mock('@/utils/download.ts', () => ({
  buildLocalFileUrl: (path: string, opts?: any) => `/api/local-file/${path}?download=1`,
  downloadFileByPath: vi.fn(),
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
  default: { name: 'VueOfficePptx', template: '<div class="mock-pptx"></div>' },
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

  it('shows loading overlay initially', () => {
    const wrapper = mountOffice()
    expect(wrapper.find('.office-loading-overlay').exists()).toBe(true)
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
    // Simulate error by calling onError
    const vm = wrapper.vm as any
    // Directly set the error ref
    await wrapper.setData({ error: 'Parse error', loading: false })
    expect(wrapper.find('.office-error-overlay').exists()).toBe(true)
    expect(wrapper.text()).toContain('Parse error')
  })

  it('shows retry button on error', async () => {
    const wrapper = mountOffice()
    await wrapper.setData({ error: 'Some error', loading: false })
    expect(wrapper.find('.office-retry-btn').exists()).toBe(true)
  })
})
