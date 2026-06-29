import { describe, expect, it, vi, beforeEach } from 'vitest'

describe('tableRowExpand', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  async function importModule() {
    return import('@/utils/tableRowExpand.ts')
  }

  describe('injectTableRowAttrs', () => {
    it('returns html unchanged when no <table> tag', async () => {
      const { injectTableRowAttrs } = await importModule()
      const html = '<p>Hello</p><div>No table</div>'
      expect(injectTableRowAttrs(html)).toBe(html)
    })

    it('returns html unchanged for empty string', async () => {
      const { injectTableRowAttrs } = await importModule()
      expect(injectTableRowAttrs('')).toBe('')
    })

    it('adds data-table-idx to table and data-row-idx to tbody rows', async () => {
      const { injectTableRowAttrs } = await importModule()
      const html = '<table><thead><tr><th>Name</th></tr></thead><tbody><tr><td>Alice</td></tr><tr><td>Bob</td></tr></tbody></table>'
      const result = injectTableRowAttrs(html)
      expect(result).toContain('data-table-idx')
      expect(result).toContain('data-row-idx="0"')
      expect(result).toContain('data-row-idx="1"')
    })

    it('returns original html when table already has data-table-idx', async () => {
      const { injectTableRowAttrs } = await importModule()
      const html = '<table data-table-idx="0"><thead><tr><th>Name</th></tr></thead><tbody><tr data-row-idx="0"><td>Alice</td></tr></tbody></table>'
      // Should return original since already annotated
      const result = injectTableRowAttrs(html)
      // When all tables already have data-table-idx, changed=false and original html is returned
      expect(result).toBe(html)
    })

    it('handles multiple tables', async () => {
      const { injectTableRowAttrs } = await importModule()
      const html = '<table><thead><tr><th>A</th></tr></thead><tbody><tr><td>1</td></tr></tbody></table><table><thead><tr><th>B</th></tr></thead><tbody><tr><td>2</td></tr></tbody></table>'
      const result = injectTableRowAttrs(html)
      // Each table gets a different data-table-idx
      expect(result).toContain('data-table-idx="0"')
      expect(result).toContain('data-table-idx="1"')
    })
  })

  describe('parseTableDataFromElement', () => {
    it('extracts headers and rows from a table element', async () => {
      const { parseTableDataFromElement } = await importModule()
      const table = document.createElement('table')
      table.innerHTML = '<thead><tr><th>Name</th><th>Age</th></tr></thead><tbody><tr><td>Alice</td><td>30</td></tr><tr><td>Bob</td><td>25</td></tr></tbody>'
      const data = parseTableDataFromElement(table as HTMLTableElement)
      expect(data).toEqual({
        headers: ['Name', 'Age'],
        rows: [['Alice', '30'], ['Bob', '25']],
      })
    })

    it('returns null when no headers or rows', async () => {
      const { parseTableDataFromElement } = await importModule()
      const table = document.createElement('table')
      table.innerHTML = '<thead></thead><tbody></tbody>'
      const data = parseTableDataFromElement(table as HTMLTableElement)
      expect(data).toBeNull()
    })

    it('handles empty tbody', async () => {
      const { parseTableDataFromElement } = await importModule()
      const table = document.createElement('table')
      table.innerHTML = '<thead><tr><th>Name</th></tr></thead><tbody></tbody>'
      const data = parseTableDataFromElement(table as HTMLTableElement)
      expect(data).toEqual({ headers: ['Name'], rows: [] })
    })
  })

  describe('drag-vs-click guard', () => {
    it('onTableMouseDown records start position', async () => {
      const { onTableMouseDown, isTableDragClick } = await importModule()
      const wrap = document.createElement('div')
      wrap.classList.add('table-wrap')
      document.body.appendChild(wrap)

      const event = new MouseEvent('mousedown', { clientX: 100, clientY: 200, bubbles: true })
      Object.defineProperty(event, 'target', { value: wrap, writable: false })
      onTableMouseDown(event)

      // Click at same position should NOT be a drag
      const clickEvent = new MouseEvent('click', { clientX: 100, clientY: 200, bubbles: true })
      Object.defineProperty(clickEvent, 'target', { value: wrap, writable: false })
      expect(isTableDragClick(clickEvent)).toBe(false)

      wrap.remove()
    })

    it('isTableDragClick returns true when moved beyond threshold', async () => {
      const { onTableMouseDown, isTableDragClick } = await importModule()
      const wrap = document.createElement('div')
      wrap.classList.add('table-wrap')
      document.body.appendChild(wrap)

      const event = new MouseEvent('mousedown', { clientX: 100, clientY: 100, bubbles: true })
      Object.defineProperty(event, 'target', { value: wrap, writable: false })
      onTableMouseDown(event)

      // Click 10px away → beyond threshold of 5
      const clickEvent = new MouseEvent('click', { clientX: 110, clientY: 100, bubbles: true })
      Object.defineProperty(clickEvent, 'target', { value: wrap, writable: false })
      expect(isTableDragClick(clickEvent)).toBe(true)

      wrap.remove()
    })

    it('isTableDragClick returns false when no wrap element', async () => {
      const { isTableDragClick } = await importModule()
      const div = document.createElement('div')
      document.body.appendChild(div)
      const event = new MouseEvent('click', { clientX: 100, clientY: 100, bubbles: true })
      Object.defineProperty(event, 'target', { value: div, writable: false })
      expect(isTableDragClick(event)).toBe(false)
      div.remove()
    })

    it('onTableTouchStart records touch start position', async () => {
      const { onTableTouchStart, isTableDragClick } = await importModule()
      const wrap = document.createElement('div')
      wrap.classList.add('table-wrap')
      document.body.appendChild(wrap)

      // jsdom doesn't have Touch constructor, create a synthetic touch object
      const touch = { identifier: 0, target: wrap, clientX: 50, clientY: 50 } as Touch
      const touchEvent = new TouchEvent('touchstart', {
        touches: [touch],
        bubbles: true,
      })
      Object.defineProperty(touchEvent, 'target', { value: wrap, writable: false })
      onTableTouchStart(touchEvent)

      // Click at same position → not a drag
      const clickEvent = new MouseEvent('click', { clientX: 50, clientY: 50, bubbles: true })
      Object.defineProperty(clickEvent, 'target', { value: wrap, writable: false })
      expect(isTableDragClick(clickEvent)).toBe(false)

      wrap.remove()
    })

    it('isTableDragClick returns false when no start recorded', async () => {
      const { isTableDragClick } = await importModule()
      const wrap = document.createElement('div')
      wrap.classList.add('table-wrap')
      document.body.appendChild(wrap)

      const clickEvent = new MouseEvent('click', { clientX: 100, clientY: 100, bubbles: true })
      Object.defineProperty(clickEvent, 'target', { value: wrap, writable: false })
      expect(isTableDragClick(clickEvent)).toBe(false)

      wrap.remove()
    })

    it('onTableTouchStart ignores event with no touches', async () => {
      const { onTableTouchStart } = await importModule()
      const wrap = document.createElement('div')
      wrap.classList.add('table-wrap')
      document.body.appendChild(wrap)

      // TouchEvent with empty touches (touchend has changedTouches, not touches)
      const touchEvent = new TouchEvent('touchstart', {
        touches: [],
        bubbles: true,
      })
      Object.defineProperty(touchEvent, 'target', { value: wrap, writable: false })
      // Should not throw
      onTableTouchStart(touchEvent)

      wrap.remove()
    })
  })
})
