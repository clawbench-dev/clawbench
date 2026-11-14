import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en'
import ExcelTableBody from '../ExcelTableBody.vue'
import type { SheetTable } from '@/utils/sheetRows'

const { parseWorkbookMock } = vi.hoisted(() => ({ parseWorkbookMock: vi.fn() }))

vi.mock('@/utils/sheetRows', async importOriginal => {
  const actual = await importOriginal<typeof import('@/utils/sheetRows')>()
  return { ...actual, parseWorkbook: parseWorkbookMock }
})

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function sheet(name: string, headers: string[], rows: string[][], extra: Partial<SheetTable> = {}): SheetTable {
  return { name, headers, rows, rowsTruncated: false, columnsTruncated: false, ...extra }
}

function mountBody(src = '/api/fs/raw/data.xlsx') {
  return mount(ExcelTableBody, {
    props: { src },
    global: { plugins: [i18n], stubs: { teleport: true } },
    attachTo: document.body,
  })
}

describe('ExcelTableBody', () => {
  beforeEach(() => {
    parseWorkbookMock.mockReset()
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, arrayBuffer: async () => new ArrayBuffer(8) })))
  })

  it('renders the first worksheet through the shared DataTable', async () => {
    parseWorkbookMock.mockResolvedValue([
      sheet('销售', ['名称', '数量'], [['苹果', '3'], ['香蕉', '5']]),
    ])
    const wrapper = mountBody()
    await flushPromises()

    expect(wrapper.findAll('.dt-grid thead th')).toHaveLength(2)
    expect(wrapper.findAll('.dt-grid tbody tr')).toHaveLength(2)
    expect(wrapper.find('.dt').classes()).toContain('is-data')
    // 单表不显示页签
    expect(wrapper.find('.sheet-tabs').exists()).toBe(false)
    wrapper.unmount()
  })

  it('reports the sheet names so the parent toolbar can render the tabs', async () => {
    parseWorkbookMock.mockResolvedValue([
      sheet('销售', ['名称', '数量'], [['苹果', '3']]),
      sheet('备注', ['说明'], [['只有一行']]),
    ])
    const wrapper = mountBody()
    await flushPromises()

    expect(wrapper.emitted('sheets')?.at(-1)).toEqual([['销售', '备注']])
    wrapper.unmount()
  })

  it('renders the worksheet picked by activeSheet', async () => {
    parseWorkbookMock.mockResolvedValue([
      sheet('销售', ['名称', '数量'], [['苹果', '3']]),
      sheet('备注', ['说明'], [['只有一行']]),
    ])
    const wrapper = mountBody()
    await flushPromises()
    expect(wrapper.find('.dt-grid thead th').text()).toContain('名称')

    await wrapper.setProps({ activeSheet: 1 })
    expect(wrapper.find('.dt-grid thead th').text()).toContain('说明')
    expect(wrapper.find('.dt-grid tbody tr').text()).toContain('只有一行')
    wrapper.unmount()
  })

  it('warns when the sheet was cut down to the row cap', async () => {
    parseWorkbookMock.mockResolvedValue([
      sheet('big', ['a'], [['1']], { rowsTruncated: true }),
    ])
    const wrapper = mountBody()
    await flushPromises()

    expect(wrapper.find('.table-notice').text()).toContain('5000')
    wrapper.unmount()
  })

  it('shows the parse failure and retries the request', async () => {
    parseWorkbookMock.mockRejectedValueOnce(new Error('Corrupted zip'))
    const wrapper = mountBody()
    await flushPromises()

    expect(wrapper.find('.excel-error').text()).toContain('Corrupted zip')

    parseWorkbookMock.mockResolvedValueOnce([sheet('ok', ['a'], [['1']])])
    await wrapper.find('.excel-retry-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.excel-error').exists()).toBe(false)
    expect(wrapper.find('.dt-grid thead th').text()).toBe('a')
    wrapper.unmount()
  })

  it('reloads when the source changes', async () => {
    parseWorkbookMock.mockResolvedValue([sheet('one', ['a'], [['1']])])
    const wrapper = mountBody('/api/fs/raw/one.xlsx')
    await flushPromises()
    expect(wrapper.find('.dt-grid thead th').text()).toBe('a')

    parseWorkbookMock.mockResolvedValue([sheet('two', ['b'], [['2']])])
    await wrapper.setProps({ src: '/api/fs/raw/two.xlsx' })
    await flushPromises()

    expect(wrapper.find('.dt-grid thead th').text()).toBe('b')
    wrapper.unmount()
  })
})
