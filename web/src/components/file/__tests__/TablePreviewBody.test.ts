import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import en from '@/i18n/locales/en'
import TablePreviewBody from '../TablePreviewBody.vue'

const { mockCopyText, mockToastShow } = vi.hoisted(() => ({
  mockCopyText: vi.fn(),
  mockToastShow: vi.fn(),
}))

vi.mock('@/utils/clipboard', () => ({ copyText: mockCopyText }))
vi.mock('@/composables/useToast', () => ({ useToast: () => ({ show: mockToastShow }) }))

/** Read an SFC's source regardless of whether vitest runs from the repo root
 *  or from `web/`. */
function readSfc(name: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, `src/components/file/${name}`), 'utf8')
    } catch { /* try the other root */ }
  }
  throw new Error(`${name} not found from ${process.cwd()}`)
}

function readComponentSource(): string {
  return readSfc('TablePreviewBody.vue')
}

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: { en },
})

function mountTable(content: string, extra: Record<string, unknown> = {}) {
  return mount(TablePreviewBody, {
    props: { file: { name: 'data.csv', content, ...extra } },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

function bodyRows(wrapper: ReturnType<typeof mountTable>) {
  return wrapper.findAll('tbody tr.data-row')
}

function firstColumn(wrapper: ReturnType<typeof mountTable>) {
  return bodyRows(wrapper).map(row => row.findAll('td')[0].text())
}

describe('TablePreviewBody', () => {
  it('renders the header row and every data row', () => {
    const wrapper = mountTable('name,qty\napple,3\nbanana,5')
    expect(wrapper.findAll('thead th.data-th').map(th => th.text().replace('⇅', '').trim())).toEqual(['name', 'qty'])
    expect(bodyRows(wrapper)).toHaveLength(2)
    expect(wrapper.text()).toContain('2 / 2 rows')
    wrapper.unmount()
  })

  it('right-aligns columns that hold mostly numbers', () => {
    const wrapper = mountTable('name,qty\napple,3\nbanana,5')
    const headers = wrapper.findAll('thead th.data-th')
    expect(headers[0].classes()).not.toContain('is-numeric')
    expect(headers[1].classes()).toContain('is-numeric')
    wrapper.unmount()
  })

  it('filters rows with the search box and reports the narrowed count', async () => {
    const wrapper = mountTable('name,qty\napple,3\nbanana,5\ncherry,7')
    await wrapper.find('.table-search-input').setValue('an')
    expect(firstColumn(wrapper)).toEqual(['banana'])
    expect(wrapper.text()).toContain('1 / 3 rows')
    wrapper.unmount()
  })

  it('shows a no-match message when the search excludes everything', async () => {
    const wrapper = mountTable('name\napple')
    await wrapper.find('.table-search-input').setValue('zzz')
    expect(wrapper.find('.table-empty').text()).toBe('No matching rows')
    wrapper.unmount()
  })

  it('hides the per-column search row until the filter button is used', async () => {
    const wrapper = mountTable('name,city\nann,paris\nbob,berlin')
    expect(wrapper.find('.filter-row').exists()).toBe(false)

    await wrapper.find('.table-filter-btn').trigger('click')
    const inputs = wrapper.findAll('.filter-row input')
    expect(inputs).toHaveLength(2)
    expect(inputs[1].attributes('placeholder')).toBe('Search column')

    await inputs[1].setValue('paris')
    expect(firstColumn(wrapper)).toEqual(['ann'])
    // The active-filter count is surfaced on the toolbar button.
    expect(wrapper.find('.table-filter-count').text()).toBe('1')
    wrapper.unmount()
  })

  it('combines the global search with column filters', async () => {
    const wrapper = mountTable('name,city\nann,paris\nbob,paris\ncid,berlin')
    await wrapper.find('.table-filter-btn').trigger('click')
    await wrapper.find('.table-search-input').setValue('b')
    await wrapper.findAll('.filter-row input')[1].setValue('paris')
    expect(firstColumn(wrapper)).toEqual(['bob'])
    wrapper.unmount()
  })

  it('cycles a column through ascending, descending and unsorted', async () => {
    const wrapper = mountTable('name,qty\nb,10\na,2\nc,1')
    const qtyHeader = () => wrapper.findAll('thead th.data-th')[1]

    await qtyHeader().trigger('click')
    expect(firstColumn(wrapper)).toEqual(['c', 'a', 'b'])
    expect(qtyHeader().attributes('aria-sort')).toBe('ascending')

    await qtyHeader().trigger('click')
    expect(firstColumn(wrapper)).toEqual(['b', 'a', 'c'])
    expect(qtyHeader().attributes('aria-sort')).toBe('descending')

    // Third click drops the sort and restores the file's own order.
    await qtyHeader().trigger('click')
    expect(firstColumn(wrapper)).toEqual(['b', 'a', 'c'])
    expect(qtyHeader().attributes('aria-sort')).toBe('none')
    wrapper.unmount()
  })

  it('paginates long tables and hides the footer for a single page', async () => {
    const rows = Array.from({ length: 250 }, (_, i) => `r${i},${i}`).join('\n')
    const wrapper = mountTable(`name,qty\n${rows}`)
    expect(bodyRows(wrapper)).toHaveLength(100)
    expect(wrapper.find('.table-page-info').text()).toBe('Page 1 / 3')

    await wrapper.findAll('.table-page-btn')[1].trigger('click')
    expect(wrapper.find('.table-page-info').text()).toBe('Page 2 / 3')
    expect(bodyRows(wrapper)[0].findAll('td')[0].text()).toBe('r100')
    wrapper.unmount()

    const short = mountTable('a\n1')
    expect(short.find('.table-footer').exists()).toBe(false)
    short.unmount()
  })

  it('reports a header-only file as empty rather than as a filtered table', () => {
    const wrapper = mountTable('name,qty')
    expect(wrapper.find('.table-empty').text()).toBe('This table has no data')
    wrapper.unmount()
  })

  it('warns when the file content was truncated', () => {
    const wrapper = mountTable('a,b\n1,2', { truncated: true })
    expect(wrapper.find('.table-notice').text()).toBe('Content truncated, showing partial text only')
    wrapper.unmount()
  })

  it('falls back to a positional label for an empty header cell', () => {
    const wrapper = mountTable('name,,qty\n1,2,3')
    expect(wrapper.findAll('thead th.data-th')[1].text()).toContain('Column 2')
    wrapper.unmount()
  })

  it('makes frozen body cells actually sticky', () => {
    // The freeze feature silently did nothing once: the rule had `left` but no
    // `position: sticky`, so the cell was merely offset and scrolled away with
    // the rest. jsdom has no layout and cannot catch it.
    const source = readComponentSource()
    const start = source.indexOf('.data-td.is-frozen {')
    expect(start).toBeGreaterThan(-1)
    const block = source.slice(start, source.indexOf('}', start))
    expect(block).toContain('position: sticky')
  })

  it('keeps the column search boxes square, borderless and no wider than their column', () => {
    // Guard on the source: a rounded, bordered input floating over a square
    // grid reads as a foreign control, and an in-flow input carries an
    // intrinsic ~20-character width that widens every column (the "oversized
    // search box" report). `size="1"` plus no min-width is what keeps the
    // column at its natural width. None of this is visible to jsdom.
    const source = readComponentSource()
    const start = source.indexOf('.filter-input {')
    const block = source.slice(start, source.indexOf('.filter-input::placeholder', start))
    expect(start).toBeGreaterThan(-1)
    expect(block).toContain('border: none')
    expect(block).toContain('border-radius: 0')
    expect(block).toContain('min-width: 0')
    expect(block).not.toMatch(/border-radius:\s*var\(/)
    expect(block).not.toMatch(/min-width:\s*calc/)
    // The element must keep the size attribute; CSS alone cannot shrink an
    // input's intrinsic width.
    expect(source).toMatch(/class="filter-input"[\s\S]{0,120}size="1"/)
  })

  it('exposes focusSearch / closeSearch for the header button and Ctrl+F', async () => {
    const wrapper = mountTable('name\napple')
    const vm = wrapper.vm as unknown as { focusSearch: () => void; closeSearch: () => void }

    vm.focusSearch()
    await nextTick()
    expect(document.activeElement).toBe(wrapper.find('.table-search-input').element)

    await wrapper.find('.table-search-input').setValue('app')
    vm.closeSearch()
    await nextTick()
    expect((wrapper.find('.table-search-input').element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })
})

// ── Column menu ─────────────────────────────────────────────────────────────

function menuItems(): HTMLElement[] {
  return Array.from(document.querySelectorAll('.popup-menu .cm-item'))
}

async function openColumnMenu(wrapper: ReturnType<typeof mountTable>, column = 0) {
  await wrapper.findAll('.th-menu-btn')[column].trigger('click')
  await nextTick()
}

async function clickMenuItem(text: string) {
  const item = menuItems().find(node => node.textContent?.includes(text))
  if (!item) {
    throw new Error(`menu item "${text}" not found in: ${menuItems().map(n => n.textContent?.trim()).join(' | ')}`)
  }
  item.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await nextTick()
}

describe('shared sheet shells', () => {
  // .bs-body / .modal-body ship padding-free on purpose (see BottomSheet.vue),
  // so a sheet that forgets its own inset renders flush against the card edges.
  // That is exactly how the stats panel shipped the first time.
  it('every sheet body adds its own padding', () => {
    for (const [name, root] of [['TableColumnStatsSheet.vue', '.cs {'], ['TableValueFilterSheet.vue', '.vf {']] as const) {
      const source = readSfc(name)
      const start = source.indexOf(root)
      expect(start, `${name} has no ${root} rule`).toBeGreaterThan(-1)
      const block = source.slice(start, source.indexOf('}', start))
      expect(block, `${name} must pad its own body`).toMatch(/padding:\s*var\(/)
    }
  })
})

describe('TablePreviewBody column menu', () => {
  it('opens the column stats in their own dialog', async () => {
    const wrapper = mountTable('name,qty\nann,2\nbob,3\ncid,')
    await openColumnMenu(wrapper, 1)
    await clickMenuItem('Column stats')
    await nextTick()
    await nextTick()

    const panel = document.querySelector('.bs-panel')
    expect(panel).not.toBeNull()
    const text = panel!.textContent ?? ''
    expect(text).toContain('Rows')
    expect(text).toContain('Empty')
    expect(text).toContain('Distinct')
    expect(text).toContain('Sum')
    expect(text).toContain('5')
    expect(text).toContain('Average')
    expect(text).toContain('2.5')
    // The numbers no longer live inside the menu itself.
    expect(document.querySelector('.cm-stats')).toBeNull()
    wrapper.unmount()
  })

  it('says a text column has no numeric summary', async () => {
    const wrapper = mountTable('name,qty\nann,2\nbob,3')
    await openColumnMenu(wrapper, 0)
    await clickMenuItem('Column stats')
    await nextTick()
    await nextTick()

    const text = document.querySelector('.bs-panel')?.textContent ?? ''
    expect(text).toContain('Text column')
    expect(text).not.toContain('Average')
    wrapper.unmount()
  })

  it('sorts from the menu', async () => {
    const wrapper = mountTable('name,qty\nann,2\nbob,5\ncid,1')
    await openColumnMenu(wrapper, 1)
    await clickMenuItem('Sort descending')
    expect(firstColumn(wrapper)).toEqual(['bob', 'ann', 'cid'])
    wrapper.unmount()
  })

  it('hides a column and restores it from the toolbar', async () => {
    const wrapper = mountTable('name,city\nann,paris\nbob,berlin')
    expect(wrapper.findAll('thead th.data-th')).toHaveLength(2)

    await openColumnMenu(wrapper, 0)
    await clickMenuItem('Hide column')
    expect(wrapper.findAll('thead th.data-th')).toHaveLength(1)
    expect(wrapper.findAll('tbody tr.data-row')[0].findAll('td')).toHaveLength(1)

    const restoreBtn = wrapper.find('button[title="Hidden columns"]')
    expect(restoreBtn.exists()).toBe(true)
    await restoreBtn.trigger('click')
    await nextTick()

    const restoreItem = document.querySelector('.restore-item') as HTMLElement | null
    expect(restoreItem?.textContent).toContain('name')
    restoreItem!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(wrapper.findAll('thead th.data-th')).toHaveLength(2)
    wrapper.unmount()
  })

  it('shows only the chosen column', async () => {
    const wrapper = mountTable('name,city,qty\nann,paris,2\nbob,berlin,5')
    await openColumnMenu(wrapper, 1)
    await clickMenuItem('Show only this column')

    const headers = wrapper.findAll('thead th.data-th')
    expect(headers).toHaveLength(1)
    expect(headers[0].text()).toContain('city')
    // Hiding all the others is recoverable through the same toolbar button.
    expect(wrapper.find('button[title="Hidden columns"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('freezes a column and unfreezes it', async () => {
    const wrapper = mountTable('name,city\nann,paris')
    await openColumnMenu(wrapper, 0)
    await clickMenuItem('Freeze column')
    expect(wrapper.findAll('thead th.data-th')[0].classes()).toContain('is-frozen')
    expect(wrapper.findAll('tbody tr.data-row')[0].findAll('td')[0].classes()).toContain('is-frozen')

    await openColumnMenu(wrapper, 0)
    await clickMenuItem('Unfreeze column')
    expect(wrapper.findAll('thead th.data-th')[0].classes()).not.toContain('is-frozen')
    wrapper.unmount()
  })

  it('draws data bars only after the menu turns them on', async () => {
    const wrapper = mountTable('name,qty\nann,2\nbob,4')
    expect(wrapper.find('.data-bar').exists()).toBe(false)

    await openColumnMenu(wrapper, 1)
    await clickMenuItem('Show data bars')

    const bars = wrapper.findAll('.data-bar')
    expect(bars).toHaveLength(2)
    // 2 is the column minimum (0%), 4 is the maximum (100%).
    expect(bars[0].attributes('style')).toContain('width: 0%')
    expect(bars[1].attributes('style')).toContain('width: 100%')
    wrapper.unmount()
  })

  it('never draws data bars on a text column', async () => {
    const wrapper = mountTable('name,qty\nann,2')
    // The text column's menu has no data-bar entry at all.
    await openColumnMenu(wrapper, 0)
    expect(menuItems().some(node => node.textContent?.includes('data bars'))).toBe(false)
    wrapper.unmount()
  })

  it('copies the whole column in the current order', async () => {
    mockCopyText.mockClear()
    const wrapper = mountTable('name,qty\nann,2\nbob,5')
    await openColumnMenu(wrapper, 0)
    await clickMenuItem('Copy column')
    expect(mockCopyText).toHaveBeenCalledTimes(1)
    expect(mockCopyText.mock.calls[0][0]).toBe('ann\nbob')
    wrapper.unmount()
  })

  it('filters rows by picking a value', async () => {
    const wrapper = mountTable('name,city\nann,paris\nbob,berlin\ncid,paris')
    await openColumnMenu(wrapper, 1)
    await clickMenuItem('Filter by value')
    await nextTick()
    await nextTick()

    const checks = Array.from(document.querySelectorAll('.vf-check')) as HTMLInputElement[]
    // Values are listed in the table's own sort order: berlin, paris.
    expect(checks).toHaveLength(2)

    // Unchecking "berlin" (the first entry, since nothing is selected yet)
    // means "everything except berlin".
    checks[0].click()
    await nextTick()

    expect(firstColumn(wrapper)).toEqual(['ann', 'cid'])
    wrapper.unmount()
  })
})
