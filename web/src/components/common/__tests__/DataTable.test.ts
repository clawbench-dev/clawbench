import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import en from '@/i18n/locales/en'
import DataTable from '../DataTable.vue'

const { mockCopyText, mockToastShow } = vi.hoisted(() => ({
  mockCopyText: vi.fn(),
  mockToastShow: vi.fn(),
}))

vi.mock('@/utils/clipboard', () => ({ copyText: mockCopyText }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: mockToastShow }) }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

const NUMERIC_HEADERS = ['region', 'q1', 'q2']
const NUMERIC_ROWS = [
  ['East', '10', '20'],
  ['West', '5', '7'],
  ['North', '3', '9'],
  ['South', '7', '11'],
]

const TEXT_HEADERS = ['name', 'city']
const TEXT_ROWS = [
  ['ann', 'paris'],
  ['bob', 'berlin'],
]

function mountTable(
  headers: string[],
  rows: string[][],
  props: Record<string, unknown> = {},
) {
  return mount(DataTable, {
    props: { headers, rows, ...props },
    global: {
      plugins: [i18n],
      // 浮层用 Teleport 挂到 body；测试里原地渲染才能用 wrapper.find 找到。
      stubs: { teleport: true },
    },
    attachTo: document.body,
  })
}

type TableWrapper = ReturnType<typeof mountTable>

function firstColumn(wrapper: TableWrapper): string[] {
  return wrapper.findAll('.dt-grid tbody tr').map(row => row.findAll('td')[0].text())
}

function menuItem(wrapper: TableWrapper, label: string) {
  const item = wrapper.findAll('.dt-pop-item').find(node => node.text().includes(label))
  if (!item) throw new Error(`menu item not found: ${label}`)
  return item
}

describe('DataTable', () => {
  it('renders a numeric table as a data table with totals', () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    expect(wrapper.find('.dt').classes()).toContain('is-data')
    expect(wrapper.findAll('.dt-grid thead th')).toHaveLength(3)
    expect(wrapper.findAll('.dt-grid tbody tr')).toHaveLength(4)
    expect(wrapper.find('.dt-count').text()).toContain('4 rows')
    // q1 = 10+5+3+7 = 25, q2 = 20+7+9+11 = 47
    expect(wrapper.find('.dt-grid tfoot').text()).toContain('25')
    expect(wrapper.find('.dt-grid tfoot').text()).toContain('47')
    // 有合计行时表格带 has-total：最后一条数据行据此不画下边线，避免与
    // 合计行的上边线叠成 2px。
    expect(wrapper.find('.dt-grid').classes()).toContain('has-total')
    wrapper.unmount()
  })

  it('表头栏的全屏按钮用的是最大化图标，不是旋转图标', () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    const btn = wrapper.find('.table-block-landscape-btn')
    expect(btn.exists()).toBe(true)
    // 图形来自 utils/blockHeaderIcons.ts（与注入式表头共用一份）
    expect(btn.html()).toContain('M8 3H5a2 2 0 0 0-2 2v3')
    expect(btn.html()).not.toContain('M21 12a9 9 0 1 1-9-9')
    wrapper.unmount()
  })

  it('keeps a table with text columns as a plain table', () => {
    const wrapper = mountTable(TEXT_HEADERS, TEXT_ROWS)
    expect(wrapper.find('.dt').classes()).not.toContain('is-data')
    expect(wrapper.find('.dt-funnel').exists()).toBe(false)
    expect(wrapper.find('.dt-search').exists()).toBe(false)
    expect(wrapper.find('.dt-grid tfoot').exists()).toBe(false)
    expect(wrapper.find('.dt-grid').classes()).not.toContain('has-total')
    wrapper.unmount()
  })

  it('lets a text table switch to data mode too (toolbar, no totals)', async () => {
    const wrapper = mountTable(TEXT_HEADERS, TEXT_ROWS)
    expect(wrapper.find('.dt-bar').exists()).toBe(false)

    await wrapper.findAll('.dt-seg button')[1].trigger('click')
    expect(wrapper.find('.dt-bar').exists()).toBe(true)
    expect(wrapper.findAll('.dt-funnel')).toHaveLength(2)
    // 没有数字列，不画合计行
    expect(wrapper.find('.dt-grid tfoot').exists()).toBe(false)
    wrapper.unmount()
  })

  it('treats a percentage column as numbers and keeps the raw text', () => {
    const wrapper = mountTable(['渠道', '转化率'], [['a', '12.5%'], ['b', '8,4%']])
    expect(wrapper.find('.dt').classes()).toContain('is-data')
    const cells = wrapper.findAll('.dt-grid tbody tr').map(row => row.findAll('td')[1].text())
    expect(cells).toEqual(['12.5%', '8,4%'])
    // 12.5 + 8.4 = 20.9
    expect(wrapper.find('.dt-grid tfoot').text()).toContain('20.9')
    wrapper.unmount()
  })

  it('copies the table from the header menu', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.find('.dt-copy-btn').trigger('click')
    const item = wrapper.findAll('.dt-pop-item').find(node => node.text().includes('Copy Markdown'))
    expect(item).toBeTruthy()
    await item!.trigger('click')

    expect(mockCopyText).toHaveBeenCalled()
    const text = String(mockCopyText.mock.calls.at(-1)?.[0])
    expect(text).toContain('| region | q1 | q2 |')
    expect(text).toContain('| East | 10 | 20 |')
    wrapper.unmount()
  })

  it('switches between plain and data mode from the toolbar', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    const [plain, data] = wrapper.findAll('.dt-seg button')

    await plain.trigger('click')
    expect(wrapper.find('.dt').classes()).not.toContain('is-data')

    await data.trigger('click')
    expect(wrapper.find('.dt').classes()).toContain('is-data')
    wrapper.unmount()
  })

  it('searches every column and reports the narrowed count', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.find('.dt-search input').setValue('west')
    expect(firstColumn(wrapper)).toEqual(['West'])
    expect(wrapper.find('.dt-count').text()).toContain('1 / 4 rows')
    wrapper.unmount()
  })

  it('shows a no-match message when the search excludes everything', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.find('.dt-search input').setValue('zzz')
    expect(wrapper.find('.dt-empty').text()).toBe('No matching rows')
    wrapper.unmount()
  })

  it('filters a single column from the filter row', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    expect(wrapper.find('.dt-filter-input').exists()).toBe(false)

    await wrapper.findAll('.dt-tool')[0].trigger('click')
    const inputs = wrapper.findAll('.dt-filter-input')
    expect(inputs).toHaveLength(3)

    await inputs[1].setValue('1')
    expect(firstColumn(wrapper)).toEqual(['East'])
    expect(wrapper.find('.dt-tool .dt-badge').text()).toBe('1')
    wrapper.unmount()
  })

  it('filters a column from its funnel menu', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.findAll('.dt-funnel')[1].trigger('click')
    await wrapper.find('.dt-pop-filter input').setValue('1')
    expect(firstColumn(wrapper)).toEqual(['East'])
    // 有筛选的列，漏斗点亮
    expect(wrapper.findAll('.dt-funnel')[1].classes()).toContain('is-on')
    wrapper.unmount()
  })

  it('cycles a column through ascending, descending and unsorted', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    const q1 = () => wrapper.findAll('.dt-grid thead th')[1]

    await q1().trigger('click')
    expect(firstColumn(wrapper)).toEqual(['North', 'West', 'South', 'East'])
    expect(q1().attributes('aria-sort')).toBe('ascending')

    await q1().trigger('click')
    expect(firstColumn(wrapper)).toEqual(['East', 'South', 'West', 'North'])
    expect(q1().attributes('aria-sort')).toBe('descending')

    await q1().trigger('click')
    expect(firstColumn(wrapper)).toEqual(['East', 'West', 'North', 'South'])
    expect(q1().attributes('aria-sort')).toBe('none')
    wrapper.unmount()
  })

  it('filters rows by picking values', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.findAll('.dt-funnel')[0].trigger('click')
    await menuItem(wrapper, 'Filter by value').trigger('click')

    const checks = wrapper.findAll('.dt-pop-check input')
    expect(checks).toHaveLength(4)
    await checks[0].setValue(false) // 取消 East
    expect(firstColumn(wrapper)).toEqual(['West', 'North', 'South'])

    await wrapper.findAll('.dt-pop-btn')[0].trigger('click') // Show all
    expect(firstColumn(wrapper)).toHaveLength(4)
    wrapper.unmount()
  })

  it('shows column stats with sum, average, min and max', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.findAll('.dt-funnel')[1].trigger('click')
    await menuItem(wrapper, 'Column stats').trigger('click')

    const text = wrapper.find('.dt-pop').text()
    expect(text).toContain('Rows')
    expect(text).toContain('Sum')
    expect(text).toContain('25')
    expect(text).toContain('6.25')
    expect(text).toContain('Min')
    expect(text).toContain('3')
    wrapper.unmount()
  })

  it('hides columns from the columns menu and restores them', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.findAll('.dt-tool')[1].trigger('click')

    const boxes = wrapper.findAll('.dt-pop-check input')
    await boxes[2].setValue(false)
    expect(wrapper.findAll('.dt-grid thead th')).toHaveLength(2)
    expect(wrapper.find('.dt-tool .dt-badge').text()).toBe('1')

    await wrapper.find('.dt-pop-btn').trigger('click')
    expect(wrapper.findAll('.dt-grid thead th')).toHaveLength(3)
    wrapper.unmount()
  })

  it('freezes a column from its menu', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    expect(wrapper.findAll('.dt-grid thead th')[0].classes()).toContain('is-frozen')

    await wrapper.findAll('.dt-funnel')[1].trigger('click')
    await menuItem(wrapper, 'Freeze this column').trigger('click')

    const headers = wrapper.findAll('.dt-grid thead th')
    expect(headers[1].classes()).toContain('is-frozen')
    expect(headers[0].classes()).not.toContain('is-frozen')
    wrapper.unmount()
  })

  it('draws data bars by default and turns them off per column', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    expect(wrapper.findAll('.dt-cell-bar').length).toBeGreaterThan(0)

    await wrapper.findAll('.dt-funnel')[1].trigger('click')
    await menuItem(wrapper, 'Hide data bars').trigger('click')
    // q2 仍然有条，q1 关了
    expect(wrapper.findAll('.dt-cell-bar')).toHaveLength(4)
    wrapper.unmount()
  })

  it('copies a column in the current order', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS)
    await wrapper.findAll('.dt-funnel')[1].trigger('click')
    await menuItem(wrapper, 'Copy this column').trigger('click')

    expect(mockCopyText).toHaveBeenCalled()
    expect(mockCopyText.mock.calls.at(-1)?.[0]).toBe('10\n5\n3\n7')
    wrapper.unmount()
  })

  it('paginates when a page size is given', async () => {
    const wrapper = mountTable(NUMERIC_HEADERS, NUMERIC_ROWS, { pageSize: 2 })
    expect(wrapper.findAll('.dt-grid tbody tr')).toHaveLength(2)
    expect(wrapper.find('.dt-pager').exists()).toBe(true)

    await wrapper.findAll('.dt-page-btn')[1].trigger('click')
    expect(wrapper.findAll('.dt-grid tbody tr')).toHaveLength(2)
    expect(wrapper.find('.dt-page-info').text()).toContain('2 / 2')
    wrapper.unmount()
  })

  it('focusSearch switches a plain table to data mode and focuses the box', async () => {
    const wrapper = mountTable(TEXT_HEADERS, TEXT_ROWS)
    expect(wrapper.find('.dt').classes()).not.toContain('is-data')

    ;(wrapper.vm as unknown as { focusSearch: () => void }).focusSearch()
    await nextTick()

    expect(wrapper.find('.dt').classes()).toContain('is-data')
    expect(document.activeElement).toBe(wrapper.find('.dt-search input').element)
    wrapper.unmount()
  })

  it('treats decimal-comma values as numbers and keeps their text', () => {
    const wrapper = mountTable(['name', 'value'], [['a', '3450,75'], ['b', '12,5']])
    expect(wrapper.find('.dt').classes()).toContain('is-data')

    const cells = wrapper.findAll('.dt-grid tbody tr').map(row => row.findAll('td')[1].text())
    expect(cells).toEqual(['3450,75', '12,5'])
    expect(wrapper.findAll('.dt-grid thead th')[1].classes()).toContain('is-numeric')
    // 3450,75 + 12,5 = 3463.25
    expect(wrapper.find('.dt-grid tfoot').text()).toContain('3,463.25')
    wrapper.unmount()
  })

  it('falls back to a positional label for an empty header', () => {
    const wrapper = mountTable(['name', ''], [['ann', '1'], ['bob', '2']])
    expect(wrapper.findAll('.dt-grid thead th')[1].text()).toContain('Column 2')
    wrapper.unmount()
  })
})
