import { ref } from 'vue'
import { parseTableDataFromElement, onTableMouseDown, onTableTouchStart, isTableDragClick } from '@/utils/tableRowExpand.ts'
import { usePlatformDetect } from '@/composables/usePlatformDetect.ts'

/**
 * Composable for table row expand modal.
 * Provides modal state, navigation, drag guard, and a unified click handler.
 * Used by ChatMessageList, ToolDetailDrawer, TaskExecDetail, and MarkdownPreview.
 *
 * The row viewer is a mobile/touch feature: it only opens on non-PC devices.
 * On PC, table images open the lightbox directly via their expand icon instead.
 */
export function useTableRowExpand() {
  const tableRowModal = ref<{ headers: string[], rows: string[][], currentIndex: number } | null>(null)
  const { isPC } = usePlatformDetect()

  function closeTableRowModal() {
    tableRowModal.value = null
  }

  function tableRowPrev() {
    if (tableRowModal.value && tableRowModal.value.currentIndex > 0) {
      tableRowModal.value.currentIndex--
    }
  }

  function tableRowNext() {
    if (tableRowModal.value && tableRowModal.value.currentIndex < tableRowModal.value.rows.length - 1) {
      tableRowModal.value.currentIndex++
    }
  }

  /**
   * Handle a click event that may be on a table data row.
   * Returns true if a table row was clicked and the modal was opened.
   * Returns false if the click was not on a table row (caller should continue event processing).
   */
  function handleTableRowClick(event: MouseEvent | PointerEvent): boolean {
    const target = event.target as HTMLElement
    // Row viewer is a mobile/touch feature — never open it on PC. On PC the
    // table image's lightbox expand icon opens the lightbox directly instead.
    if (isPC.value) return false
    // Only activate on touch — skip mouse clicks for PC mode
    if ('pointerType' in event && event.pointerType !== 'touch') return false
    // Skip if click target is an interactive element inside the cell, or the
    // lightbox expand icon (so that click bubbles to the Lightbox handler).
    if (target.closest('a, button, [contenteditable], input, select, textarea, .lightbox-expand-icon')) return false
    const tr = target.closest('tbody tr[data-row-idx]')
    if (!tr || isTableDragClick(event)) return false
    event.preventDefault()
    event.stopPropagation()
    const table = tr.closest('table[data-table-idx]') as HTMLTableElement | null
    if (!table) return false
    const rowIndex = parseInt((tr as HTMLElement).getAttribute('data-row-idx') || '0', 10)
    const data = parseTableDataFromElement(table)
    if (data && data.rows.length > 0) {
      tableRowModal.value = {
        headers: data.headers,
        rows: data.rows,
        currentIndex: rowIndex,
      }
    }
    return true
  }

  return {
    tableRowModal,
    closeTableRowModal,
    tableRowPrev,
    tableRowNext,
    handleTableRowClick,
    onTableMouseDown,
    onTableTouchStart,
  }
}
