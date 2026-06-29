import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock dependencies before import
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/tableRowExpand.ts', () => {
  const parseTableDataFromElement = vi.fn()
  const onTableMouseDown = vi.fn()
  const onTableTouchStart = vi.fn()
  const isTableDragClick = vi.fn(() => false)
  return {
    parseTableDataFromElement,
    onTableMouseDown,
    onTableTouchStart,
    isTableDragClick,
  }
})

import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { parseTableDataFromElement, isTableDragClick } from '@/utils/tableRowExpand.ts'

describe('useTableRowExpand', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('initializes with null modal state', () => {
    const { tableRowModal } = useTableRowExpand()
    expect(tableRowModal.value).toBeNull()
  })

  it('closeTableRowModal sets modal to null', () => {
    const { tableRowModal, closeTableRowModal } = useTableRowExpand()
    // Set some state first
    tableRowModal.value = { headers: ['A'], rows: [['1']], currentIndex: 0 }
    closeTableRowModal()
    expect(tableRowModal.value).toBeNull()
  })

  it('tableRowPrev decrements currentIndex when > 0', () => {
    const { tableRowModal, tableRowPrev } = useTableRowExpand()
    tableRowModal.value = { headers: ['A'], rows: [['1'], ['2'], ['3']], currentIndex: 2 }
    tableRowPrev()
    expect(tableRowModal.value?.currentIndex).toBe(1)
  })

  it('tableRowPrev does nothing when currentIndex is 0', () => {
    const { tableRowModal, tableRowPrev } = useTableRowExpand()
    tableRowModal.value = { headers: ['A'], rows: [['1'], ['2']], currentIndex: 0 }
    tableRowPrev()
    expect(tableRowModal.value?.currentIndex).toBe(0)
  })

  it('tableRowNext increments currentIndex when < rows.length - 1', () => {
    const { tableRowModal, tableRowNext } = useTableRowExpand()
    tableRowModal.value = { headers: ['A'], rows: [['1'], ['2'], ['3']], currentIndex: 0 }
    tableRowNext()
    expect(tableRowModal.value?.currentIndex).toBe(1)
  })

  it('tableRowNext does nothing when currentIndex is at last row', () => {
    const { tableRowModal, tableRowNext } = useTableRowExpand()
    tableRowModal.value = { headers: ['A'], rows: [['1'], ['2']], currentIndex: 1 }
    tableRowNext()
    expect(tableRowModal.value?.currentIndex).toBe(1)
  })

  it('tableRowPrev does nothing when modal is null', () => {
    const { tableRowModal, tableRowPrev } = useTableRowExpand()
    tableRowModal.value = null
    tableRowPrev()
    expect(tableRowModal.value).toBeNull()
  })

  it('tableRowNext does nothing when modal is null', () => {
    const { tableRowModal, tableRowNext } = useTableRowExpand()
    tableRowModal.value = null
    tableRowNext()
    expect(tableRowModal.value).toBeNull()
  })

  it('handleTableRowClick returns false for interactive elements', () => {
    const { handleTableRowClick } = useTableRowExpand()
    const anchor = document.createElement('a')
    anchor.href = '#'
    document.body.appendChild(anchor)
    const event = new MouseEvent('click', { bubbles: true })
    Object.defineProperty(event, 'target', { value: anchor, writable: false })
    expect(handleTableRowClick(event)).toBe(false)
    anchor.remove()
  })

  it('handleTableRowClick returns false when not on a data row', () => {
    const { handleTableRowClick } = useTableRowExpand()
    const div = document.createElement('div')
    document.body.appendChild(div)
    const event = new MouseEvent('click', { bubbles: true })
    Object.defineProperty(event, 'target', { value: div, writable: false })
    expect(handleTableRowClick(event)).toBe(false)
    div.remove()
  })

  it('handleTableRowClick returns false when click is a drag', () => {
    const { handleTableRowClick } = useTableRowExpand()
    ;(isTableDragClick as ReturnType<typeof vi.fn>).mockReturnValueOnce(true)

    const tr = document.createElement('tr')
    tr.setAttribute('data-row-idx', '0')
    const tbody = document.createElement('tbody')
    tbody.appendChild(tr)
    document.body.appendChild(tbody)

    const event = new MouseEvent('click', { bubbles: true })
    Object.defineProperty(event, 'target', { value: tr, writable: false })
    expect(handleTableRowClick(event)).toBe(false)
    tbody.remove()
  })

  it('handleTableRowClick opens modal for valid table row click', () => {
    const { handleTableRowClick, tableRowModal } = useTableRowExpand()
    ;(isTableDragClick as ReturnType<typeof vi.fn>).mockReturnValue(false)
    ;(parseTableDataFromElement as ReturnType<typeof vi.fn>).mockReturnValue({
      headers: ['Name', 'Age'],
      rows: [['Alice', '30'], ['Bob', '25']],
    })

    const table = document.createElement('table')
    table.setAttribute('data-table-idx', '0')
    const thead = document.createElement('thead')
    const thRow = document.createElement('tr')
    thead.appendChild(thRow)
    table.appendChild(thead)
    const tbody = document.createElement('tbody')
    const tr = document.createElement('tr')
    tr.setAttribute('data-row-idx', '1')
    tbody.appendChild(tr)
    table.appendChild(tbody)
    document.body.appendChild(table)

    const event = new MouseEvent('click', { bubbles: true })
    Object.defineProperty(event, 'target', { value: tr, writable: false })
    const result = handleTableRowClick(event)

    expect(result).toBe(true)
    expect(tableRowModal.value).toEqual({
      headers: ['Name', 'Age'],
      rows: [['Alice', '30'], ['Bob', '25']],
      currentIndex: 1,
    })

    table.remove()
  })

  it('handleTableRowClick does not open modal when table has no rows', () => {
    const { handleTableRowClick, tableRowModal } = useTableRowExpand()
    ;(isTableDragClick as ReturnType<typeof vi.fn>).mockReturnValue(false)
    ;(parseTableDataFromElement as ReturnType<typeof vi.fn>).mockReturnValue({ headers: [], rows: [] })

    const table = document.createElement('table')
    table.setAttribute('data-table-idx', '0')
    const tbody = document.createElement('tbody')
    const tr = document.createElement('tr')
    tr.setAttribute('data-row-idx', '0')
    tbody.appendChild(tr)
    table.appendChild(tbody)
    document.body.appendChild(table)

    const event = new MouseEvent('click', { bubbles: true })
    Object.defineProperty(event, 'target', { value: tr, writable: false })
    handleTableRowClick(event)
    // Modal should not be opened since rows are empty
    expect(tableRowModal.value).toBeNull()

    table.remove()
  })
})
